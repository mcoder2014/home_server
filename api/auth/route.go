package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/applications"
	"github.com/mcoder2014/home_server/api/middleware"
	authapp "github.com/mcoder2014/home_server/app/auth"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func InitRouter() error {
	conf := config.Global()
	// Browser login converts a user credential into an HttpOnly cookie. Registering
	// it requires at least one configured origin so browserLogin can reject cross-site writes.
	if conf.Auth.SiteOrigin != "" || len(conf.Auth.SiteOrigins) > 0 {
		handlers := []gin.HandlerFunc{middleware.RequireHTTPS(), middleware.RequireIdentity("", true), browserLogin}
		data.AddRoute(http.MethodPost, "/api/auth/browser-login", handlers...)
		// Web-share aliases use the same handler and shared session cookie.
		if conf.WebProjects.Enabled {
			data.AddRoute(http.MethodPost, "/api/web-share/browser-login", handlers...)
			data.AddRoute(http.MethodPost, "/api/web-projects/browser-login", handlers...)
		}
	}
	if conf.Auth.ApplicationsEnabled {
		data.AddRoute(http.MethodPost, "/api/auth/token", middleware.RequireHTTPS(), applications.IssueApplicationAccessToken)
	}
	return nil
}

func browserLogin(c *gin.Context) {
	// Strict equality is the CSRF boundary for the cookie-setting endpoint. Empty,
	// missing, and foreign origins must not create an authenticated browser session.
	if !browserOriginAllowed(c.Request, config.Global().Auth) {
		ginfmt.Fail(c, apperrors.ErrForbidden)
		return
	}
	token := c.GetHeader(middleware.HeaderKey)
	session, err := authapp.GetBrowserSession(ginfmt.RPCContext(c), token)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	utils.SetBrowserSession(c, token, session.ExpireTime)
	ginfmt.Success(c, http.StatusOK, session)
}

func browserOriginAllowed(request *http.Request, conf config.AuthConfig) bool {
	origins := request.Header.Values("Origin")
	if len(origins) != 1 || origins[0] == "" {
		return false
	}
	if origins[0] == conf.SiteOrigin {
		return true
	}
	for _, configuredOrigin := range conf.SiteOrigins {
		if origins[0] == configuredOrigin {
			return true
		}
	}
	return false
}
