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
	for _, prefix := range []string{"/api/web-share", "/api/web-projects"} {
		data.AddRoute(http.MethodGet, prefix, read, listProjects)
		data.AddRoute(http.MethodPost, prefix, write, createProject)
		data.AddRoute(http.MethodGet, prefix+"/eligible-users", read, eligibleUsers)
		data.AddRoute(http.MethodGet, prefix+"/:id", read, getProject)
		data.AddRoute(http.MethodPatch, prefix+"/:id", write, updateProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/disable", write, disableProject)
		data.AddRoute(http.MethodDelete, prefix+"/:id", write, deleteProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/restore", write, restoreProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/releases", write, uploadRelease)
		data.AddRoute(http.MethodGet, prefix+"/:id/releases", read, listReleases)
		data.AddRoute(http.MethodPost, prefix+"/:id/publish", write, publishRelease)
		data.AddRoute(http.MethodGet, prefix+"/:id/releases/:release_id/download", read, downloadRelease)
	}
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/p/:slug", "/p/:slug/*path"}, serveProjectContent)
	return nil
}
