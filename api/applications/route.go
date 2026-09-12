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
	data.AddRoute(http.MethodGet, "/api/applications", httpsOnly, userOnly, listApplicationCredentials)
	data.AddRoute(http.MethodPost, "/api/applications", httpsOnly, userOnly, createApplicationCredential)
	data.AddRoute(http.MethodGet, "/api/applications/:id", httpsOnly, userOnly, getApplicationCredential)
	data.AddRoute(http.MethodPatch, "/api/applications/:id", httpsOnly, userOnly, updateApplicationCredential)
	data.AddRoute(http.MethodPost, "/api/applications/:id/rotate", httpsOnly, userOnly, rotateApplicationSecret)
	data.AddRoute(http.MethodDelete, "/api/applications/:id", httpsOnly, userOnly, revokeApplicationCredential)
	return nil
}
