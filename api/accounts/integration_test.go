package accounts_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/api/accounts"
	applicationsapi "github.com/mcoder2014/home_server/api/applications"
	"github.com/mcoder2014/home_server/api/auth"
	"github.com/mcoder2014/home_server/api/library"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/api/webdav"
	webapi "github.com/mcoder2014/home_server/api/webprojects"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	applicationservice "github.com/mcoder2014/home_server/domain/service/applications"
	"github.com/mcoder2014/home_server/domain/service/passport"
	webservice "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/internal/accountsmigrate"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm/logger"
)

const integrationPassword = "ExampleOwner!234"
const integrationNextPassword = "ChangedPassword!456"
const integrationOrigin = "https://home.example.com"

var fixtureSequence uint64

type httpFixture struct {
	t                        *testing.T
	router                   *gin.Engine
	database                 *sql.DB
	conf                     config.Config
	owner, member, guest, ip string
}
type browserSession struct {
	Cookie             *http.Cookie
	CSRF, ID, Username string
	Revision           int64
}
type apiResponse struct {
	Status, Code int
	Data         map[string]interface{}
	Raw          []byte
	Header       http.Header
	Cookies      []*http.Cookie
}

// newHTTPFixture creates only synthetic data, imports it through the real offline
// migration plan/apply flow, then registers actual production Gin handlers. Tests
// are sequential because the application deliberately has one global DB/router.
func newHTTPFixture(t *testing.T, initialApplications ...bool) *httpFixture {
	t.Helper()
	dsn := os.Getenv("ACCOUNTS_HTTP_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNTS_HTTP_TEST_DSN to a synthetic home_server_accounts_http_test_* MariaDB")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid synthetic DSN")
	}
	if !strings.HasPrefix(parsed.DBName, "home_server_accounts_http_test_") || parsed.DBName == "home_server" {
		t.Fatal("unsafe HTTP test database")
	}
	parsed.MultiStatements = false
	parsed.ParseTime = true
	sqlDB, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := sqlDB.Query("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", parsed.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, name := range tables {
		if _, err := sqlDB.Exec("DROP TABLE `" + strings.ReplaceAll(name, "`", "``") + "`"); err != nil {
			t.Fatal(err)
		}
	}
	_, testFile, _, _ := runtime.Caller(0)
	base, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "../../domain/dal/create_table.sql"))
	if err != nil {
		t.Fatal(err)
	}
	baseline := []string{strings.Replace(string(base), "use home_server;", "", 1)}
	for _, name := range []string{"20260909_web_projects.sql", "20260912_applications.sql", "20260916_web_comments.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		baseline = append(baseline, string(raw))
	}
	for _, raw := range baseline {
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
				t.Fatal("fixture must never switch to a different database")
			}
			if _, err := sqlDB.Exec(statement); err != nil {
				t.Fatalf("baseline schema: %v", err)
			}
		}
	}
	serial := atomic.AddUint64(&fixtureSequence, 1)
	fixture := &httpFixture{t: t, database: sqlDB, owner: fmt.Sprintf("owner_%d", serial), member: fmt.Sprintf("member_%d", serial), guest: fmt.Sprintf("guest_%d", serial), ip: fmt.Sprintf("192.0.2.%d:40000", serial%200+1)}
	root := t.TempDir()
	fixture.conf = config.Config{IdentitySource: "database", ConfigSource: "database", UploadHardLimitBytes: 50 << 20}
	fixture.conf.Mysql.MasterDB = parsed.FormatDSN()
	fixture.conf.Auth.ApplicationsEnabled = true
	fixture.conf.Auth.SiteOrigin = integrationOrigin
	if len(initialApplications) > 0 {
		fixture.conf.Auth.ApplicationsEnabled = initialApplications[0]
	}
	fixture.conf.WebProjects.Enabled = true
	fixture.conf.WebProjects.SiteOrigin = integrationOrigin
	fixture.conf.WebProjects.StorageRoot = filepath.Join(root, "web")
	fixture.conf.WebDAV.SharePath = filepath.Join(root, "dav")
	if err := os.MkdirAll(fixture.conf.WebDAV.SharePath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.conf.WebDAV.SharePath, "fixture.txt"), []byte("private fixture file"), 0600); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(integrationPassword), 4)
	if err != nil {
		t.Fatal(err)
	}
	identities := []map[string]interface{}{}
	for i, name := range []string{fixture.owner, fixture.member, fixture.guest} {
		identities = append(identities, map[string]interface{}{"id": 1001 + i, "user_name": name, "password": string(passwordHash), "email": name + "@example.com", "mobile": ""})
	}
	rawUsers, _ := json.Marshal(identities)
	fixture.conf.Passport.MockData = string(rawUsers)
	rawConf, err := yaml.Marshal(fixture.conf)
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
	opts := accountsmigrate.Options{Database: parsed.DBName, BinarySHA256: strings.Repeat("a", 64), DDLChecksums: map[string]string{}, Now: time.Now()}
	for _, name := range []string{"20260913_runtime_config.sql", "20260913_accounts.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		opts.DDLChecksums[name] = hex.EncodeToString(sum[:])
		steps, err := accountsmigrate.ParseDDL(name, raw)
		if err != nil {
			t.Fatal(err)
		}
		opts.Steps = append(opts.Steps, steps...)
	}
	plan, err := accountsmigrate.BuildPlan(context.Background(), sqlDB, source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountsmigrate.Apply(context.Background(), sqlDB, source, opts, plan.SHA256, parsed.DBName); err != nil {
		t.Fatal(err)
	}
	config.SetGlobalConfig(fixture.conf)
	if err := db.InitDatabase(parsed.FormatDSN()); err != nil {
		t.Fatal(err)
	}
	db.MasterDB().Logger = logger.Discard
	if err := passport.Init(&fixture.conf); err != nil {
		t.Fatal(err)
	}
	if err := webservice.Init(&fixture.conf.WebProjects); err != nil {
		t.Fatal(err)
	}
	if err := applicationservice.Init(fixture.conf.Auth); err != nil {
		t.Fatal(err)
	}
	config.SetGlobalConfig(fixture.conf)
	runtimeService := siteconfig.New(db.MasterDB(), fixture.conf)
	if err := runtimeService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts.Configure(runtimeService)
	gin.SetMode(gin.TestMode)
	logrus.SetOutput(io.Discard)
	if err := middleware.ConfigureAuthentication(fixture.conf.Auth); err != nil {
		t.Fatal(err)
	}
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	for _, register := range []func() error{auth.InitRouter, applicationsapi.InitRouter, library.InitRouter, webdav.InitRouter, webapi.InitRouter} {
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
	t.Cleanup(func() { accounts.Configure(nil); connection, _ := gormDB.DB(); connection.Close(); sqlDB.Close() })
	configureFixtureReadCache(fixture)
	return fixture
}

// request 构造带可控身份、请求体和合成 TLS 状态的 Gin 请求，解析响应封包与 Cookie，供真实处理链集成用例复用。
func (f *httpFixture) request(method, path string, session *browserSession, body interface{}, headers map[string]string) apiResponse {
	var raw []byte
	var bodyReader io.Reader
	switch value := body.(type) {
	case io.Reader:
		bodyReader = value
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	case nil:
	default:
		raw, _ = json.Marshal(body)
	}
	if bodyReader == nil {
		bodyReader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, integrationOrigin+path, bodyReader)
	request.RemoteAddr = f.ip
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Origin", integrationOrigin)
	request.Header.Set("Content-Type", "application/json")
	if session != nil {
		request.AddCookie(session.Cookie)
		request.Header.Set("X-CSRF-Token", session.CSRF)
	}
	for key, value := range headers {
		if value == "" {
			request.Header.Del(key)
		} else {
			request.Header.Set(key, value)
		}
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	result := apiResponse{Status: recorder.Code, Raw: append([]byte(nil), recorder.Body.Bytes()...), Header: recorder.Header(), Cookies: recorder.Result().Cookies()}
	var wire struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	if json.Unmarshal(result.Raw, &wire) == nil {
		result.Code = wire.Code
		result.Data = wire.Data
	}
	return result
}

func requireSuccess(t *testing.T, response apiResponse) map[string]interface{} {
	t.Helper()
	if response.Status < 200 || response.Status >= 300 || response.Code != 0 {
		t.Fatalf("HTTP request failed: status=%d code=%d response=%s", response.Status, response.Code, response.Raw)
	}
	return response.Data
}
func requireDenied(t *testing.T, response apiResponse) {
	t.Helper()
	if response.Status < 400 && response.Code == 0 {
		t.Fatalf("request unexpectedly accepted: status=%d", response.Status)
	}
	if response.Status >= 500 {
		t.Fatalf("authorization check crashed or lost dependency: status=%d code=%d", response.Status, response.Code)
	}
}
func number(value interface{}) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case string:
		n, _ := strconv.ParseInt(value, 10, 64)
		return n
	}
	return 0
}
func object(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

// login 通过真实账号登录 handler 建立合成浏览器会话，并验证安全 Cookie 属性以及响应没有泄露凭据。
func (f *httpFixture) login(name, password string) *browserSession {
	f.t.Helper()
	response := f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": name, "password": password}, nil)
	value := requireSuccess(f.t, response)
	var cookie *http.Cookie
	for _, candidate := range response.Cookies {
		if candidate.MaxAge > 0 {
			if cookie != nil {
				f.t.Fatal("multiple active session cookies")
			}
			cookie = candidate
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" {
		f.t.Fatal("login must set a secure HttpOnly site-wide cookie")
	}
	if strings.Contains(string(response.Raw), "password_hash") || strings.Contains(string(response.Raw), cookie.Value) {
		f.t.Fatal("login JSON exposed password/session secret")
	}
	return &browserSession{Cookie: cookie, CSRF: value["csrf_token"].(string), ID: value["id"].(string), Username: name, Revision: number(value["revision"])}
}
func (f *httpFixture) me(session *browserSession) map[string]interface{} {
	f.t.Helper()
	return requireSuccess(f.t, f.request("GET", "/api/auth/me", session, nil, nil))
}
func (f *httpFixture) adminAction(admin *browserSession, id, action, method string, values map[string]interface{}) apiResponse {
	f.t.Helper()
	detail := requireSuccess(f.t, f.request("GET", "/api/admin/users/"+id, admin, nil, nil))
	rev := number(object(detail["user"])["revision"])
	values["reason"] = "isolated HTTP verification"
	values["current_password"] = integrationPassword
	return f.request(method, "/api/admin/users/"+id+"/"+action, admin, values, map[string]string{"If-Match": strconv.FormatInt(rev, 10)})
}
func (f *httpFixture) publish(admin *browserSession, namespace string, values map[string]interface{}, requestID string) apiResponse {
	f.t.Helper()
	current := requireSuccess(f.t, f.request("GET", "/api/admin/config/"+namespace, admin, nil, nil))
	rev := number(current["revision"])
	return f.request("PUT", "/api/admin/config/"+namespace, admin, map[string]interface{}{"values": values, "request_id": requestID, "reason": "isolated HTTP verification", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(rev, 10)})
}

// TestHTTPLoginCSRFAndProfileIsolation 验证 Cookie 登录、资料写入的 CSRF 边界与白名单，防止客户端伪造身份或权限字段。
func TestHTTPLoginCSRFAndProfileIsolation(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.owner, "password": integrationPassword}, map[string]string{"Origin": "https://foreign.example.com"}))
	requireDenied(t, f.request("GET", "/api/admin/users", member, nil, nil))
	rev := strconv.FormatInt(number(f.me(member)["revision"]), 10)
	requireDenied(t, f.request("PATCH", "/api/account/profile", member, map[string]interface{}{"display_name": "Member", "role": "admin"}, map[string]string{"If-Match": rev}))
	requireDenied(t, f.request("PATCH", "/api/account/profile", member, map[string]interface{}{"display_name": "Member", "user_id": "1001"}, map[string]string{"If-Match": rev}))
	requireDenied(t, f.request("PATCH", "/api/account/profile", member, map[string]interface{}{"display_name": "Member"}, map[string]string{"If-Match": rev, "X-CSRF-Token": ""}))
	requireSuccess(t, f.request("PATCH", "/api/account/profile", member, map[string]interface{}{"display_name": "Member", "contact_email": "member-profile@example.com", "contact_mobile": "12345678"}, map[string]string{"If-Match": rev}))
	updated := f.me(member)
	rev = strconv.FormatInt(number(updated["revision"]), 10)
	requireSuccess(t, f.request("PATCH", "/api/account/profile", member, map[string]interface{}{"display_name": "Renamed"}, map[string]string{"If-Match": rev}))
	updated = f.me(member)
	if updated["contact_email"] != "member-profile@example.com" || updated["contact_mobile"] != "12345678" {
		t.Fatal("partial profile PATCH must preserve unspecified contact fields")
	}
	if f.me(owner)["display_name"] != f.owner {
		t.Fatal("self profile update modified another account")
	}
}

func TestHTTPChangePasswordInvalidatesEveryOldSession(t *testing.T) {
	f := newHTTPFixture(t)
	member := f.login(f.member, integrationPassword)
	second := f.login(f.member, integrationPassword)
	requireDenied(t, f.request("POST", "/api/auth/change-password", member, map[string]interface{}{"current_password": "wrong-password", "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
	requireSuccess(t, f.request("GET", "/api/auth/me", second, nil, nil))
	requireSuccess(t, f.request("POST", "/api/auth/change-password", member, map[string]interface{}{"current_password": integrationPassword, "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", second, nil, nil))
	requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword}, nil))
	f.login(f.member, integrationNextPassword)
}

// TestHTTPTemporaryPasswordSessionIsRestricted 验证管理员初始密码只能建立受限网站会话，完成改密后旧会话失效、新密码取得正常网站能力。
func TestHTTPTemporaryPasswordSessionIsRestricted(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	created := requireSuccess(t, f.request("POST", "/api/admin/users", owner, map[string]interface{}{"user_name": "temporary_user", "generate_password": true}, nil))
	password := created["initial_password"].(string)
	if len(password) < 12 || created["password_expires_at"] == nil {
		t.Fatal("admin-created initial password missing expiry or strength")
	}
	session := f.login("temporary_user", password)
	if f.me(session)["must_change_password"] != true {
		t.Fatal("temporary session not marked restricted")
	}
	for _, path := range []string{"/api/account/invitations", "/api/applications", "/api/web-share", "/library/book/total?offset=0&limit=10"} {
		requireDenied(t, f.request("GET", path, session, nil, nil))
	}
	requireSuccess(t, f.request("POST", "/api/auth/change-password", session, map[string]interface{}{"current_password": password, "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", session, nil, nil))
	session = f.login("temporary_user", integrationNextPassword)
	if f.me(session)["must_change_password"] != false {
		t.Fatal("password restriction did not clear")
	}
}

// TestHTTPInvitationConcurrencyAndFailureDoNotOverconsume 在合成库中并发生成和兑换邀请码，验证月额度、固定有效期、单次消费及失败不扣额。
func TestHTTPInvitationConcurrencyAndFailureDoNotOverconsume(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	requireDenied(t, f.request("POST", "/api/account/invitations", member, map[string]interface{}{"request_id": "closed-invite-request", "note": "closed"}, nil))
	requireSuccess(t, f.publish(owner, "registration", map[string]interface{}{"enabled": true}, "registration-open-test"))
	results := make(chan apiResponse, 6)
	var group sync.WaitGroup
	for i := 0; i < 6; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results <- f.request("POST", "/api/account/invitations", member, map[string]interface{}{"request_id": fmt.Sprintf("concurrent-invite-%02d", i), "note": "quota"}, nil)
		}(i)
	}
	group.Wait()
	close(results)
	var codes []string
	for result := range results {
		if result.Status >= 200 && result.Status < 300 && result.Code == 0 {
			value := result.Data
			codes = append(codes, value["code"].(string))
			inv := object(value["invitation"])
			created, _ := time.Parse(time.RFC3339Nano, inv["create_time"].(string))
			expires, _ := time.Parse(time.RFC3339Nano, inv["expires_at"].(string))
			if expires.Sub(created) != 7*24*time.Hour {
				t.Fatal("invitation TTL must be exactly 168 hours")
			}
		} else {
			requireDenied(t, result)
		}
	}
	if len(codes) != 3 {
		t.Fatalf("monthly quota allowed %d concurrent invitations; want 3", len(codes))
	}
	payload := map[string]interface{}{"user_name": "registered_one", "password": "short", "confirm_password": "short", "invitation_code": codes[0]}
	requireDenied(t, f.request("POST", "/api/auth/register", nil, payload, nil))
	requireSuccess(t, f.request("POST", "/api/auth/invitations/validate", nil, map[string]interface{}{"code": codes[0]}, nil))
	payload["password"] = integrationPassword
	payload["confirm_password"] = integrationPassword
	payload["user_name"] = f.member
	requireDenied(t, f.request("POST", "/api/auth/register", nil, payload, nil))
	requireSuccess(t, f.request("POST", "/api/auth/invitations/validate", nil, map[string]interface{}{"code": codes[0]}, nil))
	payload["user_name"] = "registered_one"
	requireSuccess(t, f.request("POST", "/api/auth/register", nil, payload, nil))
	registered := f.login("registered_one", integrationPassword)
	identity := f.me(registered)
	if identity["role"] != "user" || identity["library_enabled"] != false || identity["webdav_permission"] != "none" {
		t.Fatal("registered user received implicit privileges")
	}
	requireDenied(t, f.request("POST", "/api/account/invitations", registered, map[string]interface{}{"request_id": "new-user-invite", "note": "not eligible yet"}, nil))
	results = make(chan apiResponse, 5)
	for i := 0; i < 5; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results <- f.request("POST", "/api/auth/register", nil, map[string]interface{}{"user_name": fmt.Sprintf("racing_user_%d", i), "password": integrationPassword, "confirm_password": integrationPassword, "invitation_code": codes[1]}, nil)
		}(i)
	}
	group.Wait()
	close(results)
	registeredCount := 0
	for response := range results {
		if response.Status >= 200 && response.Status < 300 && response.Code == 0 {
			registeredCount++
		} else {
			requireDenied(t, response)
		}
	}
	if registeredCount != 1 {
		t.Fatalf("one invitation registered %d users under concurrency", registeredCount)
	}
	requireSuccess(t, f.publish(owner, "registration", map[string]interface{}{"enabled": false}, "registration-close-test"))
	current := requireSuccess(t, f.request("GET", "/api/admin/config/registration", owner, nil, nil))
	requireSuccess(t, f.request("POST", "/api/admin/config/registration/rollback", owner, map[string]interface{}{"target_revision": 2, "request_id": "registration-rollback-test", "reason": "test epoch permanence", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(number(current["revision"]), 10)}))
	requireDenied(t, f.request("POST", "/api/auth/invitations/validate", nil, map[string]interface{}{"code": codes[2]}, nil))
}

func (f *httpFixture) applicationToken(session *browserSession, scopes []string) string {
	f.t.Helper()
	created := requireSuccess(f.t, f.request("POST", "/api/applications", session, map[string]interface{}{"name": "HTTP fixture application", "description": "synthetic", "scopes": scopes}, nil))
	application := object(created["application"])
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {application["access_key"].(string)}, "client_secret": {created["secret_key"].(string)}}
	response := f.request("POST", "/api/auth/token", nil, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	requireSuccess(f.t, response)
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(response.Raw, &issued) != nil || issued.AccessToken == "" {
		f.t.Fatal("actual application token exchange failed")
	}
	return issued.AccessToken
}

// TestHTTPCapabilitiesAreIndependentAndRevocable 验证管理员与普通用户都需独立藏书/WebDAV授权，且撤权同时约束用户请求和应用 scope。
func TestHTTPCapabilitiesAreIndependentAndRevocable(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	libraryPath := "/library/book/total?offset=0&limit=10"
	requireDenied(t, f.request("GET", libraryPath, owner, nil, nil))
	requireDenied(t, f.request("GET", libraryPath, member, nil, nil))
	requireDenied(t, f.request("POST", "/api/applications", member, map[string]interface{}{"name": "not-authorized", "description": "synthetic", "scopes": []string{"library:read"}}, nil))
	requireSuccess(t, f.adminAction(owner, member.ID, "library-permission", "PUT", map[string]interface{}{"enabled": true}))
	requireSuccess(t, f.request("GET", libraryPath, member, nil, nil))
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword))}
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
	requireSuccess(t, f.adminAction(owner, member.ID, "webdav-permission", "PUT", map[string]interface{}{"permission": "read"}))
	for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
		response := f.request("GET", path, nil, nil, basic)
		if response.Status != 200 || string(response.Raw) != "private fixture file" {
			t.Fatalf("authorized Basic read failed: status=%d", response.Status)
		}
	}
	requireDenied(t, f.request("PUT", "/webdav/written.txt", nil, "should-not-be-written", basic))
	if _, err := os.Stat(filepath.Join(f.conf.WebDAV.SharePath, "written.txt")); !os.IsNotExist(err) {
		t.Fatal("read-only WebDAV user created a file")
	}
	token := f.applicationToken(member, []string{"library:read", "webdav:read", "web-projects:read"})
	bearer := map[string]string{"Authorization": "Bearer " + token}
	requireSuccess(t, f.request("GET", libraryPath, nil, nil, bearer))
	requireDenied(t, f.request("GET", "/api/admin/users", nil, nil, bearer))
	requireSuccess(t, f.adminAction(owner, member.ID, "library-permission", "PUT", map[string]interface{}{"enabled": false}))
	requireDenied(t, f.request("GET", libraryPath, member, nil, nil))
	requireDenied(t, f.request("GET", libraryPath, nil, nil, bearer))
	requireSuccess(t, f.adminAction(owner, member.ID, "webdav-permission", "PUT", map[string]interface{}{"permission": "none"}))
	for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
		requireDenied(t, f.request("GET", path, nil, nil, basic))
		requireDenied(t, f.request("GET", path, nil, nil, bearer))
	}
	requireSuccess(t, f.publish(owner, "webdav", map[string]interface{}{"enabled": false}, "webdav-close-global"))
	requireSuccess(t, f.publish(owner, "webdav", map[string]interface{}{"enabled": true}, "webdav-open-global"))
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
}

// TestHTTPBanRestoreAndDeleteDoNotReviveCredentials 验证封禁使 Cookie、Bearer 和 Basic 访问失效；恢复与删除不会复活旧会话或功能授权。
func TestHTTPBanRestoreAndDeleteDoNotReviveCredentials(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	requireSuccess(t, f.adminAction(owner, member.ID, "webdav-permission", "PUT", map[string]interface{}{"permission": "write"}))
	token := f.applicationToken(member, []string{"web-projects:read", "webdav:read"})
	bearer := map[string]string{"Authorization": "Bearer " + token}
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword))}
	requireSuccess(t, f.request("GET", "/api/web-share", nil, nil, bearer))
	requireSuccess(t, f.adminAction(owner, member.ID, "ban", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	requireDenied(t, f.request("GET", "/api/web-share", nil, nil, bearer))
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
	requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword}, nil))
	requireSuccess(t, f.adminAction(owner, member.ID, "unban", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	requireDenied(t, f.request("GET", "/api/web-share", nil, nil, bearer))
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
	member = f.login(f.member, integrationPassword)
	if f.me(member)["webdav_permission"] != "none" {
		t.Fatal("unban revived WebDAV grant")
	}
	requireSuccess(t, f.adminAction(owner, member.ID, "delete", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword}, nil))
	requireDenied(t, f.request("POST", "/api/admin/users", owner, map[string]interface{}{"user_name": f.member, "generate_password": true}, nil))
}

// TestHTTPAdminPromotionAndSelfProtection 验证授予管理员会撤销旧会话但不附送业务能力，同时阻止管理者对自身执行受保护的降权动作。
func TestHTTPAdminPromotionAndSelfProtection(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	requireDenied(t, f.adminAction(owner, owner.ID, "ban", "POST", map[string]interface{}{}))
	requireDenied(t, f.adminAction(owner, owner.ID, "role", "PUT", map[string]interface{}{"role": "user"}))
	requireSuccess(t, f.adminAction(owner, member.ID, "role", "PUT", map[string]interface{}{"role": "admin"}))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	member = f.login(f.member, integrationPassword)
	requireSuccess(t, f.request("GET", "/api/admin/users", member, nil, nil))
	if identity := f.me(member); identity["library_enabled"] != false || identity["webdav_permission"] != "none" {
		t.Fatal("administrator role implicitly granted risky modules")
	}
	token := f.applicationToken(member, []string{"web-projects:read"})
	requireDenied(t, f.request("GET", "/api/admin/users", nil, nil, map[string]string{"Authorization": "Bearer " + token}))
	users := requireSuccess(t, f.request("GET", "/api/admin/users?query="+f.member, member, nil, nil))
	items := users["items"].([]interface{})
	if len(items) != 1 || number(object(items[0])["application_count"]) != 1 || number(object(items[0])["active_application_count"]) != 1 {
		t.Fatal("administrator user list did not report actual application counts")
	}
	requireDenied(t, f.adminAction(member, member.ID, "delete", "POST", map[string]interface{}{}))
	requireSuccess(t, f.adminAction(member, owner.ID, "role", "PUT", map[string]interface{}{"role": "user"}))
	requireDenied(t, f.request("GET", "/api/admin/users", owner, nil, nil))
	requireDenied(t, f.adminAction(member, member.ID, "role", "PUT", map[string]interface{}{"role": "user"}))
}

// TestHTTPDynamicConfigCASHistoryAndRuntimeStatus 验证配置发布的版本冲突、幂等性与历史回滚，确认公开展示读取到运行时生效的新值。
func TestHTTPDynamicConfigCASHistoryAndRuntimeStatus(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	requireDenied(t, f.request("GET", "/api/admin/config", member, nil, nil))
	before := requireSuccess(t, f.request("GET", "/api/admin/config/site", owner, nil, nil))
	revision := number(before["revision"])
	body := map[string]interface{}{"values": map[string]interface{}{"title": "Changed through HTTP", "notice": "Plain text"}, "request_id": "site-publish-idempotent", "reason": "HTTP CAS", "current_password": integrationPassword}
	headers := map[string]string{"If-Match": strconv.FormatInt(revision, 10)}
	published := requireSuccess(t, f.request("PUT", "/api/admin/config/site", owner, body, headers))
	if number(published["revision"]) != revision+1 {
		t.Fatal("configuration revision did not increase")
	}
	again := requireSuccess(t, f.request("PUT", "/api/admin/config/site", owner, body, headers))
	if number(again["revision"]) != revision+1 {
		t.Fatal("idempotent publish created another revision")
	}
	body["request_id"] = "site-stale-revision"
	conflict := f.request("PUT", "/api/admin/config/site", owner, body, headers)
	if conflict.Status != 409 {
		t.Fatalf("stale configuration revision must return 409, got %d", conflict.Status)
	}
	requireDenied(t, f.request("POST", "/api/admin/config/site/validate", owner, map[string]interface{}{"values": map[string]interface{}{"title": "A", "notice": "", "unknown_key": true}}, nil))
	bootstrap := requireSuccess(t, f.request("GET", "/api/site/bootstrap", nil, nil, nil))
	if object(bootstrap["site"])["title"] != "Changed through HTTP" {
		t.Fatal("published site value not applied")
	}
	status := requireSuccess(t, f.request("GET", "/api/admin/config/status", owner, nil, nil))
	if status["apply_state"] != "applied" || number(status["loaded_generation"]) != number(status["persisted_generation"]) {
		t.Fatal("runtime status does not confirm loaded generation")
	}
	history := requireSuccess(t, f.request("GET", "/api/admin/config/site/history", owner, nil, nil))
	if len(history["items"].([]interface{})) != 2 {
		t.Fatal("configuration history missing seed or publish")
	}
	requireSuccess(t, f.request("POST", "/api/admin/config/site/rollback", owner, map[string]interface{}{"target_revision": revision, "request_id": "site-rollback-request", "reason": "HTTP rollback", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(revision+1, 10)}))
	bootstrap = requireSuccess(t, f.request("GET", "/api/site/bootstrap", nil, nil, nil))
	if object(bootstrap["site"])["title"] != object(before["values"])["title"] {
		t.Fatal("rollback did not create/apply old value as a new revision")
	}
}

func TestHTTPApplicationModuleCanOpenAfterDisabledStartup(t *testing.T) {
	f := newHTTPFixture(t, false)
	owner := f.login(f.owner, integrationPassword)
	current := requireSuccess(t, f.request("GET", "/api/admin/config/auth", owner, nil, nil))
	values := object(current["values"])
	values["applications_enabled"] = true
	requireSuccess(t, f.publish(owner, "auth", values, "enable-app-after-startup"))
	f.applicationToken(owner, []string{"web-projects:read"})
}

// TestHTTPAdminReviewsPrivatePageAndOwnerCannotSelfRestore 验证管理员可预览私有网页并下架或删除，原所有者不能发布或恢复被管理员锁定的内容。
func TestHTTPAdminReviewsPrivatePageAndOwnerCannotSelfRestore(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	project, releaseID := f.privatePage(member)
	id := project["id"].(string)
	users := requireSuccess(t, f.request("GET", "/api/admin/users?query="+f.member, owner, nil, nil))
	items := users["items"].([]interface{})
	if len(items) != 1 || number(object(items[0])["project_count"]) != 1 || number(object(items[0])["published_project_count"]) != 1 {
		t.Fatal("administrator user list did not report actual hosted-page counts")
	}
	requireDenied(t, f.request("GET", "/p/http-private-fixture", guest, nil, nil))
	previewPath := "/api/admin/web-share/" + id + "/releases/" + releaseID + "/preview/index.html"
	requireDenied(t, f.request("GET", previewPath, member, nil, nil))
	preview := f.request("GET", previewPath, owner, nil, nil)
	if preview.Status != 200 || !strings.Contains(string(preview.Raw), "private review fixture") || preview.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("admin private content preview failed: status=%d", preview.Status)
	}
	blocked := requireSuccess(t, f.request("POST", "/api/admin/web-share/"+id+"/block", owner, map[string]interface{}{"reason": "synthetic risk review", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	requireDenied(t, f.request("GET", "/p/http-private-fixture", member, nil, nil))
	requireDenied(t, f.request("PATCH", "/api/web-share/"+id, member, map[string]interface{}{"access_mode": "public"}, map[string]string{"If-Match": strconv.FormatInt(number(blocked["revision"]), 10)}))
	deleted := requireSuccess(t, f.request("POST", "/api/admin/web-share/"+id+"/delete", owner, map[string]interface{}{"reason": "synthetic delete", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(number(blocked["revision"]), 10)}))
	requireDenied(t, f.request("POST", "/api/web-share/"+id+"/restore", member, map[string]interface{}{}, map[string]string{"If-Match": strconv.FormatInt(number(deleted["revision"]), 10)}))
	restored := requireSuccess(t, f.request("POST", "/api/admin/web-share/"+id+"/restore", owner, map[string]interface{}{"reason": "synthetic restore", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(number(deleted["revision"]), 10)}))
	if restored["moderation_status"] != "blocked" {
		t.Fatal("admin restore must not automatically unblock risky content")
	}
	requireSuccess(t, f.request("GET", "/api/admin/audit-logs?target_type=web_project&target_id="+id, owner, nil, nil))
}

func TestHTTPLogoutOneVersusEverySession(t *testing.T) {
	f := newHTTPFixture(t)
	first := f.login(f.member, integrationPassword)
	second := f.login(f.member, integrationPassword)
	third := f.login(f.member, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	requireSuccess(t, f.request("POST", "/api/auth/logout", first, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", first, nil, nil))
	requireSuccess(t, f.request("GET", "/api/auth/me", second, nil, nil))
	requireSuccess(t, f.request("GET", "/api/auth/me", third, nil, nil))
	requireSuccess(t, f.request("POST", "/api/auth/logout-all", second, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", second, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", third, nil, nil))
	requireSuccess(t, f.request("GET", "/api/auth/me", guest, nil, nil))
}

func TestHTTPGeneratedInitialPasswordUsesCurrentPolicy(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	policy := requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", owner, nil, nil))
	values := object(policy["values"])
	values["min_password_length"] = 32
	requireSuccess(t, f.publish(owner, "account_policy", values, "password-policy-minimum-32"))
	created := requireSuccess(t, f.request("POST", "/api/admin/users", owner, map[string]interface{}{"user_name": "long_password_user", "generate_password": true}, nil))
	password := created["initial_password"].(string)
	if len(password) < 32 {
		t.Fatal("generated initial password is below current policy minimum")
	}
	session := f.login("long_password_user", password)
	reset := requireSuccess(t, f.adminAction(owner, session.ID, "reset-password", "POST", map[string]interface{}{"generate_password": true}))
	if len(reset["initial_password"].(string)) < 32 {
		t.Fatal("reset initial password is below current policy minimum")
	}
	requireDenied(t, f.request("GET", "/api/auth/me", session, nil, nil))
}

func TestHTTPExpiredInitialPasswordInvalidatesRestrictedSession(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	created := requireSuccess(t, f.request("POST", "/api/admin/users", owner, map[string]interface{}{"user_name": "expired_temporary", "password": integrationNextPassword}, nil))
	session := f.login("expired_temporary", integrationNextPassword)
	id := object(created["user"])["id"].(string)
	if _, err := f.database.Exec("UPDATE user_account SET password_expires_at=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	requireDenied(t, f.request("GET", "/api/auth/me", session, nil, nil))
	requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": "expired_temporary", "password": integrationNextPassword}, nil))
	requireDenied(t, f.request("POST", "/api/auth/change-password", session, map[string]interface{}{"current_password": integrationNextPassword, "new_password": integrationPassword, "confirm_password": integrationPassword}, nil))
}

// privatePage 通过真实上传与发布接口建立一份仅所有者可见的合成 HTML 网页，返回项目和版本供审核测试使用。
func (f *httpFixture) privatePage(member *browserSession) (map[string]interface{}, string) {
	f.t.Helper()
	project := requireSuccess(f.t, f.request("POST", "/api/web-share", member, map[string]interface{}{"name": "Private review fixture", "description": "synthetic", "slug": "http-private-fixture", "access_mode": "owner", "member_user_ids": []string{}}, nil))
	id := project["id"].(string)
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	for name, content := range map[string]string{"index.html": "<html><link rel=stylesheet href=style.css><body>private review fixture<script src=app.js></script></body></html>", "style.css": "body{color:#123}", "app.js": "window.fixture=true;"} {
		entry, err := zipWriter.Create(name)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		f.t.Fatal(err)
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("file", "fixture.zip")
	if err != nil {
		f.t.Fatal(err)
	}
	part.Write(archive.Bytes())
	writer.WriteField("entry_file", "index.html")
	writer.Close()
	release := requireSuccess(f.t, f.request("POST", "/api/web-share/"+id+"/releases", member, upload.Bytes(), map[string]string{"Content-Type": writer.FormDataContentType(), "Idempotency-Key": "http-review-upload"}))
	releaseID := release["id"].(string)
	project = requireSuccess(f.t, f.request("GET", "/api/web-share/"+id, member, nil, nil))
	project = requireSuccess(f.t, f.request("POST", "/api/web-share/"+id+"/publish", member, map[string]interface{}{"release_id": releaseID}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	return project, releaseID
}

// TestHTTPAdminPreviewRequiresDurableAudit 验证管理员预览文档必须先落审计，资产请求不重复记文档访问；模拟审计失败时不得泄露文件。
func TestHTTPAdminPreviewRequiresDurableAudit(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	project, releaseID := f.privatePage(member)
	id := project["id"].(string)
	previewPath := "/api/admin/web-share/" + id + "/releases/" + releaseID + "/preview/index.html"
	preview := f.request("GET", previewPath, owner, nil, nil)
	if preview.Status != 200 {
		t.Fatalf("authorized preview failed: status=%d", preview.Status)
	}
	var auditCount int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM admin_audit_log WHERE actor_user_id=? AND target_type='web_project' AND target_id=?", owner.ID, id).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Errorf("opening private review entry must persist one audit; got %d", auditCount)
	}
	for _, asset := range []string{"style.css", "app.js"} {
		response := f.request("GET", strings.TrimSuffix(previewPath, "index.html")+asset, owner, nil, nil)
		if response.Status != 200 {
			t.Fatalf("authorized preview asset failed: status=%d", response.Status)
		}
	}
	if err := f.database.QueryRow("SELECT COUNT(*) FROM admin_audit_log WHERE actor_user_id=? AND target_type='web_project' AND target_id=?", owner.ID, id).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Errorf("CSS/JS preview must not duplicate entry audit; got %d", auditCount)
	}
	if _, err := f.database.Exec("CREATE TRIGGER reject_preview_audit BEFORE INSERT ON admin_audit_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic audit failure'"); err != nil {
		t.Fatal(err)
	}
	preview = f.request("GET", previewPath, owner, nil, nil)
	if preview.Status < 500 || strings.Contains(string(preview.Raw), "private review fixture") {
		t.Error("private preview content must not be returned when audit persistence fails")
	}
}

// TestHTTPLibraryWritesRequireCapabilityAndCorrectCredentialScope 验证 Cookie 与应用凭证写入共享藏书的独立授权和 scope，读权限不能借写接口修改库存或地址。
func TestHTTPLibraryWritesRequireCapabilityAndCorrectCredentialScope(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	address := map[string]interface{}{"address": "Synthetic HTTP shelf", "short_name": "HTTP"}
	requireDenied(t, f.request("POST", "/library/address/add", member, address, nil))
	requireSuccess(t, f.adminAction(owner, member.ID, "library-permission", "PUT", map[string]interface{}{"enabled": true}))
	requireSuccess(t, f.request("POST", "/library/address/add", member, address, nil))
	requireDenied(t, f.request("POST", "/library/address/add", member, address, map[string]string{"X-CSRF-Token": ""}))
	readBearer := map[string]string{"Authorization": "Bearer " + f.applicationToken(member, []string{"library:read"})}
	requireDenied(t, f.request("POST", "/library/address/add", nil, address, readBearer))
	writeBearer := map[string]string{"Authorization": "Bearer " + f.applicationToken(member, []string{"library:write"})}
	requireSuccess(t, f.request("POST", "/library/address/add", nil, address, writeBearer))
	// Quantity is present in the deployed legacy inventory schema but absent from
	// its historical create_table.sql; add that existing field only to this fixture.
	if _, err := f.database.Exec("ALTER TABLE book_storage ADD COLUMN quantity INT NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("INSERT INTO bookinfo (id,title,isbn13) VALUES (1,'Synthetic HTTP Book','9780000000002'),(2,'Second Synthetic Book','9780000000003')"); err != nil {
		t.Fatal(err)
	}
	book := map[string]interface{}{"isbn": "9780000000002", "quantity": 1, "type": 1, "lib_id": 1}
	requireDenied(t, f.request("POST", "/library/book/add", nil, book, readBearer))
	requireSuccess(t, f.request("POST", "/library/book/add", nil, book, writeBearer))
	requireSuccess(t, f.adminAction(owner, member.ID, "library-permission", "PUT", map[string]interface{}{"enabled": false}))
	requireDenied(t, f.request("POST", "/library/address/add", member, address, nil))
	requireDenied(t, f.request("POST", "/library/address/add", nil, address, writeBearer))
	book["isbn"] = "9780000000003"
	requireDenied(t, f.request("POST", "/library/book/add", nil, book, writeBearer))
	var addressCount, bookCount int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM book_address").Scan(&addressCount); err != nil {
		t.Fatal(err)
	}
	if err := f.database.QueryRow("SELECT COUNT(*) FROM book_storage").Scan(&bookCount); err != nil {
		t.Fatal(err)
	}
	if addressCount != 2 || bookCount != 1 {
		t.Fatalf("forbidden library requests changed storage: addresses=%d books=%d", addressCount, bookCount)
	}
}

// blockedRequestBody signals the handler's first body Read and pauses there.
// Authentication middleware never reads these multipart/JSON bodies, so the
// signal establishes that the original Bearer principal has already been accepted.
// No timer is used to guess when authorization happened.
type blockedRequestBody struct {
	reader      *bytes.Reader
	entered     chan struct{}
	release     chan struct{}
	finished    chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func (b *blockedRequestBody) Read(buffer []byte) (int, error) {
	b.enterOnce.Do(func() { close(b.entered); <-b.release })
	return b.reader.Read(buffer)
}

// beginPausedRequest 异步发起在读取正文时阻塞的请求，并等待确定的暂停点，便于在认证和提交之间插入权限变更。
func (f *httpFixture) beginPausedRequest(path, token, contentType string, payload []byte) (*blockedRequestBody, <-chan apiResponse) {
	f.t.Helper()
	body := &blockedRequestBody{reader: bytes.NewReader(payload), entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	done := make(chan apiResponse, 1)
	go func() {
		defer close(body.finished)
		done <- f.request("POST", path, nil, body, map[string]string{"Authorization": "Bearer " + token, "Content-Type": contentType, "Idempotency-Key": "paused-upload-request"})
	}()
	f.t.Cleanup(func() {
		body.releaseOnce.Do(func() { close(body.release) })
		select {
		case <-body.finished:
		case <-time.After(5 * time.Second):
			f.t.Error("paused server goroutine did not exit before fixture cleanup")
		}
	})
	select {
	case <-body.entered:
	case response := <-done:
		f.t.Fatalf("request ended before reaching authenticated body read: status=%d code=%d", response.Status, response.Code)
	case <-time.After(30 * time.Second):
		f.t.Fatal("request did not reach authenticated body read")
	}
	return body, done
}

func (f *httpFixture) finishPausedRequest(body *blockedRequestBody, done <-chan apiResponse) apiResponse {
	f.t.Helper()
	body.releaseOnce.Do(func() { close(body.release) })
	select {
	case response := <-done:
		return response
	case <-time.After(30 * time.Second):
		f.t.Fatal("resumed request did not finish")
	}
	return apiResponse{}
}

// mutateApplication 为并发集成用例执行指定的应用关闭、吊销、密钥轮换或重新启用动作，确保响应完成后再放行在途请求。
func (f *httpFixture) mutateApplication(session *browserSession, id, action string) {
	f.t.Helper()
	if action == "disable_enable" {
		f.mutateApplication(session, id, "disable")
		f.mutateApplication(session, id, "enable")
		return
	}
	current := requireSuccess(f.t, f.request("GET", "/api/applications/"+id, session, nil, nil))
	headers := map[string]string{"If-Match": strconv.FormatInt(number(current["revision"]), 10)}
	switch action {
	case "revoke":
		requireSuccess(f.t, f.request("DELETE", "/api/applications/"+id, session, nil, headers))
	case "rotate":
		requireSuccess(f.t, f.request("POST", "/api/applications/"+id+"/rotate", session, nil, headers))
	case "disable":
		requireSuccess(f.t, f.request("PATCH", "/api/applications/"+id, session, map[string]interface{}{"status": "disabled"}, headers))
	case "enable":
		requireSuccess(f.t, f.request("PATCH", "/api/applications/"+id, session, map[string]interface{}{"status": "enabled"}, headers))
	default:
		f.t.Fatal("unknown synthetic application action")
	}
}

// TestHTTPSlowBearerUploadRechecksApplicationAtCommit 将真实上传暂停在入口认证之后，变更应用状态再继续发送正文，验证最终事务拒绝已失效的凭据快照。
func TestHTTPSlowBearerUploadRechecksApplicationAtCommit(t *testing.T) {
	for _, action := range []string{"module_close", "revoke", "rotate", "disable_enable"} {
		// 在当前应用变更场景中暂停上传、完成凭证变更并验证拒绝后没有新增版本或改动发布指针。
		t.Run(action, func(t *testing.T) {
			f := newHTTPFixture(t)
			owner := f.login(f.owner, integrationPassword)
			member := f.login(f.member, integrationPassword)
			project, releaseID := f.privatePage(member)
			projectID := project["id"].(string)
			originalRevision := number(project["revision"])
			token := f.applicationToken(member, []string{"web-projects:write"})
			applications := requireSuccess(t, f.request("GET", "/api/applications", member, nil, nil))
			applicationID := object(applications["items"].([]interface{})[0])["id"].(string)
			var beforeCount int
			if err := f.database.QueryRow("SELECT COUNT(*) FROM web_project_release WHERE project_id=?", projectID).Scan(&beforeCount); err != nil {
				t.Fatal(err)
			}
			var payload bytes.Buffer
			writer := multipart.NewWriter(&payload)
			part, err := writer.CreateFormFile("file", "slow.html")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("<html><body>must never be committed</body></html>")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			blocked, done := f.beginPausedRequest("/api/web-share/"+projectID+"/releases", token, writer.FormDataContentType(), payload.Bytes())
			if action == "module_close" {
				namespace := requireSuccess(t, f.request("GET", "/api/admin/config/auth", owner, nil, nil))
				values := object(namespace["values"])
				values["applications_enabled"] = false
				requireSuccess(t, f.publish(owner, "auth", values, "paused-upload-close-auth"))
			} else {
				f.mutateApplication(member, applicationID, action)
			}
			response := f.finishPausedRequest(blocked, done)
			if response.Status < 400 && response.Code == 0 {
				t.Errorf("stale Bearer upload accepted after %s committed: status=%d", action, response.Status)
			}
			if response.Status >= 500 {
				t.Errorf("expected authorization denial rather than server failure: status=%d code=%d", response.Status, response.Code)
			}
			var count int
			var currentRelease string
			var revision int64
			if err := f.database.QueryRow("SELECT COUNT(*) FROM web_project_release WHERE project_id=?", projectID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if err := f.database.QueryRow("SELECT CAST(current_release_id AS CHAR),revision FROM web_project WHERE id=?", projectID).Scan(&currentRelease, &revision); err != nil {
				t.Fatal(err)
			}
			if count != beforeCount || currentRelease != releaseID || revision != originalRevision {
				t.Errorf("stale upload changed release/project after %s: releases %d -> %d, revision %d -> %d", action, beforeCount, count, originalRevision, revision)
			}
		})
	}
}

// TestHTTPSlowLibraryWriteRechecksApplicationSnapshot 将藏书写请求暂停在认证与解码之间，轮换或禁启应用后验证旧凭据不能完成库存写入。
func TestHTTPSlowLibraryWriteRechecksApplicationSnapshot(t *testing.T) {
	for _, action := range []string{"rotate", "disable_enable"} {
		// 在当前凭证变更场景中放行旧藏书请求，确认失败返回且数据库没有新增地址。
		t.Run(action, func(t *testing.T) {
			f := newHTTPFixture(t)
			owner := f.login(f.owner, integrationPassword)
			member := f.login(f.member, integrationPassword)
			requireSuccess(t, f.adminAction(owner, member.ID, "library-permission", "PUT", map[string]interface{}{"enabled": true}))
			token := f.applicationToken(member, []string{"library:write"})
			applications := requireSuccess(t, f.request("GET", "/api/applications", member, nil, nil))
			applicationID := object(applications["items"].([]interface{})[0])["id"].(string)
			payload, _ := json.Marshal(map[string]interface{}{"address": "must never be committed", "short_name": "STALE"})
			blocked, done := f.beginPausedRequest("/library/address/add", token, "application/json", payload)
			f.mutateApplication(member, applicationID, action)
			response := f.finishPausedRequest(blocked, done)
			if response.Status < 400 && response.Code == 0 {
				t.Errorf("stale library write accepted after %s committed", action)
			}
			if response.Status >= 500 {
				t.Errorf("expected authorization denial rather than server failure: status=%d code=%d", response.Status, response.Code)
			}
			var count int
			if err := f.database.QueryRow("SELECT COUNT(*) FROM book_address").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("stale Bearer wrote %d library rows after %s", count, action)
			}
		})
	}
}
