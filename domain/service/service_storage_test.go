package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	"gorm.io/gorm"
)

// The database name is fixed before any DDL. This fixture never reads deployed
// configuration; all paths are under t.TempDir and all account data is synthetic.
func storageIsolationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_STORAGE_ISOLATION_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_STORAGE_ISOLATION_TEST_DSN required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_storage_isolation_test" {
		t.Fatal("dedicated storage isolation test database required")
	}
	if err := db.InitDatabase(dsn); err != nil {
		t.Fatal("cannot connect to storage isolation test database")
	}
	database := db.MasterDB()
	connection, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	for _, table := range []string{"user_account", "site_config_current", "site_config_history", "site_runtime_state"} {
		if err := database.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"20260913_accounts.sql", "20260913_runtime_config.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		statements := strings.Split(string(raw), ";")
		if name == "20260913_accounts.sql" {
			statements = statements[:1] // Only the account table is needed for startup.
		}
		for _, statement := range statements {
			if strings.TrimSpace(statement) != "" {
				if err := database.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	now := time.Now()
	if err := database.Exec("INSERT INTO site_runtime_state (id,config_generation,registration_epoch,revision,update_time) VALUES (1,1,1,1,?)", now).Error; err != nil {
		t.Fatal(err)
	}
	for namespace, values := range config.DefaultRuntimeValues(config.Config{}) {
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		if err := database.Exec("INSERT INTO site_config_current (namespace,revision,schema_version,values_json,values_sha256,updated_by,update_time) VALUES (?,1,1,?,?,1,?)", namespace, string(raw), hash[:], now).Error; err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func TestInitStorageIsolation(t *testing.T) {
	database := storageIsolationDatabase(t)
	before := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(before) })
	tests := []struct {
		name, layout     string
		databaseMode     bool
		bootstrapEnabled bool
		databaseEnabled  bool
		wantError        bool
	}{
		{"database_disabled_bootstrap_nested_root", "nested", true, false, true, true},
		{"database_disabled_bootstrap_same_root", "same", true, false, true, true},
		{"database_disabled_bootstrap_shared_inside_web", "ancestor", true, false, true, true},
		{"database_disabled_bootstrap_symlink_overlap", "symlink", true, false, true, true},
		{"database_closed_still_checks_future_enable", "nested", true, false, false, true},
		{"database_enabled_bootstrap_overlap", "nested", true, true, true, true},
		{"database_disabled_bootstrap_creates_separate_root", "separate", true, false, true, false},
		{"database_disabled_bootstrap_separate_symlink", "separate_symlink", true, false, true, false},
		{"file_disabled_keeps_overlap_compatibility", "nested", false, false, false, false},
		{"file_enabled_rejects_overlap", "nested", false, true, false, true},
		{"file_enabled_creates_separate_root", "separate", false, true, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			shared := filepath.Join(root, "shared")
			if err := os.MkdirAll(shared, 0700); err != nil {
				t.Fatal(err)
			}
			storage := filepath.Join(root, "new", "private")
			switch test.layout {
			case "nested":
				storage = filepath.Join(shared, "web")
			case "same":
				storage = shared
			case "ancestor":
				storage = root
			case "symlink", "separate_symlink":
				target := shared
				if test.layout == "separate_symlink" {
					target = filepath.Join(root, "private")
					if err := os.MkdirAll(target, 0700); err != nil {
						t.Fatal(err)
					}
				}
				storage = filepath.Join(root, "web-link")
				if err := os.Symlink(target, storage); err != nil {
					t.Fatal(err)
				}
			}
			conf := config.Config{IdentitySource: "config", ConfigSource: "file"}
			conf.Passport.MockData = "[]"
			conf.WebProjects.Enabled = test.bootstrapEnabled
			conf.WebProjects.StorageRoot = storage
			conf.WebProjects.SiteOrigin = "https://home.example.com"
			conf.WebDAV.SharePath = shared
			if test.databaseMode {
				conf.IdentitySource, conf.ConfigSource = "database", "database"
				values := config.DefaultRuntimeValues(conf)["web_projects"]
				values["enabled"] = test.databaseEnabled
				raw, err := json.Marshal(values)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(raw)
				if err := database.Exec("UPDATE site_config_current SET values_json=?,values_sha256=? WHERE namespace='web_projects'", string(raw), hash[:]).Error; err != nil {
					t.Fatal(err)
				}
			}
			config.SetGlobalConfig(conf)
			err := Init(&conf)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "must not overlap") {
					t.Fatalf("overlapping storage must fail before serving: got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid storage layout failed startup: %v", err)
			}
			if test.databaseMode || test.bootstrapEnabled {
				if info, err := os.Stat(storage); err != nil || !info.IsDir() {
					t.Fatalf("startup did not initialize missing storage directory: %v", err)
				}
			}
			if test.databaseMode {
				if err := siteconfig.New(database, conf).Initialize(context.Background()); err != nil {
					t.Fatalf("valid database configuration failed to load: %v", err)
				}
				if !config.Runtime().WebProjects.Enabled || conf.WebProjects.Enabled {
					t.Fatal("database must enable the module while bootstrap remains disabled")
				}
			}
		})
	}
}
