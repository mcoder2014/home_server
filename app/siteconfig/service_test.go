package siteconfig

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func runtimeTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_RUNTIME_TEST_DSN is required for MariaDB configuration tests")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_accounts_runtime_test" {
		t.Fatal("runtime tests require the dedicated home_server_accounts_runtime_test database")
	}
	database, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, name := range []string{"site_config_history", "site_config_current", "site_runtime_state"} {
		if err := database.Exec("DROP TABLE IF EXISTS " + name).Error; err != nil {
			t.Fatal(err)
		}
	}
	ddl, err := os.ReadFile("../../domain/dal/migrations/20260913_runtime_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := database.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	conf := config.Config{ConfigSource: "database"}
	config.SetGlobalConfig(conf)
	now := time.Now()
	if err := database.Exec("INSERT INTO site_runtime_state (id,config_generation,registration_epoch,revision,update_time) VALUES (1,1,1,1,?)", now).Error; err != nil {
		t.Fatal(err)
	}
	for namespace, values := range config.DefaultRuntimeValues(conf) {
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		if err := database.Exec("INSERT INTO site_config_current (namespace,revision,schema_version,values_json,values_sha256,updated_by,update_time) VALUES (?,1,1,?,?,1,?)", namespace, string(raw), hash[:], now).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Exec("INSERT INTO site_config_history (namespace,revision,schema_version,values_json,values_sha256,request_id,request_hash,actor_user_id,reason,create_time) VALUES (?,1,1,?,?,?, ?,1,'initial import',?)", namespace, string(raw), hash[:], "seed", hash[:], now).Error; err != nil {
			t.Fatal(err)
		}
	}
	return New(database, conf), database
}

func allowConfigTest(tx *gorm.DB) error { return nil }

func TestPublishIsIdempotentAndRollbackNeverRestoresInvitationEpoch(t *testing.T) {
	service, database := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	request := PublishRequest{RequestID: "open", Reason: "enable invitations", Values: map[string]interface{}{"enabled": true}}
	first, err := service.Publish(ctx, "registration", 1, 1, request, allowConfigTest)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.Revision != 2 || first.ApplyState != "applied" || !config.Runtime().RegistrationEnabled {
		t.Fatal("published registration was not applied")
	}
	repeat, err := service.Publish(ctx, "registration", 1, 1, request, allowConfigTest)
	if err != nil || repeat.Revision != 2 {
		t.Fatalf("identical retry failed: %v", err)
	}
	request.Values["enabled"] = false
	if _, err := service.Publish(ctx, "registration", 1, 2, request, allowConfigTest); err == nil {
		t.Fatal("same request ID accepted different payload")
	}
	request.RequestID = "close"
	closed, err := service.Publish(ctx, "registration", 1, 2, request, allowConfigTest)
	if err != nil || closed.Revision != 3 {
		t.Fatalf("close failed: %v", err)
	}
	var epoch int64
	if err := database.Raw("SELECT registration_epoch FROM site_runtime_state WHERE id=1").Scan(&epoch).Error; err != nil {
		t.Fatal(err)
	}
	if epoch != 2 {
		t.Fatalf("close must increment epoch exactly once: %d", epoch)
	}
	result, err := service.Rollback(ctx, "registration", 1, 3, RollbackRequest{TargetRevision: 2, RequestID: "rollback", Reason: "reopen"}, allowConfigTest)
	if err != nil || result.Revision != 4 || !config.Runtime().RegistrationEnabled {
		t.Fatalf("rollback failed: %v", err)
	}
	if err := database.Raw("SELECT registration_epoch FROM site_runtime_state WHERE id=1").Scan(&epoch).Error; err != nil {
		t.Fatal(err)
	}
	if epoch != 2 {
		t.Fatal("rollback revived old invitation epoch")
	}
	history, err := service.History(ctx, "registration", 0, 2)
	if err != nil || len(history.Items) != 2 || !history.HasMore || history.NextCursor != "3" {
		t.Fatalf("history pagination is incorrect: %#v %v", history, err)
	}
}

func TestPublishRequiresAuthorizationAndRejectsStaleRevision(t *testing.T) {
	service, _ := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	request := PublishRequest{RequestID: "change", Reason: "site name", Values: map[string]interface{}{"title": "New title", "notice": ""}}
	denied := errors.New("admin session revoked")
	if _, err := service.Publish(ctx, "site", 1, 1, request, func(tx *gorm.DB) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("authorization not rechecked in transaction: %v", err)
	}
	if _, err := service.Publish(ctx, "site", 1, 1, request, nil); err == nil {
		t.Fatal("nil authorization accepted")
	}
	if _, err := service.Publish(ctx, "site", 1, 99, request, allowConfigTest); err == nil {
		t.Fatal("stale revision accepted")
	}
	if config.Runtime().SiteTitle != "CQ Home Server" {
		t.Fatal("failed publication changed active snapshot")
	}
}

func TestConcurrentConfigWritersHaveExactlyOneWinner(t *testing.T) {
	service, _ := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			_, err := service.Publish(ctx, "site", 1, 1, PublishRequest{RequestID: id, Reason: "concurrent edit", Values: map[string]interface{}{"title": id, "notice": ""}}, allowConfigTest)
			results <- err
		}(id)
	}
	group.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected one successful writer, got %d", wins)
	}
}

func TestRefreshRetainsSnapshotAndFailsClosedForCorruptConfiguration(t *testing.T) {
	service, database := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE site_config_current SET values_json='{}', revision=revision+1 WHERE namespace='library'").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE site_runtime_state SET config_generation=config_generation+1 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(ctx); err == nil {
		t.Fatal("corrupt configuration loaded")
	}
	if config.Runtime().Generation != 1 {
		t.Fatal("corrupt refresh replaced last valid snapshot")
	}
	if allowed, err := service.Enabled(ctx, "library", "enabled"); err == nil || allowed {
		t.Fatal("strong switch allowed corrupt data")
	}
	status := service.Status(ctx)
	if status.ApplyState != "stale" || status.LastError == "" {
		t.Fatalf("invalid runtime health status: %#v", status)
	}
	if err := database.Exec("DELETE FROM site_config_current WHERE namespace='library'").Error; err != nil {
		t.Fatal(err)
	}
	if err := New(database, config.Config{ConfigSource: "database"}).Initialize(ctx); err == nil {
		t.Fatal("startup accepted a missing namespace")
	}
}

func TestHistoricalValuesRemainReadableAfterBootstrapLimitDecrease(t *testing.T) {
	_, database := runtimeTestService(t)
	conf := config.Config{ConfigSource: "database", UploadHardLimitBytes: 1 << 20}
	service := New(database, conf)
	history, err := service.History(context.Background(), "web_projects", 0, 20)
	if err != nil || len(history.Items) != 1 {
		t.Fatalf("history must remain readable under tighter deployment limits: %v", err)
	}
	if _, err := service.Rollback(context.Background(), "web_projects", 1, 1, RollbackRequest{TargetRevision: 1, RequestID: "old-invalid-limit", Reason: "invalid old values"}, allowConfigTest); err == nil {
		t.Fatal("rollback bypassed current hard limit")
	}
}

func TestPublishReportsSavedWhenAnotherNamespaceCannotReload(t *testing.T) {
	service, database := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE site_config_current SET values_json='{}' WHERE namespace='library'").Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.Publish(ctx, "site", 1, 1, PublishRequest{RequestID: "persist-with-stale", Reason: "change display", Values: map[string]interface{}{"title": "Saved title", "notice": ""}}, allowConfigTest)
	if err != nil || result == nil || result.PersistedRevision != 2 || result.LoadedRevision != 1 || result.ApplyState != "stale" {
		t.Fatalf("successful save was misreported after reload failure: %#v %v", result, err)
	}
	if config.Runtime().SiteTitle != "CQ Home Server" {
		t.Fatal("failed reload partially changed snapshot")
	}
	var revision int64
	if err := database.Raw("SELECT revision FROM site_config_current WHERE namespace='site'").Scan(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatal("publish did not persist despite saved response")
	}
}

func TestLockedModuleCheckSerializesWithAdministrativeClose(t *testing.T) {
	service, database := runtimeTestService(t)
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	checked := make(chan struct{})
	release := make(chan struct{})
	transactionDone := make(chan error, 1)
	go func() {
		transactionDone <- database.Transaction(func(tx *gorm.DB) error {
			enabled, err := service.EnabledTx(tx, "library", "enabled", true)
			if err != nil {
				return err
			}
			if !enabled {
				return errors.New("library initially disabled")
			}
			close(checked)
			<-release
			return nil
		})
	}()
	<-checked
	publishDone := make(chan error, 1)
	go func() {
		_, err := service.Publish(ctx, "library", 1, 1, PublishRequest{RequestID: "close-library", Reason: "disable access", Values: map[string]interface{}{"enabled": false}}, allowConfigTest)
		publishDone <- err
	}()
	select {
	case err := <-publishDone:
		close(release)
		t.Fatalf("close bypassed the in-flight policy row lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-transactionDone; err != nil {
		t.Fatal(err)
	}
	if err := <-publishDone; err != nil {
		t.Fatal(err)
	}
	if enabled, err := service.Enabled(ctx, "library", "enabled"); err != nil || enabled {
		t.Fatalf("new requests ignored committed module close: %v", err)
	}
}
