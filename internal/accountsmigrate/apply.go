package accountsmigrate

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const metadataSQL = `CREATE TABLE schema_migration (
 migration_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 checksum CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 phase VARCHAR(16) NOT NULL,
 started_at DATETIME(6) NOT NULL,
 completed_at DATETIME(6) NULL,
 summary_json LONGTEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func metadataStep() (Step, error) {
	steps, err := ParseDDL("metadata", []byte(metadataSQL))
	if err != nil {
		return Step{}, err
	}
	return steps[0], nil
}

// verifyMarkers 将已记录 DDL 的摘要与步骤定义比较，拒绝未知阶段，以及完成标记对应的结构缺失。
func verifyMarkers(ctx context.Context, db queryer, steps []Step, schemas map[string]*TableSchema) error {
	for _, step := range steps {
		var checksum, phase string
		err := db.QueryRowContext(ctx, "SELECT checksum,phase FROM schema_migration WHERE migration_id=?", step.ID).Scan(&checksum, &phase)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return dbError("read DDL marker", err)
		}
		if checksum != step.Checksum {
			return fmt.Errorf("DDL checksum conflict at %s", step.ID)
		}
		complete, err := verifyStep(step, schemas[step.Table])
		if err != nil {
			return err
		}
		if phase == "completed" && !complete {
			return fmt.Errorf("completed DDL has missing structure at %s", step.ID)
		}
		if phase != "started" && phase != "completed" {
			return fmt.Errorf("unknown DDL phase at %s", step.ID)
		}
	}
	return nil
}

// verifyImportSource refuses conflicting preexisting identities/configuration before
// any DDL; completed imports may retain later profile, password and permission changes.
func verifyImportSource(ctx context.Context, db queryer, source *Source, plan *Plan, schemas map[string]*TableSchema) error {
	if schemas["schema_migration"] != nil {
		var checksum string
		err := db.QueryRowContext(ctx, "SELECT checksum FROM schema_migration WHERE migration_id=?", dataMigrationID).Scan(&checksum)
		if err == nil && checksum != plan.ImportSHA256 {
			return errors.New("data import checksum conflicts with recorded source/grants/DDL")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return dbError("read data import source", err)
		}
	}
	if !plan.DataAlreadyImported {
		for _, table := range []string{"site_config_current", "site_config_history", "site_runtime_state"} {
			if schemas[table] == nil {
				continue
			}
			var count int64
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&count); err != nil {
				return dbError("check existing runtime data", err)
			}
			if count != 0 {
				return errors.New("runtime tables contain data without completed import marker; refusing overwrite")
			}
		}
	}
	if schemas["user_account"] != nil {
		for _, user := range source.Users {
			var id, authVersion int64
			var username, key, hash, origin, role, permission string
			var library bool
			err := db.QueryRowContext(ctx, "SELECT id,username,username_key,password_hash,source,auth_version,role,library_enabled,webdav_permission FROM user_account WHERE id=? OR username_key=?", user.ID, strings.ToLower(user.Username)).Scan(&id, &username, &key, &hash, &origin, &authVersion, &role, &library, &permission)
			if errors.Is(err, sql.ErrNoRows) {
				if plan.DataAlreadyImported {
					return fmt.Errorf("imported user ID %d is missing", user.ID)
				}
				continue
			}
			if err != nil {
				return dbError("check existing account", err)
			}
			if id != user.ID || username != user.Username || key != strings.ToLower(user.Username) || origin != "config_import" {
				return fmt.Errorf("existing identity conflicts with source user ID %d", user.ID)
			}
			if !plan.DataAlreadyImported && (role != user.Role || library != user.LibraryEnabled || permission != user.WebDAVPermission) {
				return fmt.Errorf("unmarked existing grants conflict at user ID %d", user.ID)
			}
			if !plan.DataAlreadyImported && (hash != user.PasswordHash || authVersion != 1) {
				return fmt.Errorf("unmarked existing account has changed credentials at user ID %d", user.ID)
			}
		}
		if !plan.DataAlreadyImported {
			var count int64
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_account").Scan(&count); err != nil {
				return dbError("count existing accounts", err)
			}
			ids := make([]interface{}, 0, len(source.Users))
			for _, user := range source.Users {
				ids = append(ids, user.ID)
			}
			query := "SELECT COUNT(*) FROM user_account WHERE id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
			if err := db.QueryRowContext(ctx, query, ids...).Scan(&count); err != nil {
				return dbError("check unmarked account IDs", err)
			}
			if count != 0 {
				return errors.New("unmarked account table contains unrelated users")
			}
		}
	}
	if schemas["user_login_alias"] != nil {
		for _, alias := range source.Aliases {
			var id int64
			var kind string
			err := db.QueryRowContext(ctx, "SELECT user_id,kind FROM user_login_alias WHERE login_key=?", alias.Key).Scan(&id, &kind)
			if errors.Is(err, sql.ErrNoRows) {
				if plan.DataAlreadyImported {
					return fmt.Errorf("imported alias is missing for user ID %d", alias.UserID)
				}
				continue
			}
			if err != nil {
				return dbError("check existing aliases", err)
			}
			if id != alias.UserID || kind != alias.Kind {
				return fmt.Errorf("existing alias conflicts for user ID %d", alias.UserID)
			}
		}
	}
	return nil
}

// Apply requires a freshly reviewed plan and exact database confirmation. DDL
// completion is tracked statement-by-statement; only data import is transactional.
// A crash after DDL needs a new plan, whose remaining steps resume safely.
func Apply(ctx context.Context, db *sql.DB, source *Source, opts Options, planHash, confirmDatabase string) error {
	if confirmDatabase != opts.Database || confirmDatabase == "" {
		return errors.New("exact database confirmation is required")
	}
	if len(planHash) != 64 {
		return errors.New("reviewed plan SHA256 is required")
	}
	lockName := "home_accounts:" + digest([]byte(opts.Database))[:32]
	var locked sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lockName).Scan(&locked); err != nil {
		return dbError("acquire migration lock", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return errors.New("another account migration owns this database lock")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var ignored sql.NullInt64
		db.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&ignored)
	}()
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		return err
	}
	if plan.SHA256 != planHash {
		return errors.New("plan drift detected; regenerate and review the plan before applying")
	}
	if plan.DataAlreadyImported {
		return nil
	}
	meta, err := metadataStep()
	if err != nil {
		return err
	}
	schema, err := inspectTable(ctx, db, opts.Database, "schema_migration")
	if err != nil {
		return err
	}
	if schema == nil {
		if _, err = db.ExecContext(ctx, meta.SQL); err != nil {
			return dbError("create migration metadata", err)
		}
	}
	for _, step := range opts.Steps {
		if err := applyStep(ctx, db, opts.Database, step); err != nil {
			return err
		}
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO schema_migration (migration_id,checksum,phase,started_at,summary_json) VALUES (?,?,'started',NOW(6),'{}') ON DUPLICATE KEY UPDATE migration_id=migration_id", dataMigrationID, plan.ImportSHA256); err != nil {
		return dbError("record data import start", err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return dbError("begin data import", err)
	}
	defer tx.Rollback()
	if err := importData(ctx, tx, source, plan, opts.Now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return dbError("commit data import", err)
	}
	return nil
}

// applyStep 先记录 started，再执行尚未完成的增量 DDL，核验结构后标记 completed；DDL 隐式提交，失败不会回滚已生效结构。
func applyStep(ctx context.Context, db *sql.DB, database string, step Step) error {
	schema, err := inspectTable(ctx, db, database, step.Table)
	if err != nil {
		return err
	}
	complete, err := verifyStep(step, schema)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO schema_migration (migration_id,checksum,phase,started_at,summary_json) VALUES (?,?,'started',NOW(6),'{}') ON DUPLICATE KEY UPDATE migration_id=migration_id", step.ID, step.Checksum); err != nil {
		return dbError("record DDL start", err)
	}
	if !complete {
		if _, err = db.ExecContext(ctx, step.SQL); err != nil {
			return fmt.Errorf("DDL step %s: %w", step.ID, dbError("execute additive DDL", err))
		}
		schema, err = inspectTable(ctx, db, database, step.Table)
		if err != nil {
			return err
		}
		complete, err = verifyStep(step, schema)
		if err != nil {
			return err
		}
		if !complete {
			return fmt.Errorf("DDL verification failed at %s", step.ID)
		}
	}
	if _, err = db.ExecContext(ctx, "UPDATE schema_migration SET phase='completed',completed_at=NOW(6) WHERE migration_id=? AND checksum=?", step.ID, step.Checksum); err != nil {
		return dbError("complete DDL marker", err)
	}
	return nil
}

// importData keeps credentials, aliases, explicit grants, runtime seeds, the
// one-time session version bump and completion marker in the same transaction.
// Existing resource IDs, application credentials and shared files are untouched.
func importData(ctx context.Context, tx *sql.Tx, source *Source, plan *Plan, now time.Time) error {
	var checksum, phase string
	if err := tx.QueryRowContext(ctx, "SELECT checksum,phase FROM schema_migration WHERE migration_id=? FOR UPDATE", dataMigrationID).Scan(&checksum, &phase); err != nil {
		return dbError("lock import marker", err)
	}
	if checksum != plan.ImportSHA256 {
		return errors.New("import source checksum changed")
	}
	if phase == "completed" {
		return nil
	}
	for _, table := range []string{"site_config_current", "site_config_history", "site_runtime_state"} {
		var count int64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&count); err != nil {
			return dbError("check empty runtime seed", err)
		}
		if count != 0 {
			return errors.New("runtime tables contain data without completed import marker; refusing overwrite")
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	zone, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		return errors.New("Asia/Singapore timezone data is required")
	}
	monthNow := now.In(zone)
	eligible := time.Date(monthNow.Year(), monthNow.Month(), 1, 0, 0, 0, 0, zone)
	for _, user := range source.Users {
		created := user.CreateTime
		if created.IsZero() {
			created = now
		}
		updated := user.UpdateTime
		if updated.IsZero() {
			updated = now
		}
		var existing int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM user_account WHERE id=? FOR UPDATE", user.ID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO user_account (id,username,username_key,display_name,contact_email,contact_mobile,password_hash,status,role,library_enabled,webdav_permission,auth_version,revision,must_change_password,invite_eligible_at,source,imported_at,create_time,update_time) VALUES (?,?,?,?,?,?,?,'active',?,?,?,1,1,FALSE,?,'config_import',?,?,?)`, user.ID, user.Username, strings.ToLower(user.Username), user.Username, user.Email, user.Mobile, user.PasswordHash, user.Role, user.LibraryEnabled, user.WebDAVPermission, eligible, now, created, updated)
		}
		if err != nil {
			return dbError("insert imported account", err)
		}
	}
	for _, alias := range source.Aliases {
		var existing int64
		err := tx.QueryRowContext(ctx, "SELECT user_id FROM user_login_alias WHERE login_key=? FOR UPDATE", alias.Key).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, "INSERT INTO user_login_alias (login_key,user_id,kind) VALUES (?,?,?)", alias.Key, alias.UserID, alias.Kind)
		}
		if err != nil {
			return dbError("insert imported alias", err)
		}
		if existing != 0 && existing != alias.UserID {
			return errors.New("alias changed during import")
		}
	}
	actorID := source.Grants.Admins[0]
	if err := seedRuntime(ctx, tx, plan, actorID, now); err != nil {
		return err
	}
	retention := plan.RuntimeValues["web_projects"]["delete_retention_days"]
	if _, err := tx.ExecContext(ctx, "UPDATE web_project SET purge_after=DATE_ADD(deleted_at, INTERVAL ? DAY) WHERE status=4 AND purge_after IS NULL", retention); err != nil {
		return dbError("backfill fixed delete retention", err)
	}
	for _, user := range source.Users {
		result, err := tx.ExecContext(ctx, "UPDATE user_account SET auth_version=2 WHERE id=? AND auth_version=1", user.ID)
		if err != nil {
			return dbError("invalidate legacy user sessions", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return errors.New("account version changed during migration; data import rolled back")
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE login_token SET is_expired=1 WHERE purpose='user' AND auth_version=1"); err != nil {
		return dbError("permanently expire legacy user tokens", err)
	}
	summary, _ := json.Marshal(struct {
		IDs                               []int64 `json:"user_ids"`
		Grants                            Grants  `json:"grants"`
		SourceSHA256                      string  `json:"source_sha256"`
		UnknownCreateTimeRecordedAsImport bool    `json:"unknown_create_time_uses_import_time"`
	}{plan.SourceUserIDs, source.Grants, source.SHA256, true})
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_audit_log (actor_user_id,action,target_type,target_id,before_summary,after_summary,reason,result,request_id,create_time) VALUES (?,'accounts.import','migration',0,'{}',?,'explicit offline account/config import','success',?,?)", actorID, string(summary), plan.ImportSHA256, now); err != nil {
		return dbError("record migration audit", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE schema_migration SET phase='completed',completed_at=?,summary_json=? WHERE migration_id=? AND checksum=?", now, string(summary), dataMigrationID, plan.ImportSHA256); err != nil {
		return dbError("complete data import", err)
	}
	return nil
}

func seedRuntime(ctx context.Context, tx *sql.Tx, plan *Plan, actorID int64, now time.Time) error {
	for _, namespace := range sortedNamespaces(plan.RuntimeValues) {
		raw, _ := json.Marshal(plan.RuntimeValues[namespace])
		hash, _ := hex.DecodeString(digest(raw))
		requestHash, _ := hex.DecodeString(plan.ImportSHA256)
		if _, err := tx.ExecContext(ctx, "INSERT INTO site_config_current (namespace,revision,schema_version,values_json,values_sha256,updated_by,update_time) VALUES (?,1,1,?,?,?,?)", namespace, string(raw), hash, actorID, now); err != nil {
			return dbError("seed current configuration", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO site_config_history (namespace,revision,schema_version,values_json,values_sha256,request_id,request_hash,actor_user_id,reason,create_time) VALUES (?,1,1,?,?,'accounts-import-v1',?,?,'explicit source configuration import',?)", namespace, string(raw), hash, requestHash, actorID, now); err != nil {
			return dbError("seed configuration history", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO site_runtime_state (id,config_generation,registration_epoch,revision,update_time) VALUES (1,1,1,1,?)", now); err != nil {
		return dbError("seed runtime state", err)
	}
	return nil
}
