package accountsmigrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/config"
)

const dataMigrationID = "accounts-config:data:v1"

var legacyTables = []string{"application", "application_access_token", "book_address", "book_storage", "bookinfo", "login_token", "web_project", "web_project_member", "web_project_release", "webdav_log"}

type Options struct {
	Database, BinarySHA256 string
	Steps                  []Step
	DDLChecksums           map[string]string
	Now                    time.Time
}
type TableSummary struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"data_sha256"`
}
type FileSummary struct {
	ReleaseID int64  `json:"release_id"`
	Files     int64  `json:"files"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"manifest_sha256"`
}
type Plan struct {
	Version             int                               `json:"version"`
	Database            string                            `json:"database"`
	SourceSHA256        string                            `json:"source_sha256"`
	BinarySHA256        string                            `json:"binary_sha256"`
	DDLChecksums        map[string]string                 `json:"ddl_checksums"`
	SchemaSHA256        string                            `json:"schema_sha256"`
	SourceUserIDs       []int64                           `json:"source_user_ids"`
	AliasCount          int                               `json:"alias_count"`
	Grants              Grants                            `json:"grants"`
	Tables              []TableSummary                    `json:"tables"`
	Files               []FileSummary                     `json:"files"`
	RuntimeValues       map[string]map[string]interface{} `json:"runtime_values"`
	PendingSteps        []string                          `json:"pending_steps"`
	DataAlreadyImported bool                              `json:"data_already_imported"`
	ImportSHA256        string                            `json:"import_sha256"`
	SHA256              string                            `json:"plan_sha256"`
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// BuildPlan is read-only. Hashes bind all legacy rows, schema, source configuration,
// selected grants, executable and ready-release files; the output contains no login keys.
func BuildPlan(ctx context.Context, db *sql.DB, source *Source, opts Options) (*Plan, error) {
	if !identifier.MatchString(opts.Database) || len(opts.BinarySHA256) != 64 {
		return nil, errors.New("valid database and binary SHA256 are required")
	}
	var currentDatabase string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&currentDatabase); err != nil {
		return nil, dbError("check database", err)
	}
	if currentDatabase != opts.Database {
		return nil, errors.New("connected database differs from requested target")
	}
	values := config.DefaultRuntimeValues(source.Config)
	if len(values) != 7 {
		return nil, errors.New("all seven runtime namespaces are required")
	}
	for namespace, value := range values {
		normalized, err := config.ValidateValues(source.Config, namespace, value)
		if err != nil {
			return nil, fmt.Errorf("source effective runtime configuration is invalid in namespace %s", namespace)
		}
		values[namespace] = normalized
	}
	// Preserve the published v1 import document and checksum. The optional
	// session limit is supplied by runtime reads of legacy account policies.
	delete(values["account_policy"], "max_active_sessions")
	delete(values["account_policy"], "min_share_password_length")
	delete(values["account_policy"], "share_code_length")
	values["registration"]["enabled"] = false
	plan := &Plan{Version: 1, Database: opts.Database, SourceSHA256: source.SHA256, BinarySHA256: opts.BinarySHA256, DDLChecksums: opts.DDLChecksums, AliasCount: len(source.Aliases), Grants: source.Grants, RuntimeValues: values}
	names := map[string]bool{"schema_migration": true}
	for _, name := range legacyTables {
		names[name] = true
	}
	for _, step := range opts.Steps {
		names[step.Table] = true
	}
	schemas := map[string]*TableSchema{}
	for name := range names {
		schema, err := inspectTable(ctx, db, opts.Database, name)
		if err != nil {
			return nil, err
		}
		schemas[name] = schema
	}
	for _, name := range legacyTables {
		schema := schemas[name]
		if schema == nil || !strings.EqualFold(schema.Engine, "InnoDB") {
			return nil, fmt.Errorf("required InnoDB legacy table %s is missing or incompatible", name)
		}
		if err := verifyLegacyTypes(schema); err != nil {
			return nil, err
		}
		summary, err := tableSummary(ctx, db, schema)
		if err != nil {
			return nil, err
		}
		plan.Tables = append(plan.Tables, summary)
	}
	for _, step := range opts.Steps {
		complete, err := verifyStep(step, schemas[step.Table])
		if err != nil {
			return nil, err
		}
		if !complete {
			plan.PendingSteps = append(plan.PendingSteps, step.ID)
		}
	}
	if schemas["schema_migration"] != nil {
		meta, err := metadataStep()
		if err != nil {
			return nil, err
		}
		if _, err = verifyStep(meta, schemas["schema_migration"]); err != nil {
			return nil, err
		}
		var phase, checksum string
		err = db.QueryRowContext(ctx, "SELECT phase,checksum FROM schema_migration WHERE migration_id=?", dataMigrationID).Scan(&phase, &checksum)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, dbError("read import marker", err)
		}
		plan.DataAlreadyImported = phase == "completed"
		if err = verifyMarkers(ctx, db, opts.Steps, schemas); err != nil {
			return nil, err
		}
	}
	if plan.DataAlreadyImported && len(plan.PendingSteps) > 0 {
		return nil, errors.New("completed data import has missing schema; manual recovery is required")
	}
	for _, user := range source.Users {
		plan.SourceUserIDs = append(plan.SourceUserIDs, user.ID)
	}
	importRaw, _ := json.Marshal(struct {
		Source string
		Grants Grants
		DDL    map[string]string
		Values map[string]map[string]interface{}
	}{source.SHA256, source.Grants, opts.DDLChecksums, values})
	plan.ImportSHA256 = digest(importRaw)
	if err := verifyImportSource(ctx, db, source, plan, schemas); err != nil {
		return nil, err
	}
	if err := verifyReferences(ctx, db, source, plan.DataAlreadyImported); err != nil {
		return nil, err
	}
	files, err := verifyFiles(ctx, db, source.Config.WebProjects.StorageRoot)
	if err != nil {
		return nil, err
	}
	plan.Files = files
	plan.SchemaSHA256 = digest(schemaJSON(schemas))
	raw, _ := json.Marshal(plan)
	plan.SHA256 = digest(raw)
	return plan, nil
}

// tableSummary 按全部字段排序读取旧表数据，对逐行内容计算摘要和行数，以便后续检测计划生成后的数据漂移。
func tableSummary(ctx context.Context, db queryer, schema *TableSchema) (TableSummary, error) {
	summary := TableSummary{Name: schema.Name}
	columns := make([]string, 0, len(schema.Columns))
	for _, column := range schema.Columns {
		columns = append(columns, "`"+strings.ReplaceAll(column.Name, "`", "``")+"`")
	}
	// Explicit column names preserve extra legacy fields while avoiding SELECT *.
	query := "SELECT " + strings.Join(columns, ",") + " FROM `" + schema.Name + "` ORDER BY " + strings.Join(columns, ",")
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return summary, dbError("fingerprint "+schema.Name, err)
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return summary, dbError("fingerprint row", err)
		}
		raw, _ := json.Marshal(values)
		hash.Write(raw)
		hash.Write([]byte{'\n'})
		summary.Rows++
	}
	if err := rows.Err(); err != nil {
		return summary, dbError("fingerprint rows", err)
	}
	summary.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return summary, nil
}

// verifyReferences 检查旧表用户引用、登录令牌 ID 唯一性及应用和网页关联完整性；已导入时改用账号表作为用户集合。
func verifyReferences(ctx context.Context, db queryer, source *Source, imported bool) error {
	ids := make([]interface{}, 0, len(source.Users))
	for _, user := range source.Users {
		ids = append(ids, user.ID)
	}
	known := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	if imported {
		known = "SELECT id FROM user_account"
		ids = nil
	}
	refs := [][2]string{{"login_token", "user_id"}, {"application", "owner_user_id"}, {"web_project", "owner_user_id"}, {"web_project_member", "user_id"}, {"web_project_member", "created_by"}, {"web_project_release", "uploaded_by"}, {"webdav_log", "user_id"}}
	for _, ref := range refs {
		var count int64
		query := "SELECT COUNT(*) FROM `" + ref[0] + "` WHERE `" + ref[1] + "` IS NULL OR `" + ref[1] + "` NOT IN (" + known + ")"
		if err := db.QueryRowContext(ctx, query, ids...).Scan(&count); err != nil {
			return dbError("check owner references", err)
		}
		if count != 0 {
			return fmt.Errorf("unknown owner references in %s.%s: %d", ref[0], ref[1], count)
		}
	}
	queries := []string{
		"SELECT COUNT(*) FROM login_token WHERE id IS NULL",
		"SELECT COUNT(*) FROM (SELECT id FROM login_token GROUP BY id HAVING COUNT(*)>1) AS duplicate_ids",
		"SELECT COUNT(*) FROM application_access_token t LEFT JOIN application a ON a.id=t.application_id WHERE a.id IS NULL",
		"SELECT COUNT(*) FROM web_project_member m LEFT JOIN web_project p ON p.id=m.project_id WHERE p.id IS NULL",
		"SELECT COUNT(*) FROM web_project_release r LEFT JOIN web_project p ON p.id=r.project_id WHERE p.id IS NULL OR r.uploaded_by<>p.owner_user_id",
		"SELECT COUNT(*) FROM web_project p LEFT JOIN web_project_release r ON r.id=p.current_release_id AND r.project_id=p.id WHERE p.current_release_id IS NOT NULL AND r.id IS NULL",
		"SELECT COUNT(*) FROM web_project WHERE status NOT IN (1,2,3,4) OR access_mode NOT IN (1,2,3,4) OR (status=4 AND deleted_at IS NULL)",
		"SELECT COUNT(*) FROM web_project_release WHERE status NOT IN (1,2,3,4)",
	}
	for i, query := range queries {
		var count int64
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return dbError("check legacy integrity", err)
		}
		if count != 0 {
			return fmt.Errorf("legacy integrity check %d failed: %d rows", i+1, count)
		}
	}
	return nil
}

// verifyFiles 逐个核验就绪版本的存储路径及常规文件，比较文件数量和总字节数，并生成包含文件内容的清单摘要。
func verifyFiles(ctx context.Context, db queryer, root string) ([]FileSummary, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,storage_key,file_count,total_bytes FROM web_project_release WHERE status=2 ORDER BY id")
	if err != nil {
		return nil, dbError("read ready releases", err)
	}
	defer rows.Close()
	var summaries []FileSummary
	for rows.Next() {
		var summary FileSummary
		var key string
		var expectedFiles, expectedBytes int64
		if err := rows.Scan(&summary.ReleaseID, &key, &expectedFiles, &expectedBytes); err != nil {
			return nil, dbError("read release manifest", err)
		}
		if root == "" || !filepath.IsAbs(root) || filepath.IsAbs(key) || key == "" || strings.Contains(key, "\\") || filepath.Clean(key) != key || key == ".." || strings.HasPrefix(key, "../") {
			return nil, fmt.Errorf("unsafe storage root/key for release ID %d", summary.ReleaseID)
		}
		directory := filepath.Join(root, filepath.FromSlash(key))
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil || resolved != directory {
			return nil, fmt.Errorf("missing or symlinked content root for release ID %d", summary.ReleaseID)
		}
		hash := sha256.New()
		// 跳过目录并拒绝非普通文件，将每个文件的相对路径、实际字节数和内容摘要纳入版本清单。
		err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return errors.New("cannot walk content")
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("content must contain regular files only")
			}
			file, err := os.Open(path)
			if err != nil {
				return errors.New("cannot read content")
			}
			fileHash := sha256.New()
			size, copyErr := io.Copy(fileHash, file)
			file.Close()
			if copyErr != nil {
				return errors.New("cannot hash content")
			}
			relative, _ := filepath.Rel(directory, path)
			raw, _ := json.Marshal([]interface{}{relative, size, hex.EncodeToString(fileHash.Sum(nil))})
			hash.Write(raw)
			summary.Files++
			summary.Bytes += size
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("content manifest check failed for release ID %d", summary.ReleaseID)
		}
		if summary.Files != expectedFiles || summary.Bytes != expectedBytes {
			return nil, fmt.Errorf("content count/size mismatch for release ID %d", summary.ReleaseID)
		}
		summary.SHA256 = hex.EncodeToString(hash.Sum(nil))
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError("read ready release rows", err)
	}
	return summaries, nil
}

func sortedNamespaces(values map[string]map[string]interface{}) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
