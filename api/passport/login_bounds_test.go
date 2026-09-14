package passport

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
)

type loginCountingReader struct {
	reader io.Reader
	read   int
}

func (r *loginCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

// TestLegacyLoginBoundsBodyBeforeJSON 验证旧登录在 JSON 解码时限制读取量，避免匿名超大字段被完整载入内存。
func TestLegacyLoginBoundsBodyBeforeJSON(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old) })
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/passport/login", Login)
	body := `{"user_name":"` + strings.Repeat("a", 1<<20) + `","crypt_passwd":"!"}`
	reader := &loginCountingReader{reader: strings.NewReader(body)}
	request := httptest.NewRequest("POST", "https://home.example.com/passport/login", reader)
	request.TLS = &tls.ConnectionState{}
	request.RemoteAddr = "198.51.100.202:45002"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	t.Logf("anonymous_input_bytes=%d handler_consumed_bytes=%d HTTP_status=%d", len(body), reader.read, response.Code)
	if reader.read > 16<<10+1 {
		t.Fatal("legacy anonymous login fully decodes oversized JSON before credential validation")
	}
}

// TestLegacyLoginBoundsCryptoFailuresBySource 验证相同来源的无效 RSA 请求在昂贵解密前受到准入限流。
func TestLegacyLoginBoundsCryptoFailuresBySource(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old) })
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/passport/login", Login)
	cipher := base64.StdEncoding.EncodeToString(make([]byte, 128))
	body, _ := json.Marshal(map[string]string{"user_name": "missing_synthetic_account", "crypt_passwd": cipher})
	rateLimited := 0
	for i := 0; i < 35; i++ {
		reader := &loginCountingReader{reader: strings.NewReader(string(body))}
		request := httptest.NewRequest("POST", "https://home.example.com/passport/login", reader)
		request.TLS = &tls.ConnectionState{}
		request.RemoteAddr = "198.51.100.203:45003"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		var value struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if response.Code == 429 || value.Code == int(apperrors.ErrorCodeRateLimited) {
			rateLimited++
			if reader.read != 0 {
				t.Fatal("rate-limited login consumed request body before admission")
			}
		}
	}
	t.Logf("same_source_anonymous_RSA_failures=35 rate_limited=%d", rateLimited)
	if rateLimited != 5 {
		t.Fatalf("35 attempts must reject exactly five after the 30-request source allowance: %d", rateLimited)
	}
}
