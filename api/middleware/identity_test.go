package middleware

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestHTTPSDoesNotTrustForwardedHeadersFromArbitraryClients(t *testing.T) {
	require.NoError(t, ConfigureAuthentication(config.AuthConfig{}))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.RemoteAddr = "198.51.100.9:1234"
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	require.False(t, IsHTTPS(c))
	c.Request.RemoteAddr = "127.0.0.1:1234"
	require.True(t, IsHTTPS(c))
	c.Request.Header.Set("X-Forwarded-Proto", "https,http")
	require.False(t, IsHTTPS(c))
	c.Request.TLS = &tls.ConnectionState{}
	require.True(t, IsHTTPS(c))
}

func TestConfigureAuthenticationAcceptsHTTPSOriginConfigurations(t *testing.T) {
	tests := []struct {
		name string
		conf config.AuthConfig
	}{
		{name: "disabled"},
		{name: "legacy single", conf: config.AuthConfig{SiteOrigin: "https://home.example.com"}},
		{name: "additional origins", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com", "https://home.internal.example.com:1", "https://home.internal.example.com:65535"}}},
		{name: "single and list", conf: config.AuthConfig{SiteOrigin: "https://home.example.com", SiteOrigins: []string{"https://home.internal.example.com"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, ConfigureAuthentication(tt.conf))
		})
	}
}

// TestConfigureAuthenticationRejectsInvalidOrigins 验证认证配置拒绝带路径、通配或非法端口的来源定义，同时保留合法 HTTPS Origin。
func TestConfigureAuthenticationRejectsInvalidOrigins(t *testing.T) {
	tests := []struct {
		name string
		conf config.AuthConfig
	}{
		{name: "http single", conf: config.AuthConfig{SiteOrigin: "http://home.example.com"}},
		{name: "single path", conf: config.AuthConfig{SiteOrigin: "https://home.example.com/login"}},
		{name: "empty list item", conf: config.AuthConfig{SiteOrigins: []string{""}}},
		{name: "http list item", conf: config.AuthConfig{SiteOrigins: []string{"http://home.internal.example.com"}}},
		{name: "invalid later list item", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com", "http://home.internal.example.com"}}},
		{name: "wildcard", conf: config.AuthConfig{SiteOrigins: []string{"https://*.example.com"}}},
		{name: "username", conf: config.AuthConfig{SiteOrigins: []string{"https://user@home.example.com"}}},
		{name: "password", conf: config.AuthConfig{SiteOrigins: []string{"https://user:password@home.example.com"}}},
		{name: "missing host", conf: config.AuthConfig{SiteOrigins: []string{"https://:443"}}},
		{name: "empty port", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com:"}}},
		{name: "zero port", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com:0"}}},
		{name: "port above maximum", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com:65536"}}},
		{name: "query", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com?target=login"}}},
		{name: "empty query", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com?"}}},
		{name: "fragment", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com#login"}}},
		{name: "trailing slash", conf: config.AuthConfig{SiteOrigins: []string{"https://home.example.com/"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, ConfigureAuthentication(tt.conf))
		})
	}
}

func TestExplicitCredentialsNeverFallBackToUserSession(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set(HeaderKey, "synthetic-user-token")
	c.Request.Header.Set("Authorization", "Bearer invalid-token")
	_, err := ResolveIdentity(c, "web-projects:read", true, false)
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	c.Request.Header.Del(HeaderKey)
	_, err = ResolveIdentity(c, "", true, true)
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	c.Request.Header.Set("Authorization", "Basic invalid")
	_, err = ResolveIdentity(c, "web-projects:read", true, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)
}

func TestWebDAVRejectsMixedAuthenticationSourcesBeforeParsingBasic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/webdav/test", ValidateBasicAuth(), func(c *gin.Context) { c.Status(204) })
	request := httptest.NewRequest("GET", "/webdav/test", nil)
	request.Header.Add("Authorization", "Basic invalid")
	request.Header.Add("Authorization", "Bearer invalid")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, 400, recorder.Code)

	request = httptest.NewRequest("GET", "/webdav/test", nil)
	request.Header.Set("Authorization", "Basic invalid")
	request.Header.Set(HeaderKey, "user-token")
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, 400, recorder.Code)
}
