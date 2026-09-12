package webprojects

import (
	"net/http"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	if !config.Global().WebProjects.Enabled {
		return nil
	}
	read := middleware.RequireIdentity("web-projects:read", false)
	write := middleware.RequireIdentity("web-projects:write", false)
	data.AddRoute(http.MethodGet, "/api/web-projects", read, listProjects)
	data.AddRoute(http.MethodPost, "/api/web-projects", write, createProject)
	data.AddRoute(http.MethodGet, "/api/web-projects/eligible-users", read, eligibleUsers)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id", read, getProject)
	data.AddRoute(http.MethodPatch, "/api/web-projects/:id", write, updateProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/disable", write, disableProject)
	data.AddRoute(http.MethodDelete, "/api/web-projects/:id", write, deleteProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/restore", write, restoreProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/releases", write, uploadRelease)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id/releases", read, listReleases)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/publish", write, publishRelease)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id/releases/:release_id/download", read, downloadRelease)
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/p/:slug", "/p/:slug/*path"}, serveProjectContent)
	return nil
}
