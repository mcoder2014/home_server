package utils

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	SessionCookieName       = "__Host-cq_session"
	LegacySessionCookieName = "__Host-web_projects_session"
)

// BrowserSessionToken prefers the canonical session even when it is invalid.
// The legacy name is read only when the canonical cookie is absent: a stale
// legacy cookie must never silently override a newer login or failed session.
func BrowserSessionToken(request *http.Request) string {
	cookie, err := request.Cookie(SessionCookieName)
	if err == nil {
		return cookie.Value
	}
	cookie, err = request.Cookie(LegacySessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetBrowserSession stores only a server-validated opaque login token. User ID
// and permissions remain server-side. Upgrade writes expire obsolete cookies.
func SetBrowserSession(c *gin.Context, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if token == "" || maxAge <= 0 {
		ClearBrowserSession(c)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: SessionCookieName, Value: token, Path: "/", MaxAge: maxAge, Expires: expires, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	for _, name := range []string{LegacySessionCookieName, "home_server"} {
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
}

func ClearBrowserSession(c *gin.Context) {
	for _, name := range []string{SessionCookieName, LegacySessionCookieName, "home_server"} {
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
}
