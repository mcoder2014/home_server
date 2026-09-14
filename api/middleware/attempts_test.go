package middleware

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"golang.org/x/crypto/bcrypt"
)

// TestBasicPasswordFailureBudgetAndTrustedSource 验证成功的 Basic 请求不耗尽失败额度，并检查来源 IP 只从可信代理链解析，不能用伪造转发头绕过限制。
func TestBasicPasswordFailureBudgetAndTrustedSource(t *testing.T) {
	old := config.Global()
	oldProxies := trustedAuthProxies
	t.Cleanup(func() {
		config.SetGlobalConfig(old)
		trustedAuthProxies = oldProxies
		_ = passport.GetMockData().LoadConf("[]")
	})
	config.SetGlobalConfig(config.Config{})
	if err := ConfigureAuthentication(config.AuthConfig{TrustedProxyCIDRs: []string{"127.0.0.1/32", "10.0.0.0/8"}}); err != nil {
		t.Fatal(err)
	}
	password := "BasicSyntheticPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]map[string]interface{}{{"id": 8101, "user_name": "basic-review", "email": "basic-review@example.com", "password": string(hash)}})
	if err := passport.GetMockData().LoadConf(string(data)); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/dav", ValidateBasicAuth(), func(c *gin.Context) { c.Status(204) })
	request := func(remote, forwarded, user, pass string) int {
		r := httptest.NewRequest("GET", "/dav", nil)
		r.RemoteAddr = remote
		r.SetBasicAuth(user, pass)
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w.Code
	}
	t.Run("successful Basic traffic does not exhaust budget", func(t *testing.T) {
		for i := 0; i < 80; i++ {
			if status := request("198.51.100.81:1234", "", "basic-review", password); status != 204 {
				t.Fatalf("successful request %d was limited: HTTP %d", i, status)
			}
		}
	})
	t.Run("unknown accounts cannot spoof direct-client IP", func(t *testing.T) {
		for i := 0; i < 30; i++ {
			if status := request("198.51.100.82:1234", fmt.Sprintf("192.0.2.%d", i+1), fmt.Sprintf("missing-%d", i), "wrong"); status != 401 {
				t.Fatalf("unexpected failure response %d: %d", i, status)
			}
		}
		if status := request("198.51.100.82:1234", "192.0.2.200", "still-missing", "wrong"); status != 429 {
			t.Fatalf("spoofed XFF bypassed source failure budget: HTTP %d", status)
		}
	})
	t.Run("trusted chain uses nearest untrusted hop", func(t *testing.T) {
		for i := 0; i < 30; i++ {
			forwarded := fmt.Sprintf("192.0.2.%d, 198.51.100.83, 10.1.2.3", i+1)
			if status := request("127.0.0.1:1234", forwarded, fmt.Sprintf("proxy-missing-%d", i), "wrong"); status != 401 {
				t.Fatalf("unexpected proxy failure response %d: %d", i, status)
			}
		}
		if status := request("127.0.0.1:1234", "192.0.2.200, 198.51.100.83, 10.1.2.3", "proxy-still-missing", "wrong"); status != 429 {
			t.Fatalf("untrusted left XFF bypassed trusted chain budget: HTTP %d", status)
		}
		if status := request("127.0.0.1:1234", "198.51.100.84", "basic-review", password); status != 204 {
			t.Fatalf("another client behind trusted proxy shared wrong budget: HTTP %d", status)
		}
	})
}

// TestDatabaseBasicRequiresHTTPS 验证数据库账号模式的 Basic 拒绝明文 HTTP，直接客户端伪造 HTTPS 转发头也不能越过传输边界。
func TestDatabaseBasicRequiresHTTPS(t *testing.T) {
	old := config.Global()
	oldProxies := trustedAuthProxies
	t.Cleanup(func() { config.SetGlobalConfig(old); trustedAuthProxies = oldProxies })
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	if err := ConfigureAuthentication(config.AuthConfig{}); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.GET("/dav", ValidateBasicAuth(), func(c *gin.Context) { c.Status(204) })
	for _, forwarded := range []string{"", "https"} {
		r := httptest.NewRequest("GET", "http://example.com/dav", nil)
		r.RemoteAddr = "198.51.100.90:1234"
		r.SetBasicAuth("review", "wrong")
		r.Header.Set("X-Forwarded-Proto", forwarded)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("database Basic over HTTP (untrusted XFP %q) returned %d, want 403", forwarded, w.Code)
		}
	}
}
