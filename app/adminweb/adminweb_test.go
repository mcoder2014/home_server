package adminweb

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/app/siteconfig"
	userweb "github.com/mcoder2014/home_server/app/webprojects"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	webservice "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const adminTestPassword = "test-admin-password-12345"

func moderationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_WEB_MODERATION_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_WEB_MODERATION_TEST_DSN required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_accounts_web_test" {
		t.Fatal("dedicated web test database required")
	}
	if err := db.InitDatabase(dsn); err != nil {
		t.Fatal(err)
	}
	database := db.MasterDB()
	for _, table := range []string{"site_config_current", "site_config_history", "site_runtime_state", "application_access_token", "application", "user_account", "user_login_alias", "user_invitation", "admin_audit_log", "web_project_release", "web_project_member", "web_project", "login_token"} {
		if err := database.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Exec("CREATE TABLE login_token (id BIGINT NOT NULL,token VARCHAR(512),user_id BIGINT,is_expired INT,expire_time DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"20260909_web_projects.sql", "20260912_applications.sql", "20260913_accounts.sql", "20260913_runtime_config.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(stmt) != "" {
				if err := database.Exec(stmt).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(before) })
	conf := config.Config{IdentitySource: "database", ConfigSource: "database"}
	conf.WebProjects.StorageRoot = t.TempDir()
	conf.WebProjects.MaxUploadBytes = 50 << 20
	conf.WebProjects.Enabled = true
	config.SetGlobalConfig(conf)
	now := time.Now()
	if err := database.Exec("INSERT INTO site_runtime_state VALUES (1,1,1,1,?)", now).Error; err != nil {
		t.Fatal(err)
	}
	for ns, values := range config.DefaultRuntimeValues(conf) {
		raw, _ := json.Marshal(values)
		hash := sha256.Sum256(raw)
		if err := database.Exec("INSERT INTO site_config_current VALUES (?,1,1,?,?,100,?)", ns, string(raw), hash[:], now).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := siteconfig.New(database, conf).Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminTestPassword), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []*model.UserAccount{
		{ID: 100, Username: "admin", UsernameKey: "admin", PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleAdmin, AuthVersion: 1, Revision: 1, InviteEligibleAt: now, Source: "test", WebDAVPermission: "none", CreateTime: now, UpdateTime: now},
		{ID: 200, Username: "owner", UsernameKey: "owner", PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleUser, AuthVersion: 1, Revision: 1, InviteEligibleAt: now, Source: "test", WebDAVPermission: "none", CreateTime: now, UpdateTime: now},
	} {
		if err := database.Table(dal.AccountTable).Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	releaseID := int64(3000)
	project := &model.WebProject{ID: 1000, OwnerUserID: 200, Name: "private project", Description: "test", Slug: "private-test", AccessMode: model.WebProjectAccessOwner, Status: model.WebProjectStatusEnabled, Revision: 1, CurrentReleaseID: &releaseID, ModerationStatus: "normal", CreateTime: now, UpdateTime: now}
	if err := dal.CreateWebProject(database, project); err != nil {
		t.Fatal(err)
	}
	release := &model.WebProjectRelease{ID: releaseID, ProjectID: project.ID, UploadedBy: 200, StorageKey: "200/upload/html/1000/releases/3000/content", Status: model.WebProjectReleaseReady, EntryFile: "index.html", SHA256: strings.Repeat("a", 64), FileCount: 1, TotalBytes: 4, Extra: "{}", CreateTime: now, UpdateTime: now}
	if err := dal.CreateWebProjectRelease(database, release); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(conf.WebProjects.StorageRoot, release.StorageKey)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestAdminModerationCannotBeBypassedByOwnerPublishOrRestore(t *testing.T) {
	database := moderationDatabase(t)
	ctx := context.Background()
	if _, err := List(ctx, 200, 1, Filter{}); err == nil {
		t.Fatal("regular owner accessed global admin list")
	}
	page, err := List(ctx, 100, 1, Filter{})
	if err != nil || page == nil || len(page.Items) != 1 {
		t.Fatalf("admin cannot see private project: %v", err)
	}
	blocked, err := Change(ctx, 100, 1, 1000, 1, "block", MutationRequest{Reason: "risk", CurrentPassword: adminTestPassword})
	if err != nil || blocked == nil || blocked.ModerationStatus != "blocked" {
		t.Fatalf("project was not blocked: %v", err)
	}
	conf := config.Runtime().WebProjects
	if _, err := userweb.Default.PublishRelease(&conf, 200, 1000, 3000, 2); err == nil {
		t.Fatal("owner published blocked project")
	}
	if _, _, err := userweb.Default.GetPublishedProject("private-test"); err == nil {
		t.Fatal("blocked project remains publicly readable")
	}
	deleted, err := Change(ctx, 100, 1, 1000, 2, "delete", MutationRequest{Reason: "remove risk", CurrentPassword: adminTestPassword})
	if err != nil || deleted == nil || deleted.PurgeAfter == nil {
		t.Fatalf("admin delete missing frozen deadline: %v", err)
	}
	if _, err := userweb.Default.ChangeProjectStatus(200, 1000, 3, "restore", 7); err == nil {
		t.Fatal("owner restored administrator deleted project")
	}
	if _, err := Change(ctx, 100, 1, 1000, 3, "restore", MutationRequest{Reason: "reviewed", CurrentPassword: adminTestPassword}); err != nil {
		t.Fatal(err)
	}
	var logs int64
	if err := database.Table(dal.AdminAuditTable).Count(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if logs != 3 {
		t.Fatalf("expected three audited admin mutations, got %d", logs)
	}
}

func TestAdminPreviewBypassesVisibilityButRequiresLiveAdminSession(t *testing.T) {
	database := moderationDatabase(t)
	ctx := context.Background()
	if err := database.Exec("UPDATE site_config_current SET values_json='{\"enabled\":false}' WHERE namespace='web_projects'").Error; err != nil {
		t.Fatal(err)
	}
	file, err := OpenPreview(ctx, 100, 1, 1000, 3000, "")
	if err != nil || file == nil {
		t.Fatalf("admin preview should bypass module and visibility: %v", err)
	}
	_ = file.Close()
	if _, err := OpenPreview(ctx, 100, 1, 1000, 3000, "../secret"); err == nil {
		t.Fatal("preview accepted traversal")
	}
	if err := database.Table(dal.AccountTable).Where("id=100").Update("auth_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPreview(ctx, 100, 1, 1000, 3000, ""); err == nil {
		t.Fatal("revoked admin session previewed content")
	}
}

func TestBannedOwnerAndClosedSiteCannotPublish(t *testing.T) {
	database := moderationDatabase(t)
	if err := database.Table(dal.AccountTable).Where("id=200").Update("status", model.AccountBanned).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := userweb.Default.GetPublishedProject("private-test"); err == nil {
		t.Fatal("banned owner's content remains readable")
	}
	conf := config.Runtime().WebProjects
	if _, err := userweb.Default.PublishRelease(&conf, 200, 1000, 3000, 1, testWebActor()); err == nil {
		t.Fatal("banned owner published")
	}
	if err := database.Table(dal.AccountTable).Where("id=200").Update("status", model.AccountActive).Error; err != nil {
		t.Fatal(err)
	}
	values := config.DefaultRuntimeValues(config.Global())["web_projects"]
	values["enabled"] = false
	raw, _ := json.Marshal(values)
	hash := sha256.Sum256(raw)
	if err := database.Table(dal.SiteConfigCurrentTable).Where("namespace='web_projects'").Updates(map[string]interface{}{"values_json": string(raw), "values_sha256": hash[:]}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: "new", Slug: "new-project", AccessMode: "owner"}); err == nil {
		t.Fatal("closed site accepted project creation")
	}
}

func TestCleanupKeepsFrozenDeadlineAndChecksLiveCleanupSwitch(t *testing.T) {
	database := moderationDatabase(t)
	now := time.Now()
	deleted := now.Add(-30 * 24 * time.Hour)
	future := now.Add(4 * 24 * time.Hour)
	if err := database.Table(dal.WebProjectTable).Where("id=1000").Updates(map[string]interface{}{"status": model.WebProjectStatusDeleted, "deleted_at": deleted, "purge_after": future}).Error; err != nil {
		t.Fatal(err)
	}
	conf := config.Runtime().WebProjects
	conf.DeleteRetentionDays = 1
	path := filepath.Join(conf.StorageRoot, "200/upload/html/1000/releases/3000/content/index.html")
	if err := webservice.CleanupExpiredProjects(&conf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("lower retention removed content before its frozen deadline")
	}
	past := now.Add(-time.Hour)
	if err := database.Table(dal.WebProjectTable).Where("id=1000").Update("purge_after", past).Error; err != nil {
		t.Fatal(err)
	}
	values := config.DefaultRuntimeValues(config.Global())["web_projects"]
	for _, enabled := range []bool{false, true} {
		values["cleanup_enabled"] = enabled
		raw, _ := json.Marshal(values)
		hash := sha256.Sum256(raw)
		if err := database.Table(dal.SiteConfigCurrentTable).Where("namespace='web_projects'").Updates(map[string]interface{}{"values_json": string(raw), "values_sha256": hash[:]}).Error; err != nil {
			t.Fatal(err)
		}
		if err := webservice.CleanupExpiredProjects(&conf); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(path)
		if !enabled && err != nil {
			t.Fatal("disabled cleanup removed an expired page")
		}
		if enabled && !os.IsNotExist(err) {
			t.Fatal("reenabled cleanup did not remove the expired page")
		}
	}
}

func TestRevokedUploadSessionReturnsAuthenticationFailure(t *testing.T) {
	database := moderationDatabase(t)
	if err := database.Table(dal.AccountTable).Where("id=200").Update("auth_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	conf := config.Runtime().WebProjects
	_, err := userweb.Default.PublishRelease(&conf, 200, 1000, 3000, 1, testWebActor())
	if !errors.Is(err, webservice.ErrUnauthorized) {
		t.Fatalf("revoked session must preserve authentication error, got %v", err)
	}
}

func TestFileIdentityModeKeepsLegacyWebTableCompatible(t *testing.T) {
	database := moderationDatabase(t)
	for _, column := range []string{"moderation_status", "moderation_reason", "moderated_by", "moderated_at", "purge_after"} {
		if err := database.Exec("ALTER TABLE web_project DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	conf := config.Global()
	conf.IdentitySource = "file"
	conf.ConfigSource = "file"
	config.SetGlobalConfig(conf)
	created, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: "legacy", Slug: "legacy-example", AccessMode: "owner"})
	if err != nil {
		t.Fatalf("legacy create queried new moderation columns: %v", err)
	}
	id, err := webservice.ParsePositiveID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userweb.Default.GetOwnedProject(200, id); err != nil {
		t.Fatalf("legacy read queried new columns: %v", err)
	}
	deleted, err := userweb.Default.ChangeProjectStatus(200, id, 1, "delete", 7)
	if err != nil {
		t.Fatalf("legacy delete wrote purge_after: %v", err)
	}
	if _, err := userweb.Default.ChangeProjectStatus(200, id, deleted.Revision, "restore", 7); err != nil {
		t.Fatalf("legacy restore wrote purge_after: %v", err)
	}
}

func setResourceQuota(t *testing.T, database *gorm.DB, changes map[string]interface{}) config.WebProjectsConfig {
	t.Helper()
	values := config.DefaultRuntimeValues(config.Global())["web_projects"]
	for key, value := range changes {
		values[key] = value
	}
	normalized, err := config.ValidateValues(config.Global(), "web_projects", values)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(normalized)
	hash := sha256.Sum256(raw)
	if err := database.Table(dal.SiteConfigCurrentTable).Where("namespace='web_projects'").Updates(map[string]interface{}{"values_json": string(raw), "values_sha256": hash[:]}).Error; err != nil {
		t.Fatal(err)
	}
	if err := siteconfig.New(database, config.Global()).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return config.Runtime().WebProjects
}

func TestConcurrentProjectCreationCannotExceedUserQuota(t *testing.T) {
	database := moderationDatabase(t)
	setResourceQuota(t, database, map[string]interface{}{"max_projects_per_user": 2})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, slug := range []string{"quota-project-a", "quota-project-b"} {
		group.Add(1)
		go func(slug string) {
			defer group.Done()
			_, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: slug, Slug: slug, AccessMode: "owner"}, testWebActor())
			results <- err
		}(slug)
	}
	group.Wait()
	close(results)
	passed, limited := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, webservice.ErrRateLimited) {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || limited != 1 {
		t.Fatalf("quota race results passed=%d limited=%d", passed, limited)
	}
	var count int64
	if err := database.Table(dal.WebProjectTable).Where("owner_user_id=200 AND status<>?", model.WebProjectStatusDeleted).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("project quota exceeded: %d", count)
	}
}

func TestConcurrentCrossProjectUploadsShareOneUserBudget(t *testing.T) {
	database := moderationDatabase(t)
	ids := []int64{}
	for _, slug := range []string{"space-project-a", "space-project-b"} {
		project, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: slug, Slug: slug, AccessMode: "owner"}, testWebActor())
		if err != nil {
			t.Fatal(err)
		}
		id, err := webservice.ParsePositiveID(project.ID)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	conf := setResourceQuota(t, database, map[string]interface{}{"max_user_bytes": 12, "max_project_bytes": 8, "max_expanded_bytes": 8, "max_upload_bytes": 8, "max_file_bytes": 8})
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range ids {
		group.Add(1)
		go func(id int64) {
			defer group.Done()
			_, err := userweb.Default.UploadRelease(&conf, 200, id, "page.html", "", "", strings.NewReader("hello"), testWebActor())
			results <- err
		}(id)
	}
	group.Wait()
	close(results)
	passed, limited := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, webservice.ErrTooLarge) {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || limited != 1 {
		t.Fatalf("shared byte quota race passed=%d limited=%d", passed, limited)
	}
	var used int64
	if err := database.Table(dal.WebProjectReleaseTable).Select("COALESCE(SUM(total_bytes),0)").Where("uploaded_by=200").Scan(&used).Error; err != nil {
		t.Fatal(err)
	}
	if used != 9 {
		t.Fatalf("unexpected committed bytes: %d", used)
	}
}

func TestQuotaFailureDoesNotPruneAndDeletingBytesRemainCharged(t *testing.T) {
	database := moderationDatabase(t)
	now := time.Now()
	old := &model.WebProjectRelease{ID: 3001, ProjectID: 1000, UploadedBy: 200, StorageKey: "200/upload/html/1000/releases/3001/content", Status: model.WebProjectReleaseReady, EntryFile: "index.html", SHA256: strings.Repeat("b", 64), FileCount: 1, TotalBytes: 4, Extra: "{}", CreateTime: now, UpdateTime: now}
	if err := dal.CreateWebProjectRelease(database, old); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(config.Global().WebProjects.StorageRoot, old.StorageKey)
	if err := os.MkdirAll(oldPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "index.html"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	conf := setResourceQuota(t, database, map[string]interface{}{"max_user_bytes": 8, "max_project_bytes": 8, "max_expanded_bytes": 8, "max_upload_bytes": 8, "max_file_bytes": 8, "max_releases": 2})
	for _, status := range []model.WebProjectReleaseStatus{model.WebProjectReleaseReady, model.WebProjectReleaseDeleting} {
		if err := database.Table(dal.WebProjectReleaseTable).Where("id=3001").Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := userweb.Default.UploadRelease(&conf, 200, 1000, "page.html", "", "", strings.NewReader("next"), testWebActor()); !errors.Is(err, webservice.ErrTooLarge) {
			t.Fatalf("quota did not reject existing %s bytes: %v", status.String(), err)
		}
		stored, err := dal.QueryWebProjectRelease(1000, 3001)
		if err != nil || stored == nil || stored.Status != status {
			t.Fatal("rejected quota mutated an existing release")
		}
		if _, err := os.Stat(filepath.Join(oldPath, "index.html")); err != nil {
			t.Fatal("rejected quota deleted an existing release")
		}
	}
	old.Status = model.WebProjectReleaseDeleting
	if err := webservice.RemoveRetiredReleases(&conf, []*model.WebProjectRelease{old}); err != nil {
		t.Fatal(err)
	}
	if _, err := userweb.Default.UploadRelease(&conf, 200, 1000, "page.html", "", "", strings.NewReader("next"), testWebActor()); err != nil {
		t.Fatalf("completed physical cleanup did not release quota: %v", err)
	}
}

func TestDeletedProjectsReleaseCountButRestoreRequiresAFreeSlot(t *testing.T) {
	database := moderationDatabase(t)
	setResourceQuota(t, database, map[string]interface{}{"max_projects_per_user": 1})
	deleted, err := userweb.Default.ChangeProjectStatus(200, 1000, 1, "delete", 7, testWebActor())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: "replacement", Slug: "quota-replacement", AccessMode: "owner"}, testWebActor()); err != nil {
		t.Fatal(err)
	}
	if _, err := userweb.Default.ChangeProjectStatus(200, 1000, deleted.Revision, "restore", 7, testWebActor()); !errors.Is(err, webservice.ErrRateLimited) {
		t.Fatalf("restore bypassed active project cap: %v", err)
	}
	original, err := dal.QueryWebProjectByID(1000)
	if err != nil || original == nil || original.Status != model.WebProjectStatusDeleted {
		t.Fatal("rejected restore changed the deleted project")
	}
	if _, err := os.Stat(filepath.Join(config.Global().WebProjects.StorageRoot, "200/upload/html/1000/releases/3000/content/index.html")); err != nil {
		t.Fatal("rejected restore removed retained content")
	}
}

func TestLibraryWriteRejectsChangedApplicationSnapshot(t *testing.T) {
	cases := []string{"rotate", "disable-enable", "owner-mismatch", "token-expiry"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			database := moderationDatabase(t)
			auth := config.DefaultRuntimeValues(config.Global())["auth"]
			auth["applications_enabled"] = true
			raw, _ := json.Marshal(auth)
			hash := sha256.Sum256(raw)
			if err := database.Table(dal.SiteConfigCurrentTable).Where("namespace='auth'").Updates(map[string]interface{}{"values_json": string(raw), "values_sha256": hash[:]}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Table(dal.AccountTable).Where("id=200").Update("library_enabled", true).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			application := &model.Application{ID: 4000, OwnerUserID: 200, Name: "write snapshot", AccessKey: "ak_cq_" + strings.Repeat("a", 22), SecretDigest: make([]byte, 32), Scopes: []string{"library:read", "library:write"}, Status: model.ApplicationStatusEnabled, Revision: 1, SecretVersion: 1, ExpiresAt: now.Add(time.Hour), CreateTime: now, UpdateTime: now}
			if err := dal.CreateApplication(database, application); err != nil {
				t.Fatal(err)
			}
			actor := &utils.Principal{Kind: "application", UserID: 200, AuthVersion: 1, ApplicationID: 4000, ApplicationRevision: 1, SecretVersion: 1, TokenExpiresAt: now.Add(time.Minute), Scopes: append([]string(nil), application.Scopes...)}
			if err := database.Transaction(func(tx *gorm.DB) error { return accounts.RequireLibraryWriteTx(tx, actor) }); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "rotate":
				if err := database.Table(dal.ApplicationTable).Where("id=4000").Update("secret_version", 2).Error; err != nil {
					t.Fatal(err)
				}
			case "disable-enable":
				if err := database.Table(dal.ApplicationTable).Where("id=4000").Update("revision", 3).Error; err != nil {
					t.Fatal(err)
				}
			case "owner-mismatch":
				if err := database.Table(dal.ApplicationTable).Where("id=4000").Update("owner_user_id", 100).Error; err != nil {
					t.Fatal(err)
				}
			case "token-expiry":
				actor.TokenExpiresAt = now.Add(-time.Second)
			}
			if err := database.Transaction(func(tx *gorm.DB) error { return accounts.RequireLibraryWriteTx(tx, actor) }); err == nil {
				t.Fatal("in-flight library write accepted changed " + name)
			}
		})
	}
}

func testWebActor() *utils.Principal {
	return &utils.Principal{Kind: "user", UserID: 200, AuthVersion: 1, TokenExpiresAt: time.Now().Add(time.Hour)}
}

func TestUserWriteRejectsMissingOrExpiredSessionSnapshot(t *testing.T) {
	database := moderationDatabase(t)
	if err := database.Table(dal.AccountTable).Where("id=200").Update("library_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, expiry := range []time.Time{{}, time.Now().Add(-time.Second)} {
		actor := testWebActor()
		actor.TokenExpiresAt = expiry
		if _, err := userweb.Default.CreateProject(200, webservice.CreateProjectInput{Name: "expired", Slug: "expired-session", AccessMode: "owner"}, actor); !errors.Is(err, webservice.ErrUnauthorized) {
			t.Fatalf("web write accepted stale user session: %v", err)
		}
		if err := database.Transaction(func(tx *gorm.DB) error { return accounts.RequireLibraryWriteTx(tx, actor) }); !errors.Is(err, webservice.ErrUnauthorized) {
			t.Fatalf("library write accepted stale user session: %v", err)
		}
	}
}
