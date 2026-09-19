package manuals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	applicationservice "github.com/mcoder2014/home_server/domain/service/applications"
	manualservice "github.com/mcoder2014/home_server/domain/service/manuals"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

const manualTestOrigin = "https://manuals.example.test"

type manualHTTPSession struct {
	cookie *http.Cookie
	csrf   string
}

type manualHTTPResponse struct {
	status int
	code   int
	data   map[string]interface{}
	body   []byte
	header http.Header
}

type manualHTTPFixture struct {
	t              *testing.T
	router         *gin.Engine
	database       *sql.DB
	storageRoot    string
	owner, member  *manualHTTPSession
	readToken      string
	ownerID        int64
	originalConfig config.Config
	originalRoutes map[string]map[string]data.HttpRoute
}

func newManualHTTPFixture(t *testing.T) *manualHTTPFixture {
	t.Helper()
	dsn := os.Getenv("MANUALS_TEST_DSN")
	if dsn == "" {
		t.Skip("MANUALS_TEST_DSN is required for manual HTTP integration tests")
	}
	parsed, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(parsed.DBName, "home_server_manuals_test_"), "refuse non-fixture database")
	parsed.MultiStatements = false
	parsed.ParseTime = true
	sqlDB, err := sql.Open("mysql", parsed.FormatDSN())
	require.NoError(t, err)
	dropManualTestTables(t, sqlDB, parsed.DBName)
	for _, ddl := range []string{manualAccountTestDDL, manualSessionTestDDL} {
		_, err = sqlDB.Exec(ddl)
		require.NoError(t, err)
	}
	for _, name := range []string{"20260913_runtime_config.sql", "20260912_applications.sql", "20260919_manuals.sql"} {
		raw, readErr := migrations.SQL.ReadFile(name)
		require.NoError(t, readErr)
		executeManualTestDDL(t, sqlDB, string(raw))
	}

	root := t.TempDir()
	conf := config.Config{IdentitySource: "database", ConfigSource: "database"}
	conf.Auth.ApplicationsEnabled = true
	conf.Auth.SiteOrigin = manualTestOrigin
	conf.Manuals.Enabled = true
	conf.Manuals.StorageRoot = filepath.Join(root, "manuals")
	require.NoError(t, manualservice.Init(&conf.Manuals, "", ""))
	seedManualRuntimeConfig(t, sqlDB, conf)
	seedManualIdentities(t, sqlDB)
	readToken := seedManualApplication(t, sqlDB)

	originalConfig, originalRoutes := config.Global(), data.RouterMap
	config.SetGlobalConfig(conf)
	require.NoError(t, db.InitDatabase(parsed.FormatDSN()))
	db.MasterDB().Logger = logger.Discard
	runtimeService := siteconfig.New(db.MasterDB(), conf)
	require.NoError(t, runtimeService.Initialize(context.Background()))
	require.NoError(t, applicationservice.Init(conf.Auth))
	require.NoError(t, middleware.ConfigureAuthentication(conf.Auth))

	data.RouterMap = map[string]map[string]data.HttpRoute{}
	require.NoError(t, InitRouter())
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.RecoveryWithWriter(io.Discard), middleware.AddLogID)
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		router.Handle(method, path, handlers...)
	})
	fixture := &manualHTTPFixture{
		t: t, router: router, database: sqlDB, storageRoot: conf.Manuals.StorageRoot,
		owner:     manualBrowserSession(t, sqlDB, 1001, "owner-session-token"),
		member:    manualBrowserSession(t, sqlDB, 1002, "member-session-token"),
		readToken: readToken, ownerID: 1001, originalConfig: originalConfig, originalRoutes: originalRoutes,
	}
	t.Cleanup(func() {
		config.SetGlobalConfig(fixture.originalConfig)
		data.RouterMap = fixture.originalRoutes
		if connection, connectionErr := db.MasterDB().DB(); connectionErr == nil {
			_ = connection.Close()
		}
		_ = sqlDB.Close()
	})
	return fixture
}

func dropManualTestTables(t *testing.T, database *sql.DB, schema string) {
	t.Helper()
	rows, err := database.Query("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?", schema)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	require.NoError(t, rows.Close())
	for _, table := range tables {
		_, err = database.Exec("DROP TABLE `" + strings.ReplaceAll(table, "`", "``") + "`")
		require.NoError(t, err)
	}
}

func executeManualTestDDL(t *testing.T, database *sql.DB, raw string) {
	t.Helper()
	lines := make([]string, 0)
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
		require.False(t, strings.HasPrefix(strings.ToUpper(statement), "USE "))
		_, err := database.Exec(statement)
		require.NoError(t, err)
	}
}

func seedManualRuntimeConfig(t *testing.T, database *sql.DB, conf config.Config) {
	t.Helper()
	now := time.Now().UTC()
	_, err := database.Exec("INSERT INTO site_runtime_state (id, config_generation, registration_epoch, revision, update_time) VALUES (1, 1, 1, 1, ?)", now)
	require.NoError(t, err)
	for namespace, values := range config.DefaultRuntimeValues(conf) {
		raw, marshalErr := json.Marshal(values)
		require.NoError(t, marshalErr)
		digest := sha256.Sum256(raw)
		_, err = database.Exec("INSERT INTO site_config_current (namespace, revision, schema_version, values_json, values_sha256, updated_by, update_time) VALUES (?, 1, 1, ?, ?, 1001, ?)", namespace, string(raw), digest[:], now)
		require.NoError(t, err)
	}
}

func seedManualIdentities(t *testing.T, database *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	for id, name := range map[int64]string{1001: "owner", 1002: "member"} {
		_, err := database.Exec("INSERT INTO user_account (id, username, username_key, display_name, avatar_version, contact_email, contact_mobile, password_hash, status, role, library_enabled, webdav_permission, auth_version, revision, must_change_password, invite_eligible_at, source, create_time, update_time) VALUES (?, ?, ?, ?, 0, '', '', '', 'active', 'user', FALSE, 'none', 1, 1, FALSE, ?, 'test', ?, ?)", id, name, name, name, now, now, now)
		require.NoError(t, err)
	}
}

func manualBrowserSession(t *testing.T, database *sql.DB, userID int64, token string) *manualHTTPSession {
	t.Helper()
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(token))
	_, err := database.Exec("INSERT INTO login_token (id, user_id, token_digest, auth_version, purpose, is_expired, authenticated_at, login_ip, user_agent, client_name, os_name, device_type, login_source, expire_time, create_time, update_time) VALUES (?, ?, ?, 1, 'user', 0, ?, '127.0.0.1', 'manual-test', 'test', 'test', 'test', 'test', ?, ?, ?)", userID, userID, digest[:], now, now.Add(time.Hour), now, now)
	require.NoError(t, err)
	return &manualHTTPSession{cookie: &http.Cookie{Name: utils.SessionCookieName, Value: token, Path: "/", Secure: true, HttpOnly: true}, csrf: middleware.CSRFToken(token)}
}

func seedManualApplication(t *testing.T, database *sql.DB) string {
	t.Helper()
	rawToken := "at_cq_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	digest := sha256.Sum256([]byte(rawToken))
	now := time.Now().UTC()
	scopes := `["manuals:read"]`
	_, err := database.Exec("INSERT INTO application (id, owner_user_id, name, description, access_key, secret_digest, scopes, status, active_slot, revision, secret_version, expires_at, create_time, update_time) VALUES (2001, 1001, 'reader', '', 'ak_cq_AAAAAAAAAAAAAAAAAAAAAA', ?, ?, 1, 1, 1, 1, ?, ?, ?)", make([]byte, 32), scopes, now.Add(time.Hour), now, now)
	require.NoError(t, err)
	_, err = database.Exec("INSERT INTO application_access_token (id, application_id, token_digest, secret_version, application_revision, scope_snapshot, expired_at, create_time) VALUES (3001, 2001, ?, 1, 1, ?, ?, ?)", digest[:], scopes, now.Add(time.Hour), now)
	require.NoError(t, err)
	return rawToken
}

func (fixture *manualHTTPFixture) request(method, path string, session *manualHTTPSession, body interface{}, headers map[string]string) manualHTTPResponse {
	fixture.t.Helper()
	var reader io.Reader
	switch value := body.(type) {
	case nil:
		reader = http.NoBody
	case io.Reader:
		reader = value
	case []byte:
		reader = bytes.NewReader(value)
	default:
		raw, err := json.Marshal(value)
		require.NoError(fixture.t, err)
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, manualTestOrigin+path, reader)
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.RemoteAddr = "192.0.2.10:45000"
	request.Header.Set("Origin", manualTestOrigin)
	request.Header.Set("Content-Type", "application/json")
	if session != nil {
		request.AddCookie(session.cookie)
		request.Header.Set("X-CSRF-Token", session.csrf)
	}
	for key, value := range headers {
		if value == "" {
			request.Header.Del(key)
		} else {
			request.Header.Set(key, value)
		}
	}
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	response := manualHTTPResponse{status: recorder.Code, body: append([]byte(nil), recorder.Body.Bytes()...), header: recorder.Header()}
	var envelope struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	if json.Unmarshal(response.body, &envelope) == nil {
		response.code, response.data = envelope.Code, envelope.Data
	}
	return response
}

func (fixture *manualHTTPFixture) multipart(fields map[string]string, filename string, content []byte) (*bytes.Buffer, string) {
	fixture.t.Helper()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	for name, value := range fields {
		require.NoError(fixture.t, writer.WriteField(name, value))
	}
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(fixture.t, err)
	_, err = part.Write(content)
	require.NoError(fixture.t, err)
	require.NoError(fixture.t, writer.Close())
	return buffer, writer.FormDataContentType()
}

func requireManualSuccess(t *testing.T, response manualHTTPResponse) map[string]interface{} {
	t.Helper()
	if response.status < 200 || response.status >= 300 || response.code != 0 {
		t.Fatalf("manual request failed: status=%d code=%d body=%s", response.status, response.code, response.body)
	}
	return response.data
}

func requireManualDenied(t *testing.T, response manualHTTPResponse) {
	t.Helper()
	if response.status < 400 || response.status >= 500 || response.code == 0 {
		t.Fatalf("manual request was not safely denied: status=%d code=%d body=%s", response.status, response.code, response.body)
	}
}

func manualRevision(value map[string]interface{}) int64 {
	revision, _ := value["revision"].(float64)
	return int64(revision)
}

func TestManualHTTPPermissionsIdempotencyAndProtectedFiles(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	create := map[string]interface{}{"name": "冰箱100%_!", "description": "家庭说明", "categories": []string{"厨房", "Kitchen"}, "access_mode": "owner", "client_request_id": "create-1"}
	requireManualDenied(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, create, map[string]string{"X-CSRF-Token": ""}))
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, create, nil))
	manualID := created["id"].(string)
	require.Equal(t, int64(1), manualRevision(created))
	require.ElementsMatch(t, []interface{}{"Kitchen", "厨房"}, created["categories"].([]interface{}))
	replayed := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, create, nil))
	require.Equal(t, manualID, replayed["id"])
	require.Equal(t, int64(1), manualRevision(replayed))
	conflictingCreate := map[string]interface{}{"name": "另一份", "categories": []string{}, "access_mode": "owner", "client_request_id": "create-1"}
	requireManualDenied(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, conflictingCreate, nil))

	inline := map[string]interface{}{"kind": "text", "title": "保养", "text": "每月清洁滤网", "client_request_id": "inline-1"}
	inlineResult := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/items", fixture.owner, inline, nil))
	inlineItem := inlineResult["item"].(map[string]interface{})
	require.Equal(t, int64(2), manualRevision(inlineResult))
	inlineReplay := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/items", fixture.owner, inline, nil))
	require.Equal(t, inlineItem["id"], inlineReplay["item"].(map[string]interface{})["id"])
	require.Equal(t, int64(2), manualRevision(inlineReplay))

	private := requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 2, "status": "active"}, nil))
	require.Equal(t, int64(3), manualRevision(private))
	requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, fixture.owner, nil, nil))
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, fixture.member, nil, nil))
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, nil))
	requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, map[string]string{"Authorization": "Bearer " + fixture.readToken}))
	requireManualDenied(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/items", nil, map[string]interface{}{"kind": "url", "url": "https://example.com", "client_request_id": "app-write"}, map[string]string{"Authorization": "Bearer " + fixture.readToken}))

	authenticated := requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 3, "access_mode": "authenticated"}, nil))
	require.Equal(t, int64(4), manualRevision(authenticated))
	requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, fixture.member, nil, nil))
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, nil))
	public := requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 4, "access_mode": "public"}, nil))
	require.Equal(t, int64(5), manualRevision(public))
	requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, nil))
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, map[string]string{"Authorization": "Bearer invalid"}))
	stale := &manualHTTPSession{cookie: &http.Cookie{Name: utils.SessionCookieName, Value: "missing-session-token", Path: "/"}}
	requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, stale, nil, nil))
	private = requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 5, "access_mode": "owner"}, nil))
	require.Equal(t, int64(6), manualRevision(private))
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, nil))
	public = requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 6, "access_mode": "public"}, nil))
	require.Equal(t, int64(7), manualRevision(public))

	pngBytes := manualPNG(t)
	upload, contentType := fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "image-1"}, "plate.png", pngBytes)
	uploaded := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, upload, map[string]string{"Content-Type": contentType}))
	imageItem := uploaded["item"].(map[string]interface{})
	imageID := imageItem["id"].(string)
	require.Equal(t, "ready", imageItem["preview_status"])
	require.Equal(t, int64(8), manualRevision(uploaded))
	upload, contentType = fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "image-1"}, "plate.png", pngBytes)
	uploadReplay := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, upload, map[string]string{"Content-Type": contentType}))
	require.Equal(t, imageID, uploadReplay["item"].(map[string]interface{})["id"])
	require.Equal(t, int64(8), manualRevision(uploadReplay))

	contentPath := "/api/manuals/" + manualID + "/items/" + imageID + "/content"
	thumbnailPath := "/api/manuals/" + manualID + "/items/" + imageID + "/thumbnail"
	original := fixture.request(http.MethodGet, contentPath, nil, nil, nil)
	require.Equal(t, http.StatusOK, original.status)
	require.Equal(t, pngBytes, original.body)
	require.Equal(t, "private, no-store", original.header.Get("Cache-Control"))
	require.Equal(t, "nosniff", original.header.Get("X-Content-Type-Options"))
	head := fixture.request(http.MethodHead, contentPath, nil, nil, nil)
	require.Equal(t, http.StatusOK, head.status)
	require.Empty(t, head.body)
	partial := fixture.request(http.MethodGet, contentPath, nil, nil, map[string]string{"Range": "bytes=0-7"})
	require.Equal(t, http.StatusPartialContent, partial.status)
	require.Equal(t, pngBytes[:8], partial.body)
	thumbnail := fixture.request(http.MethodGet, thumbnailPath, nil, nil, nil)
	require.Equal(t, http.StatusOK, thumbnail.status)
	require.Equal(t, "image/jpeg", thumbnail.header.Get("Content-Type"))
	require.Greater(t, len(thumbnail.body), 10)

	private = requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 8, "access_mode": "owner"}, nil))
	require.Equal(t, int64(9), manualRevision(private))
	requireManualDenied(t, fixture.request(http.MethodGet, contentPath, nil, nil, map[string]string{"Range": "bytes=0-7"}))
	require.Equal(t, http.StatusPartialContent, fixture.request(http.MethodGet, contentPath, fixture.owner, nil, map[string]string{"Range": "bytes=0-7"}).status)
	public = requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 9, "access_mode": "public"}, nil))
	require.Equal(t, int64(10), manualRevision(public))

	textBytes := []byte("第一行\n第二行")
	textUpload, textContentType := fixture.multipart(map[string]string{"title": "备注", "client_request_id": "text-file-1"}, "notes.txt", textBytes)
	textResult := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, textUpload, map[string]string{"Content-Type": textContentType}))
	textItem := textResult["item"].(map[string]interface{})
	require.Equal(t, "text", textItem["kind"])
	require.Equal(t, string(textBytes), textItem["text"])
	require.Empty(t, textItem["content_url"])
	require.Equal(t, int64(11), manualRevision(textResult))
	_, err := os.Stat(filepath.Join(fixture.storageRoot, strconv.FormatInt(fixture.ownerID, 10), manualID, textItem["id"].(string)))
	require.ErrorIs(t, err, os.ErrNotExist)

	itemDirectory := filepath.Join(fixture.storageRoot, strconv.FormatInt(fixture.ownerID, 10), manualID, imageID)
	require.DirExists(t, itemDirectory)
	deleted := requireManualSuccess(t, fixture.request(http.MethodDelete, "/api/manuals/"+manualID+"/items/"+imageID, fixture.owner, map[string]interface{}{"revision": 11}, nil))
	require.Equal(t, int64(12), manualRevision(deleted))
	require.DirExists(t, itemDirectory, "logical deletion must retain original bytes")
	requireManualDenied(t, fixture.request(http.MethodGet, contentPath, fixture.owner, nil, nil))
	upload, contentType = fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "image-1"}, "plate.png", pngBytes)
	requireManualDenied(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, upload, map[string]string{"Content-Type": contentType}))

	_, err = fixture.database.Exec("UPDATE user_account SET status = 'banned' WHERE id = ?", fixture.ownerID)
	require.NoError(t, err)
	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/"+manualID, nil, nil, nil))
}

func manualPNG(t *testing.T) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, 4, 3))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	imageData.Set(3, 2, color.RGBA{B: 255, A: 255})
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, imageData))
	return buffer.Bytes()
}

const manualAccountTestDDL = `
CREATE TABLE user_account (
    id BIGINT NOT NULL PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    username_key VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    display_name VARCHAR(64) NOT NULL DEFAULT '',
    avatar_version BIGINT NOT NULL DEFAULT 0,
    contact_email VARCHAR(254) NOT NULL DEFAULT '',
    contact_mobile VARCHAR(32) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    role VARCHAR(16) NOT NULL DEFAULT 'user',
    library_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    webdav_permission VARCHAR(16) NOT NULL DEFAULT 'none',
    auth_version BIGINT NOT NULL DEFAULT 1,
    revision BIGINT NOT NULL DEFAULT 1,
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    password_expires_at DATETIME(6) NULL,
    invite_eligible_at DATETIME(6) NOT NULL,
    source VARCHAR(16) NOT NULL,
    invited_by_user_id BIGINT NULL,
    created_by_user_id BIGINT NULL,
    imported_at DATETIME(6) NULL,
    last_login_at DATETIME(6) NULL,
    deleted_at DATETIME(6) NULL,
    create_time DATETIME(6) NOT NULL,
    update_time DATETIME(6) NOT NULL,
    UNIQUE KEY uk_account_username (username_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const manualSessionTestDDL = `
CREATE TABLE login_token (
    id BIGINT NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    token_digest BINARY(32) NOT NULL,
    auth_version BIGINT NOT NULL,
    purpose VARCHAR(32) NOT NULL,
    is_expired INT NOT NULL DEFAULT 0,
    authenticated_at DATETIME(6) NOT NULL,
    login_ip VARCHAR(128) NOT NULL,
    user_agent VARCHAR(512) NOT NULL,
    client_name VARCHAR(128) NOT NULL,
    os_name VARCHAR(128) NOT NULL,
    device_type VARCHAR(128) NOT NULL,
    login_source VARCHAR(64) NOT NULL,
    expire_time DATETIME(6) NOT NULL,
    create_time DATETIME(6) NOT NULL,
    update_time DATETIME(6) NOT NULL,
    UNIQUE KEY uk_login_token_digest (token_digest),
    KEY idx_login_token_user (user_id, is_expired, expire_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
