package accountsmigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/dal/migrations"
)

// fixtureDatabase 仅清空带专用前缀的测试库，重建合成旧表和数据，并读取内嵌迁移文件构造固定时间的迁移参数。
func fixtureDatabase(t *testing.T) (*sql.DB, *Source, Options) {
	t.Helper()
	dsn := os.Getenv("ACCOUNTS_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNTS_MIGRATION_TEST_DSN to an isolated synthetic MariaDB")
	}
	source, err := ParseSource([]byte(`passport:
  mock_data: '[{"id":123,"user_name":"owner","password":"`+fixtureHash+`","email":"owner@example.com"}]'
`), Grants{Admins: []int64{123}, Library: []int64{123}, WebDAVWrite: []int64{123}})
	if err != nil {
		t.Fatal(err)
	}
	source.Config.Mysql.MasterDB = dsn
	db, name, err := OpenDatabase(source, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "home_server_accounts_migration_test_") {
		db.Close()
		t.Fatal("test database prefix must be home_server_accounts_migration_test_")
	}
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", name)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		if !identifier.MatchString(table) {
			t.Fatal("unsafe table name")
		}
		tables = append(tables, table)
	}
	rows.Close()
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, "DROP TABLE `"+table+"`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		"CREATE TABLE bookinfo (id BIGINT PRIMARY KEY,title VARCHAR(512)) ENGINE=InnoDB",
		"CREATE TABLE book_storage (id BIGINT PRIMARY KEY,quantity INT) ENGINE=InnoDB",
		"CREATE TABLE book_address (id BIGINT PRIMARY KEY,address VARCHAR(512)) ENGINE=InnoDB",
		"CREATE TABLE login_token (id BIGINT NULL,user_id BIGINT,token VARCHAR(256),is_expired INT DEFAULT 0,expire_time DATETIME) ENGINE=InnoDB",
		"CREATE TABLE webdav_log (id BIGINT PRIMARY KEY,user_id BIGINT NOT NULL,filepath VARCHAR(512)) ENGINE=InnoDB",
		"CREATE TABLE application (id BIGINT PRIMARY KEY,owner_user_id BIGINT NOT NULL,revision BIGINT,secret_version BIGINT) ENGINE=InnoDB",
		"CREATE TABLE application_access_token (id BIGINT PRIMARY KEY,application_id BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE web_project (id BIGINT PRIMARY KEY,owner_user_id BIGINT NOT NULL,status TINYINT UNSIGNED,access_mode TINYINT UNSIGNED,current_release_id BIGINT,deleted_at DATETIME(6),revision BIGINT) ENGINE=InnoDB",
		"CREATE TABLE web_project_member (project_id BIGINT,user_id BIGINT,created_by BIGINT) ENGINE=InnoDB",
		"CREATE TABLE web_project_release (id BIGINT PRIMARY KEY,project_id BIGINT,uploaded_by BIGINT,storage_key VARCHAR(512),status TINYINT UNSIGNED,file_count INT,total_bytes BIGINT) ENGINE=InnoDB",
		"INSERT INTO book_storage (id,quantity) VALUES (1,58)",
		"INSERT INTO login_token (id,user_id,token,is_expired,expire_time) VALUES (100,123,'legacy-secret',0,'2030-01-01')",
		"INSERT INTO application (id,owner_user_id,revision,secret_version) VALUES (1,123,7,4)",
		"INSERT INTO application_access_token (id,application_id) VALUES (1,1)",
		"INSERT INTO web_project (id,owner_user_id,status,access_mode,revision) VALUES (1,123,3,1,8)",
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	var steps []Step
	files := map[string]string{}
	for _, file := range []string{"20260913_runtime_config.sql", "20260913_accounts.sql"} {
		raw, err := migrations.SQL.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseDDL(file, raw)
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, parsed...)
		files[file] = digest(raw)
	}
	opts := Options{Database: name, BinarySHA256: strings.Repeat("a", 64), Steps: steps, DDLChecksums: files, Now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.FixedZone("SG", 28800))}
	t.Cleanup(func() { db.Close() })
	return db, source, opts
}

// TestMigrationPlansAppliesAndDoesNotResetChangedCredentials 验证计划只读、应用要求库名和摘要一致、首次迁移使旧会话失效，且重跑保留用户后来修改的密码及认证版本。
func TestMigrationPlansAppliesAndDoesNotResetChangedCredentials(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	ctx := context.Background()
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='schema_migration'", opts.Database).Scan(&count); err != nil || count != 0 {
		t.Fatal("plan must not create tables")
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, "wrong_database"); err == nil {
		t.Fatal("wrong database confirmation accepted")
	}
	if err := Apply(ctx, db, source, opts, strings.Repeat("0", 64), opts.Database); err == nil {
		t.Fatal("wrong plan hash accepted")
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err != nil {
		t.Fatal(err)
	}
	var hash string
	var authVersion, revision int64
	if err := db.QueryRow("SELECT password_hash,auth_version FROM user_account WHERE id=123").Scan(&hash, &authVersion); err != nil {
		t.Fatal(err)
	}
	if hash != fixtureHash || authVersion != 2 {
		t.Fatal("legacy password must survive; old sessions must lose their version")
	}
	var expired int
	if err := db.QueryRow("SELECT is_expired FROM login_token WHERE id=100").Scan(&expired); err != nil || expired != 1 {
		t.Fatal("old sessions must remain expired even for a legacy rollback binary")
	}
	if err := db.QueryRow("SELECT revision FROM application WHERE id=1").Scan(&revision); err != nil || revision != 7 {
		t.Fatal("application revision changed")
	}
	if _, err := db.Exec("UPDATE user_account SET password_hash='changed-in-app',display_name='new display',auth_version=3 WHERE id=123"); err != nil {
		t.Fatal(err)
	}
	plan, err = BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT password_hash,auth_version FROM user_account WHERE id=123").Scan(&hash, &authVersion); err != nil {
		t.Fatal(err)
	}
	if hash != "changed-in-app" || authVersion != 3 {
		t.Fatal("rerun reset a password or invalidated sessions again")
	}
}

func TestMigrationRejectsUnknownOwnersBeforeDDL(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	if _, err := db.Exec("UPDATE application SET owner_user_id=999 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("unknown owner accepted")
	}
}

// TestMigrationResumesCompletedDDLWithStartedMarker 模拟 DDL 已生效但标记仍为 started 的中断，验证新计划可恢复完成且拒绝迁移文件摘要变化。
func TestMigrationResumesCompletedDDLWithStartedMarker(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(metadataSQL); err != nil {
		t.Fatal(err)
	}
	first := opts.Steps[0]
	if _, err := db.Exec("INSERT INTO schema_migration (migration_id,checksum,phase,started_at,summary_json) VALUES (?,?,'started',NOW(6),'{}')", first.ID, first.Checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(first.SQL); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err != nil {
		t.Fatal(err)
	}
	var phase string
	if err := db.QueryRow("SELECT phase FROM schema_migration WHERE migration_id=?", first.ID).Scan(&phase); err != nil || phase != "completed" {
		t.Fatal("interrupted DDL was not recovered")
	}
	opts.Steps[0].Checksum = strings.Repeat("0", 64)
	if _, err := BuildPlan(ctx, db, source, opts); err == nil {
		t.Fatal("changed embedded DDL checksum accepted")
	}
}

func TestMigrationRejectsStructureAndSourceDriftBeforeApplying(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	ctx := context.Background()
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	oldSHA := source.SHA256
	source.SHA256 = strings.Repeat("b", 64)
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err == nil {
		t.Fatal("source drift accepted")
	}
	source.SHA256 = oldSHA
	if _, err := db.Exec("ALTER TABLE login_token ADD COLUMN auth_version INT NOT NULL DEFAULT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(ctx, db, source, opts); err == nil {
		t.Fatal("same-name incompatible column accepted")
	}
}

// TestMigrationSeedsEffectiveValuesAndFixedDeletionDeadline 验证导入保留源配置的配额和删除保留期，固定历史删除截止时间，并默认关闭邀请注册。
func TestMigrationSeedsEffectiveValuesAndFixedDeletionDeadline(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	ctx := context.Background()
	source.Config.Auth.MaxApplicationsPerUser = 37
	source.Config.WebProjects.DeleteRetentionDays = 19
	if _, err := db.Exec("UPDATE web_project SET status=4,deleted_at='2026-09-01 12:00:00' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRow("SELECT JSON_UNQUOTE(JSON_EXTRACT(values_json,'$.max_applications_per_user')) FROM site_config_current WHERE namespace='auth'").Scan(&value); err != nil || value != "37" {
		t.Fatal("source effective application quota not imported")
	}
	if err := db.QueryRow("SELECT JSON_UNQUOTE(JSON_EXTRACT(values_json,'$.enabled')) FROM site_config_current WHERE namespace='registration'").Scan(&value); err != nil || value != "false" {
		t.Fatal("registration must initially remain closed")
	}
	if err := db.QueryRow("SELECT DATE_FORMAT(purge_after,'%Y-%m-%d %H:%i:%s') FROM web_project WHERE id=1").Scan(&value); err != nil || value != "2026-09-20 12:00:00" {
		t.Fatal("legacy deletion deadline not preserved")
	}
}

// TestMigrationFileManifestDetectsContentDrift 用临时网页文件验证计划含内容摘要但不泄漏私密字段，并拒绝同尺寸内容变化或文件缺失。
func TestMigrationFileManifestDetectsContentDrift(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	root := t.TempDir()
	source.Config.WebProjects.StorageRoot = root
	key := "123/upload/html/1/releases/2/content"
	directory := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "index.html")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO web_project_release (id,project_id,uploaded_by,storage_key,status,file_count,total_bytes) VALUES (2,1,123,?,2,1,5)", key); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(context.Background(), db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Bytes != 5 {
		t.Fatal("ready content not verified")
	}
	raw, _ := json.Marshal(plan)
	for _, private := range []string{fixtureHash, "owner@example.com", "legacy-secret", key, root} {
		if strings.Contains(string(raw), private) {
			t.Fatal("plan contains private values")
		}
	}
	if err := os.WriteFile(path, []byte("world"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), db, source, opts, plan.SHA256, opts.Database); err == nil {
		t.Fatal("same-size content drift accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("missing content accepted")
	}
}

func TestMigrationRejectsExtraColumnsInNewTables(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	if _, err := db.Exec(opts.Steps[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE site_config_current ADD COLUMN unexpected INT"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("unexpected new-table structure accepted")
	}
}

func TestMigrationRejectsUnmarkedAccountGrantConflicts(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	for _, step := range opts.Steps {
		if step.Table == "user_account" && step.Create {
			if _, err := db.Exec(step.SQL); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec("INSERT INTO user_account (id,username,username_key,password_hash,source,invite_eligible_at,create_time,update_time) VALUES (123,'owner','owner',?,'config_import',NOW(6),NOW(6),NOW(6))", fixtureHash); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("unmarked account grants differ from reviewed bootstrap plan")
	}
}

func TestMigrationRejectsUnmarkedConfigurationBeforeDDL(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	if _, err := db.Exec(opts.Steps[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO site_config_current (namespace,revision,schema_version,values_json,values_sha256,updated_by,update_time) VALUES ('registration',1,1,'{}',?,123,NOW(6))", make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("unmarked runtime data must be refused in read-only preflight")
	}
}

func TestMigrationRejectsIncompatibleLegacyIdentityColumn(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	if _, err := db.Exec("ALTER TABLE login_token MODIFY COLUMN user_id VARCHAR(32)"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), db, source, opts); err == nil {
		t.Fatal("incompatible legacy identity type accepted")
	}
}

// TestCompletedImportRequiresCompleteSchema 迁移完成后删除历史表及其 DDL 标记，验证再次规划仍会拒绝不完整结构。
func TestCompletedImportRequiresCompleteSchema(t *testing.T) {
	db, source, opts := fixtureDatabase(t)
	ctx := context.Background()
	plan, err := BuildPlan(ctx, db, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, source, opts, plan.SHA256, opts.Database); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE site_config_history"); err != nil {
		t.Fatal(err)
	}
	for _, step := range opts.Steps {
		if step.Table == "site_config_history" {
			if _, err := db.Exec("DELETE FROM schema_migration WHERE migration_id=?", step.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := BuildPlan(ctx, db, source, opts); err == nil {
		t.Fatal("completed import with missing runtime history schema accepted")
	}
}
