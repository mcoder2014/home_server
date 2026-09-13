package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBrowserSessionPrefersCanonicalCookie(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: LegacySessionCookieName, Value: "old-user"})
	require.Equal(t, "old-user", BrowserSessionToken(request))
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "new-user"})
	require.Equal(t, "new-user", BrowserSessionToken(request))
	request.Header.Set("Cookie", SessionCookieName+"=; "+LegacySessionCookieName+"=old-user")
	require.Empty(t, BrowserSessionToken(request), "empty canonical session must not reactivate a legacy identity")
}

func TestBrowserSessionWritesOneSecureCookieAndExpiresOldKeys(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	SetBrowserSession(c, "opaque-token", time.Now().Add(time.Hour))
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 3)
	for _, cookie := range cookies {
		require.True(t, cookie.Secure)
		require.True(t, cookie.HttpOnly)
		require.Equal(t, "/", cookie.Path)
		require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		if cookie.Name == SessionCookieName {
			require.Positive(t, cookie.MaxAge)
			require.Equal(t, "opaque-token", cookie.Value)
		} else {
			require.Negative(t, cookie.MaxAge)
			require.Empty(t, cookie.Value)
		}
	}
}
