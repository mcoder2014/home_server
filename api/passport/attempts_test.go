package passport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	passportservice "github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
)

// TestDatabaseLegacyLoginRequiresHTTPS 验证数据库模式的旧 RSA 登录同样要求可信 HTTPS，不能只靠客户端填写转发头。
func TestDatabaseLegacyLoginRequiresHTTPS(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old) })
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	if err := middleware.ConfigureAuthentication(config.AuthConfig{}); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.POST("/passport/login", Login)
	for _, forwarded := range []string{"", "https"} {
		request := httptest.NewRequest("POST", "http://example.com/passport/login", strings.NewReader(`{}`))
		request.RemoteAddr = "198.51.100.91:1234"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-Proto", forwarded)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != 403 {
			t.Errorf("database legacy login over HTTP (untrusted XFP %q) returned %d, want 403", forwarded, recorder.Code)
		}
	}
}

// TestLegacyPasswordFailuresKeepBasicIndependent 检查旧 RSA 登录的用户名与别名共享失败预算，同时保留原生 Basic 的独立预算。
func TestLegacyPasswordFailuresKeepBasicIndependent(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old); _ = passportservice.GetMockData().LoadConf("[]") })
	config.SetGlobalConfig(config.Config{})
	password := "LegacySyntheticPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]map[string]interface{}{{"id": 9101, "user_name": "legacy-review", "email": "legacy@example.com", "password": string(hash)}})
	if err := passportservice.GetMockData().LoadConf(string(data)); err != nil {
		t.Fatal(err)
	}
	pub, _, err := passportservice.GetLoginRsa(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pub)
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rsa.EncryptPKCS1v15(rand.Reader, parsed.(*rsa.PublicKey), []byte("incorrect password"))
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/passport/login", Login)
	engine.GET("/dav", middleware.ValidateBasicAuth(), func(c *gin.Context) { c.Status(204) })
	for i := 0; i < 11; i++ {
		name := "legacy-review"
		if i%2 == 1 {
			name = "legacy@example.com"
		}
		body, _ := json.Marshal(map[string]string{"user_name": name, "crypt_passwd": base64.StdEncoding.EncodeToString(encoded)})
		request := httptest.NewRequest("POST", "http://example.com/passport/login", strings.NewReader(string(body)))
		request.RemoteAddr = "198.51.100.92:1234"
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		var result struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		want := int(apperrors.ErrorCodeUserNameOrPasswdWrong)
		if i == 10 {
			want = int(apperrors.ErrorCodeRateLimited)
		}
		if recorder.Code != 200 || result.Code != want {
			t.Fatalf("legacy request %d: status=%d code=%d, want HTTP 200 code=%d", i, recorder.Code, result.Code, want)
		}
	}
	request := httptest.NewRequest("GET", "http://example.com/dav", nil)
	request.RemoteAddr = "198.51.100.93:1234"
	request.SetBasicAuth("legacy@example.com", password)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != 204 {
		t.Fatalf("legacy anonymous login failures blocked independent Basic authentication: HTTP %d", recorder.Code)
	}
}
