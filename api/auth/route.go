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
	if conf.Auth.SiteOrigin != "" {
		handlers := []gin.HandlerFunc{middleware.RequireHTTPS(), middleware.RequireIdentity("", true), browserLogin}
		data.AddRoute(http.MethodPost, "/api/auth/browser-login", handlers...)
		// Alias the original PR endpoint during rollout; both write one shared cookie.
		if conf.WebProjects.Enabled {
			data.AddRoute(http.MethodPost, "/api/web-projects/browser-login", handlers...)
		}
	}
	if conf.Auth.ApplicationsEnabled {
		data.AddRoute(http.MethodPost, "/api/auth/token", middleware.RequireHTTPS(), applications.Token)
	}
	return nil
}

func browserLogin(c *gin.Context) {
	if c.GetHeader("Origin") != config.Global().Auth.SiteOrigin {
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
