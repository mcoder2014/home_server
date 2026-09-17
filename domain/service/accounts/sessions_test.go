package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sessionTestDatabase only writes to the named disposable session fixture. It
// does not require or read production configuration or credentials.
func sessionTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ACCOUNTS_SESSIONS_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNTS_SESSIONS_TEST_DSN to the dedicated synthetic session database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "home_server_account_sessions_test_") {
		t.Fatal("unsafe session test database")
	}
	parsed.ParseTime = true
	parsed.Loc = time.Local
	parsed.MultiStatements = false
	if err := db.InitDatabase(parsed.FormatDSN()); err != nil {
		t.Fatal("session test database initialization failed")
	}
	database := db.MasterDB()
	database.Logger = logger.Discard
	old := config.Global()
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	snapshot := config.Runtime()
	snapshot.AccountPolicy.BcryptCost = 4
	// General session regressions opt into a larger synthetic-only limit.
	snapshot.AccountPolicy.MaxActiveSessions = 100
	if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		config.SetGlobalConfig(old)
		connection, _ := database.DB()
		connection.Close()
	})
	for _, table := range []struct {
		name  string
		value interface{}
	}{
		{dal.AccountTable, &model.UserAccount{}}, {dal.LoginAliasTable, &model.LoginAlias{}},
		{dal.TableUserToken, &model.AccountSession{}}, {dal.ApplicationTable, &model.Application{}},
		{dal.WebProjectTable, &model.WebProject{}}, {dal.InvitationTable, &model.UserInvitation{}},
		{dal.AdminAuditTable, &model.AdminAuditLog{}},
	} {
		if err := database.Table(table.name).AutoMigrate(table.value); err != nil {
			t.Fatal(err)
		}
		if err := database.Exec("DELETE FROM " + table.name).Error; err != nil {
			t.Fatal(err)
		}
	}
	// AutoMigrate does not install the production migration's required range index.
	if !database.Migrator().HasIndex(dal.TableUserToken, "idx_login_token_effective_range") {
		if err := database.Exec("ALTER TABLE login_token ADD KEY idx_login_token_effective_range (user_id,auth_version,is_expired,expire_time,create_time,id)").Error; err != nil {
			t.Fatal(err)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(reviewPassword), 4)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{8101, 8102, 8103} {
		user := &model.UserAccount{ID: id, Username: "session" + strconv.FormatInt(id, 10), UsernameKey: "session" + strconv.FormatInt(id, 10), DisplayName: "Synthetic session", PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleUser, WebDAVPermission: model.WebDAVNone, AuthVersion: 1, Revision: 1, InviteEligibleAt: now, Source: "test", CreateTime: now, UpdateTime: now}
		if id == 8103 {
			user.Role = model.RoleAdmin
		}
		if err := database.Transaction(func(tx *gorm.DB) error { return dal.InsertAccount(tx, user) }); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func sessionTestToken(id int64) string {
	return fmt.Sprintf("us_cq_synthetic_session_%d", id)
}

func sessionTestInsert(t *testing.T, database *gorm.DB, id, userID int64, purpose string, create, expire time.Time) *model.AccountSession {
	t.Helper()
	digest := sha256.Sum256([]byte(sessionTestToken(id)))
	session := &model.AccountSession{ID: id, UserID: userID, TokenDigest: digest[:], AuthVersion: 1, Purpose: purpose, AuthenticatedAt: create, CreateTime: create, UpdateTime: create, ExpireTime: expire}
	if err := dal.InsertAccountSession(database, session); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSessionUserCountsExposeOnlyAuthenticatableSessions(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{8201, 8202, 8203} {
		sessionTestInsert(t, database, id, 8101, model.SessionUser, now, now.Add(time.Hour))
	}
	sessionTestInsert(t, database, 8204, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(-time.Second))
	sessionTestInsert(t, database, 8205, 8101, "future_purpose", now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8206, 8101, model.SessionUser, now, now.Add(time.Hour))
	if err := database.Table(dal.TableUserToken).Where("id = ?", 8206).Update("auth_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	temporaryExpiry := now.Add(5 * time.Minute)
	if err := database.Table(dal.AccountTable).Where("id = ?", 8102).Updates(map[string]interface{}{"must_change_password": true, "password_expires_at": temporaryExpiry}).Error; err != nil {
		t.Fatal(err)
	}
	sessionTestInsert(t, database, 8211, 8102, model.SessionPasswordChange, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8212, 8102, model.SessionUser, now, now.Add(time.Hour))
	page, err := ListUsers(context.Background(), dal.AccountFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Items {
		switch item["id"] {
		case "8101":
			if item["active_session_count"] != float64(3) || item["restricted_session_count"] != float64(0) {
				t.Fatalf("ordinary account counts must exclude unusable sessions: active=%v restricted=%v", item["active_session_count"], item["restricted_session_count"])
			}
		case "8102":
			if item["active_session_count"] != float64(1) || item["restricted_session_count"] != float64(1) {
				t.Fatalf("temporary account count must contain only its valid restricted session: active=%v restricted=%v", item["active_session_count"], item["restricted_session_count"])
			}
		}
	}
}

func TestSessionMetadataParsesBoundedUAAndCanonicalIP(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
	metadata := NewSessionMetadata("2001:0db8:0000:0000:0000:0000:0000:0001", ua, "web")
	if metadata.LoginIP != "2001:db8::1" || !strings.Contains(metadata.ClientName, "Chrome") || !strings.Contains(metadata.OSName, "Windows") || metadata.DeviceType != "desktop" || metadata.LoginSource != "web" || metadata.UserAgent != ua {
		t.Fatalf("login descriptions must come from bounded login metadata: %+v", metadata)
	}
	metadata = NewSessionMetadata("not-an-ip", strings.Repeat("猫", 1000), "forged_source")
	if len(metadata.UserAgent) > 1024 || !utf8.ValidString(metadata.UserAgent) || len(metadata.ClientName) > 128 || len(metadata.OSName) > 128 || metadata.LoginIP != "unknown" || metadata.LoginSource != "unknown" {
		t.Fatal("unrecognized metadata must remain valid and bounded")
	}
	metadata = NewSessionMetadata("", "", "legacy")
	if metadata.DeviceType != "unknown" || metadata.LoginSource != "legacy" {
		t.Fatal("missing UA must not invent a device")
	}
}

func TestSessionLoginAndVerifiedIssuanceRetainMetadata(t *testing.T) {
	database := sessionTestDatabase(t)
	metadata := NewSessionMetadata("198.51.100.8", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "web")
	user, token, session, err := Login(context.Background(), "session8101", reviewPassword, metadata)
	if err != nil || user == nil || session == nil {
		t.Fatalf("metadata must not prevent valid login: %v", err)
	}
	_, stored, err := CheckSession(context.Background(), token, false)
	if err != nil || stored.LoginIP != metadata.LoginIP || stored.UserAgent != metadata.UserAgent || stored.DeviceType != "mobile" || stored.LoginSource != "web" {
		t.Fatalf("web session must retain login metadata: %v", err)
	}
	metadata.LoginSource = "legacy"
	legacyToken, err := IssueVerifiedSession(context.Background(), user.ID, user.AuthVersion, metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, legacy, err := CheckSession(context.Background(), legacyToken, false)
	if err != nil || legacy.LoginSource != "legacy" || legacy.LoginIP != metadata.LoginIP || legacy.UserAgent != metadata.UserAgent {
		t.Fatal("verified legacy session lost login metadata")
	}
	var count int64
	if err := database.Table(dal.TableUserToken).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("each issuance must create exactly one session")
	}
}

func TestSessionListUsesCreateTimeAndIDCursorAndProtectsCurrent(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	expire := now.Add(time.Hour)
	sessionTestInsert(t, database, 8200, 8101, model.SessionUser, now, expire)
	sessionTestInsert(t, database, 8299, 8101, model.SessionUser, now.Add(-2*time.Minute), expire)
	sessionTestInsert(t, database, 8202, 8101, model.SessionUser, now.Add(-time.Minute), expire)
	sessionTestInsert(t, database, 8203, 8101, model.SessionUser, now.Add(-time.Minute), expire)
	sessionTestInsert(t, database, 8204, 8101, model.SessionUser, now.Add(-time.Minute), now)
	sessionTestInsert(t, database, 8205, 8101, "unknown", now, expire)
	sessionTestInsert(t, database, 8206, 8101, model.SessionUser, now, expire)
	if err := database.Table(dal.TableUserToken).Where("id = ?", 8206).Update("is_expired", 1).Error; err != nil {
		t.Fatal(err)
	}
	page, err := ListSessions(context.Background(), sessionTestToken(8200), SessionFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.CurrentSession == nil || page.CurrentSession.ID != "8200" || page.TotalCount != 4 || page.RestrictedCount != 0 || len(page.Items) != 2 || page.Items[0].ID != "8203" || page.Items[1].ID != "8202" || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("current session must be separate and sorted by create_time,id: %+v", page)
	}
	last, err := ListSessions(context.Background(), sessionTestToken(8200), SessionFilter{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != "8299" || last.HasMore || last.NextCursor != "" || last.TotalCount != page.TotalCount {
		t.Fatalf("cursor must use both create time and ID: %+v %v", last, err)
	}
	if page.CurrentSession.RemainingSeconds != int64(page.CurrentSession.ExpireTime.Sub(page.ServerTime)/time.Second) {
		t.Fatal("remaining lifetime must use the response server time")
	}
	raw, _ := json.Marshal(page)
	for _, private := range []string{"token_digest", "auth_version", "password_hash"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("session list leaked %s", private)
		}
	}
}

func TestSessionRestrictedExpiryAndCountsMatchAuthentication(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8300, 8103, model.SessionUser, now, now.Add(time.Hour))
	passwordExpiry := now.Add(3 * time.Minute)
	if err := database.Table(dal.AccountTable).Where("id = ?", 8102).Updates(map[string]interface{}{"must_change_password": true, "password_expires_at": passwordExpiry}).Error; err != nil {
		t.Fatal(err)
	}
	sessionTestInsert(t, database, 8311, 8102, model.SessionPasswordChange, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8312, 8102, model.SessionUser, now, now.Add(time.Hour))
	page, err := AdminUserSessions(context.Background(), sessionTestToken(8300), 8102, SessionFilter{Limit: 20})
	if err != nil || page.TotalCount != 1 || page.RestrictedCount != 1 || page.CurrentSession != nil || len(page.Items) != 1 || page.Items[0].ID != "8311" || !page.Items[0].ExpireTime.Equal(passwordExpiry) {
		t.Fatalf("administrator must see only the authenticatable restricted session: %+v %v", page, err)
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(8311), true); err != nil {
		t.Fatal("listed restricted session must authenticate for password change")
	}
	if _, err := ListSessions(context.Background(), sessionTestToken(8311), SessionFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("restricted session must not gain access to login management")
	}
	if err := database.Table(dal.AccountTable).Where("id = ?", 8102).Update("password_expires_at", now).Error; err != nil {
		t.Fatal(err)
	}
	page, err = AdminUserSessions(context.Background(), sessionTestToken(8300), 8102, SessionFilter{})
	if err != nil || page.TotalCount != 0 || page.RestrictedCount != 0 || len(page.Items) != 0 {
		t.Fatal("expired initial password must remove all restricted sessions from counts")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(8311), true); err == nil {
		t.Fatal("expired restricted session unexpectedly authenticated")
	}
}

func TestSessionBatchRevokeIsAtomicAndIdempotent(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8400, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8401, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8402, 8101, model.SessionUser, now, now.Add(-time.Minute))
	sessionTestInsert(t, database, 8499, 8102, model.SessionUser, now, now.Add(time.Hour))
	for _, ids := range [][]string{{"8401", "8499"}, {"8401", "999999999"}} {
		if _, err := RevokeSessions(context.Background(), sessionTestToken(8400), ids); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("invisible target must reject whole batch: %v", err)
		}
		if _, _, err := CheckSession(context.Background(), sessionTestToken(8401), false); err != nil {
			t.Fatal("rejected mixed batch partially revoked an owned session")
		}
	}
	if _, err := RevokeSessions(context.Background(), sessionTestToken(8400), []string{"8401", "8400"}); !errors.Is(err, apperrors.ErrInvalid) {
		t.Fatal("current session must have its own logout action")
	}
	result, err := RevokeSessions(context.Background(), sessionTestToken(8400), []string{"8401", "8401", "8402"})
	if err != nil || result.RevokedCount != 1 || result.AlreadyInactiveCount != 1 {
		t.Fatalf("batch must deduplicate IDs and count expired owned rows: %+v %v", result, err)
	}
	result, err = RevokeSessions(context.Background(), sessionTestToken(8400), []string{"8401", "8402"})
	if err != nil || result.RevokedCount != 0 || result.AlreadyInactiveCount != 2 {
		t.Fatalf("same owned targets must be safely retryable: %+v %v", result, err)
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(8400), false); err != nil {
		t.Fatal("batch logout revoked the current session")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(8499), false); err != nil {
		t.Fatal("batch logout modified another account")
	}
}

func TestSessionRevokeOthersRetainsCurrentVersionAndApplications(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8500, 8101, model.SessionUser, now, now.Add(time.Hour))
	for id := int64(8501); id <= 8525; id++ {
		sessionTestInsert(t, database, id, 8101, model.SessionUser, now, now.Add(time.Hour))
	}
	sessionTestInsert(t, database, 8599, 8101, model.SessionUser, now, now.Add(-time.Minute))
	app := &model.Application{ID: 8600, OwnerUserID: 8101, Name: "Synthetic application", AccessKey: "synthetic_key", SecretDigest: []byte("synthetic_digest"), Scopes: []string{"library:read"}, Status: model.ApplicationStatusEnabled, Revision: 1, SecretVersion: 1, ExpiresAt: now.Add(time.Hour), CreateTime: now, UpdateTime: now}
	if err := database.Table(dal.ApplicationTable).Create(app).Error; err != nil {
		t.Fatal(err)
	}
	result, err := RevokeOtherSessions(context.Background(), sessionTestToken(8500))
	if err != nil || result.RevokedCount != 25 {
		t.Fatalf("revoke others must cover sessions beyond a loaded page: %+v %v", result, err)
	}
	user, _, err := CheckSession(context.Background(), sessionTestToken(8500), false)
	if err != nil || user.AuthVersion != 1 {
		t.Fatal("selective logout must preserve current session and auth version")
	}
	if err := database.Table(dal.ApplicationTable).Select("status", "revision").Where("id = ?", app.ID).Take(app).Error; err != nil || app.Status != model.ApplicationStatusEnabled || app.Revision != 1 {
		t.Fatal("selective logout must not disable application credentials")
	}
	page, err := ListSessions(context.Background(), sessionTestToken(8500), SessionFilter{})
	if err != nil || page.TotalCount != 1 || len(page.Items) != 0 {
		t.Fatal("only the current login should remain")
	}
}

func TestSessionServicesRevalidateActingSessionAfterMiddleware(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8700, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8701, 8101, model.SessionUser, now, now.Add(time.Hour))
	ctx := WithReadSnapshot(context.Background())
	if _, _, err := CheckSession(ctx, sessionTestToken(8700), false); err != nil {
		t.Fatal(err)
	}
	if err := Logout(context.Background(), sessionTestToken(8700)); err != nil {
		t.Fatal(err)
	}
	if _, err := ListSessions(ctx, sessionTestToken(8700), SessionFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("list trusted stale middleware authentication")
	}
	if _, err := RevokeSessions(ctx, sessionTestToken(8700), []string{"8701"}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("batch write trusted an already revoked acting session")
	}
	if _, err := RevokeOtherSessions(ctx, sessionTestToken(8700)); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("logout others trusted an already revoked acting session")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(8701), false); err != nil {
		t.Fatal("rejected stale request changed the target session")
	}
}

func TestSessionAdministratorReadRequiresLiveAdministratorSession(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8800, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 8803, 8103, model.SessionUser, now, now.Add(time.Hour))
	if _, err := AdminUserSessions(context.Background(), sessionTestToken(8800), 8102, SessionFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("ordinary user could enumerate another account's sessions")
	}
	ctx := WithReadSnapshot(context.Background())
	if _, _, err := CheckSession(ctx, sessionTestToken(8803), false); err != nil {
		t.Fatal(err)
	}
	if err := database.Table(dal.AccountTable).Where("id = ?", 8103).Update("role", model.RoleUser).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := AdminUserSessions(ctx, sessionTestToken(8803), 8102, SessionFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("administrator read trusted the stale request user cache")
	}
}

func TestSessionInputLimitsRejectMalformedIDsAndCursor(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 8900, 8101, model.SessionUser, now, now.Add(time.Hour))
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "8901"
	}
	for _, ids := range [][]string{nil, {}, {"0"}, {"-0"}, {"-"}, {"--1"}, {"-+1"}, {"+1"}, {"1e3"}, {"9223372036854775808"}, {"-9223372036854775809"}, tooMany} {
		if _, err := RevokeSessions(context.Background(), sessionTestToken(8900), ids); !errors.Is(err, apperrors.ErrInvalid) {
			t.Fatalf("invalid selection was accepted: %v", ids)
		}
	}
	zeroTimeCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"1","create_time":"0001-01-01T00:00:00Z"}`))
	missingTimeCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"1"}`))
	for _, filter := range []SessionFilter{{Limit: -1}, {Limit: 101}, {Cursor: "not-a-cursor"}, {Cursor: strings.Repeat("a", 1000)}, {Cursor: zeroTimeCursor}, {Cursor: missingTimeCursor}} {
		if _, err := ListSessions(context.Background(), sessionTestToken(8900), filter); !errors.Is(err, apperrors.ErrInvalid) {
			t.Fatal("invalid pagination was accepted")
		}
	}
}

func TestSessionConcurrentRevokeIsIdempotentAndPreservesCurrent(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionTestInsert(t, database, 9000, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9001, 8101, model.SessionUser, now, now.Add(time.Hour))
	var group sync.WaitGroup
	results := make(chan *SessionRevokeResult, 2)
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := RevokeSessions(context.Background(), sessionTestToken(9000), []string{"9001"})
			results <- result
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent same-target revoke must serialize without errors: %v", err)
		}
	}
	var revoked, inactive int64
	for result := range results {
		revoked += result.RevokedCount
		inactive += result.AlreadyInactiveCount
	}
	if revoked != 1 || inactive != 1 {
		t.Fatal("concurrent revoke did not preserve idempotent counts")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(9000), false); err != nil {
		t.Fatal("concurrent revokes affected current session")
	}
}

func TestSessionEffectiveCountsMatchAuthenticationOnAllBoundaries(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	sessionTestInsert(t, database, 9100, 8103, model.SessionUser, now, now.Add(time.Hour))
	for _, test := range []struct {
		name                       string
		status, purpose            string
		mustChange, emptyDigest    bool
		oldVersion, revoked        bool
		valid, restricted          bool
		sessionExpiry, passwordTTL time.Duration
	}{
		{name: "ordinary", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: time.Hour, valid: true},
		{name: "revoked", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: time.Hour, revoked: true},
		{name: "expired", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: -time.Second},
		{name: "old_version", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: time.Hour, oldVersion: true},
		{name: "empty_digest", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: time.Hour, emptyDigest: true},
		{name: "banned", status: model.AccountBanned, purpose: model.SessionUser, sessionExpiry: time.Hour},
		{name: "deleted", status: model.AccountDeleted, purpose: model.SessionUser, sessionExpiry: time.Hour},
		{name: "unknown_purpose", status: model.AccountActive, purpose: "unexpected", sessionExpiry: time.Hour},
		{name: "ordinary_during_initial_password", status: model.AccountActive, purpose: model.SessionUser, sessionExpiry: time.Hour, mustChange: true, passwordTTL: time.Hour},
		{name: "restricted", status: model.AccountActive, purpose: model.SessionPasswordChange, sessionExpiry: time.Hour, mustChange: true, passwordTTL: time.Minute, valid: true, restricted: true},
		{name: "restricted_after_initial_expiry", status: model.AccountActive, purpose: model.SessionPasswordChange, sessionExpiry: time.Hour, mustChange: true, passwordTTL: -time.Second},
		{name: "restricted_without_deadline", status: model.AccountActive, purpose: model.SessionPasswordChange, sessionExpiry: time.Hour, mustChange: true},
		{name: "restricted_without_mustchange", status: model.AccountActive, purpose: model.SessionPasswordChange, sessionExpiry: time.Hour, passwordTTL: time.Hour, valid: true, restricted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := database.Table(dal.TableUserToken).Where("user_id = ?", 8102).Delete(&model.AccountSession{}).Error; err != nil {
				t.Fatal(err)
			}
			fields := map[string]interface{}{"status": test.status, "must_change_password": test.mustChange, "password_expires_at": nil}
			if test.passwordTTL != 0 {
				fields["password_expires_at"] = now.Add(test.passwordTTL)
			}
			if err := database.Table(dal.AccountTable).Where("id = ?", 8102).Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			session := sessionTestInsert(t, database, 9102, 8102, test.purpose, now, now.Add(test.sessionExpiry))
			if test.revoked {
				session.IsExpired = 1
			}
			if test.oldVersion {
				session.AuthVersion = 2
			}
			if test.emptyDigest {
				session.TokenDigest = []byte{}
			}
			if err := database.Table(dal.TableUserToken).Where("id = ?", session.ID).Select("is_expired", "auth_version", "token_digest").Updates(session).Error; err != nil {
				t.Fatal(err)
			}
			page, err := AdminUserSessions(context.Background(), sessionTestToken(9100), 8102, SessionFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if (page.TotalCount == 1) != test.valid || (page.RestrictedCount == 1) != test.restricted || (len(page.Items) == 1) != test.valid {
				t.Fatalf("counts disagree with effective conditions: %+v", page)
			}
			_, _, err = CheckSession(context.Background(), sessionTestToken(9102), true)
			if (err == nil) != test.valid {
				t.Fatalf("authentication disagrees with listed validity: %v", err)
			}
		})
	}
}

func TestSessionConcurrentLoginAndRevokeOthersFollowUserLock(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	sessionTestInsert(t, database, 9200, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9201, 8101, model.SessionUser, now, now.Add(time.Hour))
	for i := 0; i < 12; i++ {
		if err := database.Table(dal.TableUserToken).Where("id = ?", 9201).Update("is_expired", 0).Error; err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		issued := make(chan string, 1)
		issueErrors := make(chan error, 1)
		revoked := make(chan *SessionRevokeResult, 1)
		revokeErrors := make(chan error, 1)
		go func() {
			<-start
			token, err := IssueVerifiedSession(context.Background(), 8101, 1)
			issued <- token
			issueErrors <- err
		}()
		go func() {
			<-start
			result, err := RevokeOtherSessions(context.Background(), sessionTestToken(9200))
			revoked <- result
			revokeErrors <- err
		}()
		close(start)
		token, result := <-issued, <-revoked
		if err := <-issueErrors; err != nil {
			t.Fatalf("concurrent issue failed: %v", err)
		}
		if err := <-revokeErrors; err != nil {
			t.Fatalf("concurrent revoke failed: %v", err)
		}
		_, _, checkError := CheckSession(context.Background(), token, false)
		if checkError == nil && result.RevokedCount != 1 || errors.Is(checkError, apperrors.ErrUnauthorized) && result.RevokedCount != 2 {
			t.Fatalf("new login did not match the user-lock order: revoked=%d active=%v", result.RevokedCount, checkError == nil)
		}
		if checkError != nil && !errors.Is(checkError, apperrors.ErrUnauthorized) {
			t.Fatal(checkError)
		}
		if _, _, err := CheckSession(context.Background(), sessionTestToken(9201), false); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("previously existing other login survived revoke others")
		}
		if err := Logout(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionAuditUsesCurrentIdentityAndPreservesLegacyActorName(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	if err := database.Table(dal.AccountTable).Where("id = ?", 8103).Updates(map[string]interface{}{"display_name": "当前昵称", "avatar_version": 7}).Error; err != nil {
		t.Fatal(err)
	}
	log := &model.AdminAuditLog{ID: 9300, ActorUserID: 8103, Action: "synthetic-action", TargetType: "user", TargetID: 8101, BeforeSummary: `{"display_name":"历史昵称"}`, AfterSummary: `{}`, Reason: "Synthetic fixture", Result: "success", CreateTime: now}
	if err := database.Table(dal.AdminAuditTable).Create(log).Error; err != nil {
		t.Fatal(err)
	}
	page, err := ListAudit(context.Background(), 0, 20, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	items := page["items"].([]AuditView)
	if len(items) != 1 || items[0].ActorUser == nil || items[0].ActorUser.DisplayName != "当前昵称" || items[0].ActorUser.AvatarURL != "/api/account/avatars/8103/7" || items[0].ActorUserName != "session8103" || items[0].Before["display_name"] != "历史昵称" {
		t.Fatal("audit must show current identity while retaining legacy name and historical evidence")
	}
	if err := database.Table(dal.AccountTable).Where("id = ?", 8103).Update("status", model.AccountDeleted).Error; err != nil {
		t.Fatal(err)
	}
	page, err = ListAudit(context.Background(), 0, 20, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	items = page["items"].([]AuditView)
	if items[0].ActorUserName != "session8103" || items[0].ActorUser.DisplayName != "已注销用户" || items[0].ActorUser.AvatarURL != "" {
		t.Fatal("deletion must preserve the legacy actor username for authorized audit readers")
	}
}

func TestSessionAggregateDatabaseFailureDoesNotBecomeZero(t *testing.T) {
	database := sessionTestDatabase(t)
	if err := database.Migrator().DropTable(dal.TableUserToken); err != nil {
		t.Fatal(err)
	}
	if _, err := ListUsers(context.Background(), dal.AccountFilter{Limit: 20}); !errors.Is(err, apperrors.ErrDependency) {
		t.Fatal("failed session aggregate became successful user counts")
	}
	if _, err := UserDetail(context.Background(), 8101); !errors.Is(err, apperrors.ErrDependency) {
		t.Fatal("failed session aggregate became invented zero detail counts")
	}
}

// TestSessionPersistedSignedIDsPaginateWithoutChangingAuthentication reproduces
// the existing generator's signed-overflow IDs without depending on wall time.
func TestSessionPersistedSignedIDsPaginateWithoutChangingAuthentication(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	sessionTestInsert(t, database, -9400, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, -9401, 8101, model.SessionUser, now.Add(-time.Minute), now.Add(time.Hour))
	sessionTestInsert(t, database, -9402, 8101, model.SessionUser, now.Add(-2*time.Minute), now.Add(time.Hour))
	page, err := ListSessions(context.Background(), sessionTestToken(-9400), SessionFilter{Limit: 1})
	if err != nil || page.CurrentSession == nil || page.CurrentSession.ID != "-9400" || len(page.Items) != 1 || page.Items[0].ID != "-9401" || !page.HasMore {
		t.Fatalf("stored signed IDs must remain authenticatable and visible: %+v %v", page, err)
	}
	last, err := ListSessions(context.Background(), sessionTestToken(-9400), SessionFilter{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != "-9402" || last.HasMore {
		t.Fatalf("server-issued signed cursor must remain valid: %+v %v", last, err)
	}
}

func TestSessionPersistedSignedIDsRevokeAtomicallyAndProtectCurrent(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	sessionTestInsert(t, database, -9500, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, -9501, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, -9599, 8102, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, -9223372036854775808, 8101, model.SessionUser, now, now.Add(-time.Minute))
	result, err := RevokeSessions(context.Background(), sessionTestToken(-9500), []string{"-9501", "-9223372036854775808"})
	if err != nil || result.RevokedCount != 1 || result.AlreadyInactiveCount != 1 {
		t.Fatalf("stored signed IDs must support selected logout: %+v %v", result, err)
	}
	if _, err := RevokeSessions(context.Background(), sessionTestToken(-9500), []string{"-9599"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("signed foreign ID must remain invisible: %v", err)
	}
	if _, err := RevokeSessions(context.Background(), sessionTestToken(-9500), []string{"-9500"}); !errors.Is(err, apperrors.ErrInvalid) {
		t.Fatal("signed current ID bypassed the current-session protection")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(-9500), false); err != nil {
		t.Fatal("selected logout revoked the signed current session")
	}
}
