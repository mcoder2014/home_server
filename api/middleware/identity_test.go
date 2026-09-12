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
