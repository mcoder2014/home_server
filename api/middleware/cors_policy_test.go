package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
)

func TestCORSPermitsConfiguredBrowserWritesAndRejectsOtherOrigins(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old) })
	conf := config.Config{}
	conf.Auth.SiteOrigin = "https://home.example.com"
	config.SetGlobalConfig(conf)
	router := gin.New()
	router.Use(CORS())
	router.DELETE("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, origin := range []string{"https://home.example.com", "https://untrusted.example.com"} {
		request := httptest.NewRequest(http.MethodOptions, "/resource", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", "DELETE")
		request.Header.Set("Access-Control-Request-Headers", "X-CSRF-Token,If-Match")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if origin == conf.Auth.SiteOrigin {
			if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Errorf("configured DELETE preflight was not allowed: status=%d origin=%q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
			}
		} else if response.Code != http.StatusForbidden {
			t.Errorf("untrusted origin received status %d", response.Code)
		}
	}
}
