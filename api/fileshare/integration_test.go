package fileshare_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	accountsapi "github.com/mcoder2014/home_server/api/accounts"
	applicationsapi "github.com/mcoder2014/home_server/api/applications"
	"github.com/mcoder2014/home_server/api/auth"
	fileshareapi "github.com/mcoder2014/home_server/api/fileshare"
	"github.com/mcoder2014/home_server/api/middleware"
	fileapp "github.com/mcoder2014/home_server/app/fileshare"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	applicationservice "github.com/mcoder2014/home_server/domain/service/applications"
	fileservice "github.com/mcoder2014/home_server/domain/service/fileshare"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/internal/accountsmigrate"
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm/logger"
)

const fileSharingPassword = "FileSharingOwner!234"
const fileSharingOrigin = "https://home.example.com"

var fileSharingFixtureSequence uint64

type httpFixture struct {
	t                        *testing.T
	router                   *gin.Engine
	database                 *sql.DB
	conf                     config.Config
	owner, member, guest, ip string
}

type browserSession struct {
	Cookie      *http.Cookie
	CSRF        string
	ID          string
	ExtraCookie []*http.Cookie
}

type apiResponse struct {
	Status  int
	Code    int
	Data    map[string]interface{}
	Raw     []byte
	Header  http.Header
	Cookies []*http.Cookie
}

// newHTTPFixture builds an isolated schema, imports three synthetic identities
// through the production migration flow, then registers the real auth and file
// sharing handlers. Package tests stay sequential because these services use
// process-global configuration, database and router registries by design.
func newHTTPFixture(t *testing.T, identitySource string, customize func(*config.Config)) *httpFixture {
	t.Helper()
	dsn := os.Getenv("FILE_SHARING_TEST_DSN")
	if dsn == "" {
		t.Skip("FILE_SHARING_TEST_DSN required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_file_sharing_test_ci" {
		t.Fatal("dedicated file sharing test database required")
	}
	parsed.MultiStatements = false
	parsed.ParseTime = true
	sqlDB, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	resetSchema(t, sqlDB, parsed.DBName)
	applyBaseline(t, sqlDB)

	serial := atomic.AddUint64(&fileSharingFixtureSequence, 1)
	fixture := &httpFixture{t: t, database: sqlDB, owner: fmt.Sprintf("file_owner_%d", serial), member: fmt.Sprintf("file_member_%d", serial), guest: fmt.Sprintf("file_guest_%d", serial), ip: fmt.Sprintf("192.0.2.%d:41000", serial%200+1)}
	root := t.TempDir()
	fixture.conf = config.Config{IdentitySource: identitySource, ConfigSource: "database", UploadHardLimitBytes: 50 << 20}
	fixture.conf.Mysql.MasterDB = parsed.FormatDSN()
	fixture.conf.Auth.ApplicationsEnabled = true
	fixture.conf.Auth.SiteOrigin = fileSharingOrigin
	fixture.conf.WebDAV.SharePath = filepath.Join(root, "dav")
	fixture.conf.FileSharing = config.FileSharingConfig{Enabled: true, StorageRoot: filepath.Join(root, "files"), MaxFileBytes: 1 << 20, MaxFilesPerUser: 20, MaxUserBytes: 4 << 20, MinFreeDiskBytes: 1, MaxConcurrentUploadsPerUser: 2, MaxConcurrentUploads: 4}
	if customize != nil {
		customize(&fixture.conf)
	}
	if err := os.MkdirAll(fixture.conf.WebDAV.SharePath, 0700); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(fileSharingPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]map[string]interface{}, 0, 3)
	for index, name := range []string{fixture.owner, fixture.member, fixture.guest} {
		identities = append(identities, map[string]interface{}{"id": 1001 + index, "user_name": name, "password": string(hash), "email": name + "@example.com", "mobile": ""})
	}
	rawUsers, _ := json.Marshal(identities)
	fixture.conf.Passport.MockData = string(rawUsers)
	migrateAccounts(t, sqlDB, parsed.DBName, fixture.conf, root)
	configureFixture(t, fixture, parsed.FormatDSN())
	return fixture
}

func resetSchema(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	rows, err := database.Query("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", name)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if _, err := database.Exec("DROP TABLE `" + strings.ReplaceAll(table, "`", "``") + "`"); err != nil {
			t.Fatal(err)
		}
	}
}

func applyBaseline(t *testing.T, database *sql.DB) {
	t.Helper()
	_, testFile, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "../../domain/dal/create_table.sql"))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{strings.Replace(string(raw), "use home_server;", "", 1)}
	for _, name := range []string{"20260909_web_projects.sql", "20260912_applications.sql", "20260919_file_sharing.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		statements = append(statements, string(raw))
	}
	for _, raw := range statements {
		if err := executeSQL(database, raw); err != nil {
			t.Fatalf("baseline schema: %v", err)
		}
	}
}

// executeSQL removes line comments before splitting so semicolons in migration
// comments cannot become executable fragments in the synthetic database.
func executeSQL(database *sql.DB, raw string) error {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(statement), "USE ") {
			return fmt.Errorf("migration attempted to switch database")
		}
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateAccounts(t *testing.T, database *sql.DB, databaseName string, conf config.Config, root string) {
	t.Helper()
	rawConf, err := yaml.Marshal(conf)
	if err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(root, "source.yaml")
	if err := os.WriteFile(confPath, rawConf, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := accountsmigrate.ReadSource(confPath, accountsmigrate.Grants{Admins: []int64{1001}})
	if err != nil {
		t.Fatal(err)
	}
	options := accountsmigrate.Options{Database: databaseName, BinarySHA256: strings.Repeat("b", 64), DDLChecksums: map[string]string{}, Now: time.Now()}
	for _, name := range []string{"20260913_runtime_config.sql", "20260913_accounts.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		options.DDLChecksums[name] = hex.EncodeToString(sum[:])
		steps, err := accountsmigrate.ParseDDL(name, raw)
		if err != nil {
			t.Fatal(err)
		}
		options.Steps = append(options.Steps, steps...)
	}
	plan, err := accountsmigrate.BuildPlan(context.Background(), database, source, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountsmigrate.Apply(context.Background(), database, source, options, plan.SHA256, databaseName); err != nil {
		t.Fatal(err)
	}
	centerOptions, err := accountsmigrate.AccountCenterOptions(databaseName)
	if err != nil {
		t.Fatal(err)
	}
	centerPlan, err := accountsmigrate.BuildAdditivePlan(context.Background(), database, centerOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountsmigrate.ApplyAdditive(context.Background(), database, centerOptions, centerPlan.SHA256, databaseName, true); err != nil {
		t.Fatal(err)
	}
}

func configureFixture(t *testing.T, fixture *httpFixture, dsn string) {
	t.Helper()
	config.SetGlobalConfig(fixture.conf)
	if err := db.InitDatabase(dsn); err != nil {
		t.Fatal(err)
	}
	db.MasterDB().Logger = logger.Discard
	if err := passport.Init(&fixture.conf); err != nil {
		t.Fatal(err)
	}
	if err := applicationservice.Init(fixture.conf.Auth); err != nil {
		t.Fatal(err)
	}
	if err := fileservice.Init(&fixture.conf.FileSharing, fixture.conf.WebDAV.SharePath, fixture.conf.WebProjects.StorageRoot, fixture.conf.Manuals.StorageRoot); err != nil {
		t.Fatal(err)
	}
	config.SetGlobalConfig(fixture.conf)
	if fixture.conf.ConfigSource == "database" {
		runtimeService := siteconfig.New(db.MasterDB(), fixture.conf)
		if err := runtimeService.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
		snapshot := config.Runtime()
		snapshot.AccountPolicy.MaxActiveSessions = 100
		if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
		accountsapi.Configure(runtimeService)
	} else {
		accountsapi.Configure(nil)
	}
	fileapp.Default = fileapp.New()
	gin.SetMode(gin.TestMode)
	logrus.SetOutput(io.Discard)
	if err := middleware.ConfigureAuthentication(fixture.conf.Auth); err != nil {
		t.Fatal(err)
	}
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	for _, register := range []func() error{auth.InitRouter, applicationsapi.InitRouter, fileshareapi.InitRouter} {
		if err := register(); err != nil {
			t.Fatal(err)
		}
	}
	fixture.router = gin.New()
	fixture.router.Use(gin.RecoveryWithWriter(io.Discard), middleware.AddLogID)
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		fixture.router.Handle(method, path, handlers...)
	})
	gormDB := db.MasterDB()
	t.Cleanup(func() {
		accountsapi.Configure(nil)
		connection, _ := gormDB.DB()
		_ = connection.Close()
		_ = fixture.database.Close()
	})
}

func (fixture *httpFixture) request(method, path string, session *browserSession, body interface{}, headers map[string]string) apiResponse {
	fixture.t.Helper()
	var raw []byte
	var reader io.Reader
	switch value := body.(type) {
	case io.Reader:
		reader = value
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	case nil:
	default:
		raw, _ = json.Marshal(body)
	}
	if reader == nil {
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, fileSharingOrigin+path, reader)
	request.RemoteAddr = fixture.ip
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Origin", fileSharingOrigin)
	request.Header.Set("Content-Type", "application/json")
	if session != nil {
		if session.Cookie != nil {
			request.AddCookie(session.Cookie)
			request.Header.Set("X-CSRF-Token", session.CSRF)
		}
		for _, cookie := range session.ExtraCookie {
			request.AddCookie(cookie)
		}
	}
	for name, value := range headers {
		if value == "" {
			request.Header.Del(name)
		} else {
			request.Header.Set(name, value)
		}
	}
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	response := apiResponse{Status: recorder.Code, Raw: append([]byte(nil), recorder.Body.Bytes()...), Header: recorder.Header(), Cookies: recorder.Result().Cookies()}
	var envelope struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	if json.Unmarshal(response.Raw, &envelope) == nil {
		response.Code = envelope.Code
		response.Data = envelope.Data
	}
	return response
}

func (fixture *httpFixture) login(name string) *browserSession {
	fixture.t.Helper()
	response := fixture.request(http.MethodPost, "/api/auth/login", nil, map[string]interface{}{"user_name": name, "password": fileSharingPassword}, nil)
	data := requireSuccess(fixture.t, response)
	var cookie *http.Cookie
	for _, candidate := range response.Cookies {
		if candidate.Name == utils.SessionCookieName && candidate.MaxAge > 0 {
			cookie = candidate
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" {
		fixture.t.Fatal("login did not issue the secure site-wide session cookie")
	}
	return &browserSession{Cookie: cookie, CSRF: data["csrf_token"].(string), ID: data["id"].(string)}
}

func requireSuccess(t *testing.T, response apiResponse) map[string]interface{} {
	t.Helper()
	if response.Status < 200 || response.Status >= 300 || response.Code != 0 {
		t.Fatalf("request failed: status=%d code=%d response=%s", response.Status, response.Code, response.Raw)
	}
	return response.Data
}

func requireError(t *testing.T, response apiResponse, status, code int) {
	t.Helper()
	if response.Status != status || response.Code != code {
		t.Fatalf("unexpected error: status=%d code=%d response=%s", response.Status, response.Code, response.Raw)
	}
}

func object(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

func multipartBody(t *testing.T, filename string, content []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func (fixture *httpFixture) upload(session *browserSession, filename string, content []byte) map[string]interface{} {
	fixture.t.Helper()
	body, contentType := multipartBody(fixture.t, filename, content)
	return requireSuccess(fixture.t, fixture.request(http.MethodPost, "/api/files", session, body, map[string]string{"Content-Type": contentType}))
}

func (fixture *httpFixture) createShare(session *browserSession, fileID string, input map[string]interface{}) map[string]interface{} {
	fixture.t.Helper()
	data := requireSuccess(fixture.t, fixture.request(http.MethodPost, "/api/files/"+fileID+"/shares", session, input, nil))
	share := object(data["share"])
	if share["token"] == "" || share["url"] != fileSharingOrigin+"/s/"+share["token"].(string) {
		fixture.t.Fatalf("share URL/token contract broken: %#v", share)
	}
	return data
}

func (fixture *httpFixture) downloadCount(token string) int64 {
	fixture.t.Helper()
	var count int64
	if err := fixture.database.QueryRow("SELECT download_count FROM file_shares WHERE token=?", token).Scan(&count); err != nil {
		fixture.t.Fatal(err)
	}
	return count
}

func (fixture *httpFixture) grantCookie(response apiResponse) *http.Cookie {
	fixture.t.Helper()
	for _, cookie := range response.Cookies {
		if strings.HasPrefix(cookie.Name, "hs_fs_") && cookie.MaxAge > 0 {
			if !cookie.Secure || !cookie.HttpOnly || !strings.HasPrefix(cookie.Path, "/api/file-shares/") {
				fixture.t.Fatal("unlock cookie lacks security attributes")
			}
			return cookie
		}
	}
	fixture.t.Fatal("unlock response did not issue a grant cookie")
	return nil
}

func shareState(t *testing.T, response apiResponse, expected string) map[string]interface{} {
	t.Helper()
	data := requireSuccess(t, response)
	if data["state"] != expected {
		t.Fatalf("share state = %#v, want %q", data, expected)
	}
	return data
}

func (fixture *httpFixture) applicationToken(session *browserSession, scopes []string) string {
	fixture.t.Helper()
	created := requireSuccess(fixture.t, fixture.request(http.MethodPost, "/api/applications", session, map[string]interface{}{"name": "file sharing test client", "description": "synthetic", "scopes": scopes}, nil))
	application := object(created["application"])
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {application["access_key"].(string)}, "client_secret": {created["secret_key"].(string)}}
	response := fixture.request(http.MethodPost, "/api/auth/token", nil, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if response.Status != http.StatusOK {
		fixture.t.Fatalf("application token exchange failed: %d %s", response.Status, response.Raw)
	}
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(response.Raw, &issued) != nil || issued.AccessToken == "" {
		fixture.t.Fatal("application token response missing access_token")
	}
	return issued.AccessToken
}

// TestFileSharingHTTPAccessAndLifecycle exercises the complete user-visible
// lifecycle through production Gin handlers. It covers all ACL/secret states,
// download accounting, private storage failures and owner-only management.
func TestFileSharingHTTPAccessAndLifecycle(t *testing.T) {
	fixture := newHTTPFixture(t, "database", nil)
	owner := fixture.login(fixture.owner)
	member := fixture.login(fixture.member)
	guest := fixture.login(fixture.guest)

	eligible := requireSuccess(t, fixture.request(http.MethodGet, "/api/files/eligible-users", owner, nil, nil))
	items, ok := eligible["items"].([]interface{})
	if !ok || len(items) != 3 || strings.Contains(string(fixture.request(http.MethodGet, "/api/files/eligible-users", owner, nil, nil).Raw), "password") {
		t.Fatalf("eligible users response is incomplete or sensitive: %#v", eligible)
	}

	readToken := fixture.applicationToken(owner, []string{"files:read"})
	readHeaders := map[string]string{"Authorization": "Bearer " + readToken}
	requireSuccess(t, fixture.request(http.MethodGet, "/api/files", nil, nil, readHeaders))
	body, contentType := multipartBody(t, "scope-denied.bin", []byte("x"))
	requireError(t, fixture.request(http.MethodPost, "/api/files", nil, body, map[string]string{"Authorization": "Bearer " + readToken, "Content-Type": contentType}), http.StatusForbidden, 3)
	writeToken := fixture.applicationToken(owner, []string{"files:write"})
	body, contentType = multipartBody(t, "scope-write.bin", []byte("app"))
	requireSuccess(t, fixture.request(http.MethodPost, "/api/files", nil, body, map[string]string{"Authorization": "Bearer " + writeToken, "Content-Type": contentType}))
	requireSuccess(t, fixture.request(http.MethodGet, "/api/files", nil, nil, map[string]string{"Authorization": "Bearer " + writeToken}))

	content := []byte("private file sharing payload")
	uploadBody, uploadHeaders := multipartForRequest(t, "report.bin", content)
	uploadResponse := fixture.request(http.MethodPost, "/api/files", owner, uploadBody, uploadHeaders)
	file := requireSuccess(t, uploadResponse)
	fileID := file["id"].(string)
	if file["name"] != "report.bin" || int64(file["size_bytes"].(float64)) != int64(len(content)) || strings.Contains(string(uploadResponse.Raw), "storage_key") || strings.Contains(string(uploadResponse.Raw), fixture.conf.FileSharing.StorageRoot) {
		t.Fatalf("upload response leaked storage or wrong metadata: %s", uploadResponse.Raw)
	}
	var storageKey string
	if err := fixture.database.QueryRow("SELECT storage_key FROM files WHERE id=?", fileID).Scan(&storageKey); err != nil {
		t.Fatal(err)
	}
	storagePath := filepath.Join(fixture.conf.FileSharing.StorageRoot, filepath.FromSlash(storageKey))
	storageInfo, err := os.Stat(storagePath)
	if err != nil || !storageInfo.Mode().IsRegular() || storageInfo.Mode().Perm()&0077 != 0 {
		t.Fatalf("stored bytes are missing or not private: info=%#v err=%v", storageInfo, err)
	}
	requireError(t, fixture.request(http.MethodGet, "/api/files/"+fileID, guest, nil, nil), http.StatusNotFound, 4)
	requireError(t, fixture.request(http.MethodDelete, "/api/files/"+fileID, guest, nil, nil), http.StatusNotFound, 4)

	publicResult := fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "public", "secret_mode": "none", "max_downloads": 1})
	publicShare := object(publicResult["share"])
	publicToken := publicShare["token"].(string)
	authShare := object(fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "authenticated", "secret_mode": "none"})["share"])
	authToken := authShare["token"].(string)
	membersResult := fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "members", "member_user_ids": []string{member.ID}, "secret_mode": "password", "secret": "member-password", "expires_at": time.Now().Add(time.Hour), "max_downloads": 7})
	membersShare := object(membersResult["share"])
	membersToken := membersShare["token"].(string)
	codeResult := fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "public", "secret_mode": "code", "secret": "Ab12Cd"})
	codeShare := object(codeResult["share"])
	codeToken := codeShare["token"].(string)
	stalePublicShare := object(fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "public", "secret_mode": "none"})["share"])
	stalePublicToken := stalePublicShare["token"].(string)
	if publicResult["secret"] != "" || membersResult["secret"] != "member-password" || codeResult["secret"] != "Ab12Cd" {
		t.Fatal("create response did not obey one-time secret contract")
	}
	listed := fixture.request(http.MethodGet, "/api/files/"+fileID+"/shares", owner, nil, nil)
	requireSuccess(t, listed)
	if strings.Contains(string(listed.Raw), "member-password") || strings.Contains(string(listed.Raw), "Ab12Cd") || strings.Contains(string(listed.Raw), "secret_hash") {
		t.Fatal("share listing exposed a secret")
	}

	staleSession := fixture.login(fixture.guest)
	staleDigest := sha256.Sum256([]byte(staleSession.Cookie.Value))
	updated, err := fixture.database.Exec("UPDATE login_token SET is_expired=1 WHERE token_digest=? AND is_expired=0", staleDigest[:])
	if err != nil {
		t.Fatal(err)
	}
	if count, err := updated.RowsAffected(); err != nil || count != 1 {
		t.Fatalf("revoking stale browser session affected %d rows: %v", count, err)
	}
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+authToken, staleSession, nil, nil), "login_required")
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, staleSession, nil, nil), "login_required")
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+stalePublicToken, staleSession, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+stalePublicToken+"/download", staleSession, nil, nil); response.Status != http.StatusOK || !bytes.Equal(response.Raw, content) {
		t.Fatalf("stale session blocked public download: %d %s", response.Status, response.Raw)
	}
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+codeToken, staleSession, nil, nil), "locked")
	staleUnlock := fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/unlock", staleSession, map[string]interface{}{"secret": "Ab12Cd"}, nil)
	requireSuccess(t, staleUnlock)
	staleSession.ExtraCookie = append(staleSession.ExtraCookie, fixture.grantCookie(staleUnlock))
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+codeToken, staleSession, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/download", staleSession, nil, nil); response.Status != http.StatusOK || !bytes.Equal(response.Raw, content) {
		t.Fatalf("stale session blocked code download: %d %s", response.Status, response.Raw)
	}
	badAuthorization := map[string]string{"Authorization": "Bearer invalid"}
	requireError(t, fixture.request(http.MethodGet, "/api/file-shares/"+stalePublicToken, nil, nil, badAuthorization), http.StatusUnauthorized, 403)
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/unlock", nil, map[string]interface{}{"secret": "Ab12Cd"}, badAuthorization), http.StatusUnauthorized, 403)
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+stalePublicToken+"/download", nil, nil, badAuthorization), http.StatusUnauthorized, 403)

	state := shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+publicToken, nil, nil, nil), "available")
	if object(state["file"])["name"] != "report.bin" {
		t.Fatalf("available share file = %#v", state["file"])
	}
	for _, path := range []string{"/api/file-shares/" + publicToken, "/api/file-shares/" + publicToken + "/download"} {
		if response := fixture.request(http.MethodHead, path, nil, nil, nil); response.Status < 400 {
			t.Fatalf("HEAD unexpectedly started a download: %s status=%d", path, response.Status)
		}
	}
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+publicToken+"/download", nil, nil, map[string]string{"Range": "bytes=0-3"}), http.StatusBadRequest, 2)
	if count := fixture.downloadCount(publicToken); count != 0 {
		t.Fatalf("HEAD/Range consumed %d downloads", count)
	}
	heldPath := storagePath + ".held"
	if err := os.Rename(storagePath, heldPath); err != nil {
		t.Fatal(err)
	}
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+publicToken+"/download", nil, nil, nil), http.StatusNotFound, 4)
	if count := fixture.downloadCount(publicToken); count != 0 {
		t.Fatalf("missing bytes consumed %d downloads", count)
	}
	if err := os.Rename(heldPath, storagePath); err != nil {
		t.Fatal(err)
	}

	responses := make(chan apiResponse, 12)
	var downloads sync.WaitGroup
	for index := 0; index < 12; index++ {
		downloads.Add(1)
		go func() {
			defer downloads.Done()
			responses <- fixture.request(http.MethodPost, "/api/file-shares/"+publicToken+"/download", nil, nil, nil)
		}()
	}
	downloads.Wait()
	close(responses)
	successes := 0
	for response := range responses {
		if response.Status == http.StatusOK {
			successes++
			if !bytes.Equal(response.Raw, content) || response.Header.Get("Content-Type") != "application/octet-stream" || response.Header.Get("Accept-Ranges") != "none" || response.Header.Get("Referrer-Policy") != "no-referrer" || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment") {
				t.Fatalf("download headers/body are unsafe: headers=%v body=%q", response.Header, response.Raw)
			}
		} else {
			requireError(t, response, http.StatusNotFound, 4)
		}
	}
	if successes != 1 || fixture.downloadCount(publicToken) != 1 {
		t.Fatalf("one-use share started %d transfers, count=%d", successes, fixture.downloadCount(publicToken))
	}

	loginGate := shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+authToken, nil, nil, nil), "login_required")
	if loginGate["file"] != nil {
		t.Fatal("login gate leaked file metadata")
	}
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+authToken, guest, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+authToken+"/download", guest, nil, nil); response.Status != http.StatusOK || !bytes.Equal(response.Raw, content) {
		t.Fatalf("authenticated download failed: %d %s", response.Status, response.Raw)
	}

	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, nil, nil, nil), "login_required")
	requireError(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, guest, nil, nil), http.StatusNotFound, 4)
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+membersToken+"/download", owner, nil, nil); response.Status != http.StatusOK || fixture.downloadCount(membersToken) != 1 {
		t.Fatalf("owner precondition download failed: status=%d count=%d response=%s", response.Status, fixture.downloadCount(membersToken), response.Raw)
	}
	locked := shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, member, nil, nil), "locked")
	if locked["file"] != nil || locked["expires_at"] != nil || locked["remaining_downloads"] != nil || locked["download_count"] != float64(0) || locked["max_downloads"] != float64(0) {
		t.Fatalf("locked member gate reflected protected metadata: %#v", locked)
	}
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+membersToken+"/unlock", member, map[string]interface{}{"secret": "wrong-password"}, nil), http.StatusForbidden, 3)
	requireSuccess(t, fixture.request(http.MethodGet, "/api/auth/me", member, nil, nil))
	unlocked := fixture.request(http.MethodPost, "/api/file-shares/"+membersToken+"/unlock", member, map[string]interface{}{"secret": "member-password"}, nil)
	requireSuccess(t, unlocked)
	member.ExtraCookie = append(member.ExtraCookie, fixture.grantCookie(unlocked))
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, member, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+membersToken+"/download", member, nil, nil); response.Status != http.StatusOK {
		t.Fatalf("member download failed: %d %s", response.Status, response.Raw)
	}
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+membersToken, owner, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+membersToken+"/download", owner, nil, nil); response.Status != http.StatusOK {
		t.Fatalf("owner secret bypass failed: %d %s", response.Status, response.Raw)
	}

	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+codeToken, nil, nil, nil), "locked")
	requireError(t, fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/unlock", nil, map[string]interface{}{"secret": "000000"}, nil), http.StatusForbidden, 3)
	codeUnlock := fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/unlock", nil, map[string]interface{}{"secret": "Ab12Cd"}, nil)
	requireSuccess(t, codeUnlock)
	anonymousGrant := &browserSession{ExtraCookie: []*http.Cookie{fixture.grantCookie(codeUnlock)}}
	shareState(t, fixture.request(http.MethodGet, "/api/file-shares/"+codeToken, anonymousGrant, nil, nil), "available")
	if response := fixture.request(http.MethodPost, "/api/file-shares/"+codeToken+"/download", anonymousGrant, nil, nil); response.Status != http.StatusOK {
		t.Fatalf("code download failed: %d %s", response.Status, response.Raw)
	}

	requireSuccess(t, fixture.request(http.MethodDelete, "/api/files/"+fileID+"/shares/"+codeShare["id"].(string), owner, nil, nil))
	requireError(t, fixture.request(http.MethodGet, "/api/file-shares/"+codeToken, anonymousGrant, nil, nil), http.StatusNotFound, 4)
	expiring := object(fixture.createShare(owner, fileID, map[string]interface{}{"access_mode": "public", "secret_mode": "none"})["share"])
	if _, err := fixture.database.Exec("UPDATE file_shares SET expires_at=DATE_SUB(NOW(6), INTERVAL 1 SECOND) WHERE token=?", expiring["token"]); err != nil {
		t.Fatal(err)
	}
	requireError(t, fixture.request(http.MethodGet, "/api/file-shares/"+expiring["token"].(string), nil, nil, nil), http.StatusNotFound, 4)

	empty := fixture.upload(owner, "empty.dat", nil)
	emptyShare := object(fixture.createShare(owner, empty["id"].(string), map[string]interface{}{"access_mode": "public", "secret_mode": "none"})["share"])
	emptyDownload := fixture.request(http.MethodPost, "/api/file-shares/"+emptyShare["token"].(string)+"/download", nil, nil, nil)
	if emptyDownload.Status != http.StatusOK || len(emptyDownload.Raw) != 0 || emptyDownload.Header.Get("Content-Length") != "0" || fixture.downloadCount(emptyShare["token"].(string)) != 1 {
		t.Fatalf("empty download contract failed: status=%d headers=%v body=%q", emptyDownload.Status, emptyDownload.Header, emptyDownload.Raw)
	}

	requireSuccess(t, fixture.request(http.MethodDelete, "/api/files/"+fileID, owner, nil, nil))
	if _, err := os.Stat(storagePath); !os.IsNotExist(err) {
		t.Fatalf("deleted file bytes remain visible: %v", err)
	}
	for _, token := range []string{publicToken, authToken, membersToken, codeToken} {
		requireError(t, fixture.request(http.MethodGet, "/api/file-shares/"+token, nil, nil, nil), http.StatusNotFound, 4)
	}
}

func multipartForRequest(t *testing.T, filename string, content []byte) ([]byte, map[string]string) {
	body, contentType := multipartBody(t, filename, content)
	return body, map[string]string{"Content-Type": contentType}
}

type gatedReader struct {
	reader  *bytes.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (reader *gatedReader) Read(target []byte) (int, error) {
	reader.once.Do(func() {
		close(reader.started)
		<-reader.release
	})
	return reader.reader.Read(target)
}

// TestSlowUploadReservationAndExactSessionRecheck proves that the request-body
// receive phase owns the concurrency slot and that revoking only the accepted
// session before commit leaves neither metadata nor private bytes behind.
func TestSlowUploadReservationAndExactSessionRecheck(t *testing.T) {
	fixture := newHTTPFixture(t, "database", func(conf *config.Config) {
		conf.FileSharing.MaxConcurrentUploadsPerUser = 1
		conf.FileSharing.MaxConcurrentUploads = 1
	})
	owner := fixture.login(fixture.owner)
	body, contentType := multipartBody(t, "slow.bin", bytes.Repeat([]byte("s"), 64<<10))
	blocked := &gatedReader{reader: bytes.NewReader(body), started: make(chan struct{}), release: make(chan struct{})}
	result := make(chan apiResponse, 1)
	go func() {
		result <- fixture.request(http.MethodPost, "/api/files", owner, blocked, map[string]string{"Content-Type": contentType})
	}()
	<-blocked.started

	secondBody, secondType := multipartBody(t, "second.bin", []byte("second"))
	requireError(t, fixture.request(http.MethodPost, "/api/files", owner, secondBody, map[string]string{"Content-Type": secondType}), http.StatusTooManyRequests, 8)
	digest := sha256.Sum256([]byte(owner.Cookie.Value))
	updated, err := fixture.database.Exec("UPDATE login_token SET is_expired=1 WHERE token_digest=? AND is_expired=0", digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if count, err := updated.RowsAffected(); err != nil || count != 1 {
		t.Fatalf("exact session revocation affected %d rows: %v", count, err)
	}
	close(blocked.release)
	requireError(t, <-result, http.StatusUnauthorized, 403)

	var rows int
	if err := fixture.database.QueryRow("SELECT COUNT(*) FROM files").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("revoked upload committed rows=%d err=%v", rows, err)
	}
	regular := 0
	if err := filepath.Walk(fixture.conf.FileSharing.StorageRoot, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			regular++
		}
		return nil
	}); err != nil || regular != 0 {
		t.Fatalf("revoked upload retained %d files: %v", regular, err)
	}
}

// TestConfigIdentityQuotaSerializesAcrossApplicationInstances uses two real
// legacy sessions but two independent Application objects, bypassing the local
// striped lock to verify that credential-row locking enforces one owner quota
// across service instances.
func TestConfigIdentityQuotaSerializesAcrossApplicationInstances(t *testing.T) {
	fixture := newHTTPFixture(t, "config", func(conf *config.Config) {
		conf.FileSharing.MaxFileBytes = 1
		conf.FileSharing.MaxUserBytes = 1
	})
	first := fixture.login(fixture.owner)
	second := fixture.login(fixture.owner)
	principals := make([]*utils.Principal, 0, 2)
	contexts := make([]context.Context, 0, 2)
	for _, session := range []*browserSession{first, second} {
		identity, err := passport.CheckToken(context.Background(), session.Cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		principals = append(principals, &utils.Principal{Kind: "user", UserID: identity.ID, AuthVersion: identity.AuthVersion, TokenExpiresAt: identity.SessionExpiry})
		contexts = append(contexts, context.WithValue(context.Background(), utils.CtxKeyLoginToken, session.Cookie.Value))
	}
	applications := []*fileapp.Application{fileapp.New(), fileapp.New()}
	errorsByUpload := make(chan error, 2)
	var group sync.WaitGroup
	for index := range applications {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, err := applications[index].Upload(contexts[index], principals[index], fmt.Sprintf("quota-%d.bin", index), strings.NewReader("x"))
			errorsByUpload <- err
		}(index)
	}
	group.Wait()
	close(errorsByUpload)
	succeeded, limited := 0, 0
	for err := range errorsByUpload {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, fileservice.ErrRateLimited):
			limited++
		default:
			t.Fatalf("unexpected concurrent quota result: %v", err)
		}
	}
	var count, size int64
	if err := fixture.database.QueryRow("SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM files WHERE deleted_at IS NULL").Scan(&count, &size); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || limited != 1 || count != 1 || size != 1 {
		t.Fatalf("quota race results succeeded=%d limited=%d rows=%d bytes=%d", succeeded, limited, count, size)
	}
}
