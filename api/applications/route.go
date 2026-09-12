package applications

import (
	"net/http"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	if !config.Global().Auth.ApplicationsEnabled {
		return nil
	}
	httpsOnly := middleware.RequireHTTPS()
	userOnly := middleware.RequireIdentity("", true)
	data.AddRoute(http.MethodGet, "/api/applications", httpsOnly, userOnly, list)
	data.AddRoute(http.MethodPost, "/api/applications", httpsOnly, userOnly, create)
	data.AddRoute(http.MethodGet, "/api/applications/:id", httpsOnly, userOnly, get)
	data.AddRoute(http.MethodPatch, "/api/applications/:id", httpsOnly, userOnly, update)
	data.AddRoute(http.MethodPost, "/api/applications/:id/rotate", httpsOnly, userOnly, rotate)
	data.AddRoute(http.MethodDelete, "/api/applications/:id", httpsOnly, userOnly, revoke)
	return nil
}
