package applications

import (
	"net/http"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	httpsOnly := middleware.RequireHTTPS()
	userOnly := middleware.RequireIdentity("", true)
	enabled := middleware.RequireModule("auth")
	data.AddRoute(http.MethodGet, "/api/applications", httpsOnly, userOnly, listApplicationCredentials)
	data.AddRoute(http.MethodPost, "/api/applications", httpsOnly, userOnly, enabled, createApplicationCredential)
	data.AddRoute(http.MethodGet, "/api/applications/:id", httpsOnly, userOnly, getApplicationCredential)
	data.AddRoute(http.MethodPatch, "/api/applications/:id", httpsOnly, userOnly, enabled, updateApplicationCredential)
	data.AddRoute(http.MethodPost, "/api/applications/:id/rotate", httpsOnly, userOnly, enabled, rotateApplicationSecret)
	data.AddRoute(http.MethodDelete, "/api/applications/:id", httpsOnly, userOnly, revokeApplicationCredential)
	return nil
}
