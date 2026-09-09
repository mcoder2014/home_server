package webprojects

import (
	"net/http"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	if !config.Global().WebProjects.Enabled {
		return nil
	}
	management := requireManagementLogin()
	data.AddRoute(http.MethodGet, "/api/web-projects", management, listProjects)
	data.AddRoute(http.MethodPost, "/api/web-projects", management, createProject)
	data.AddRoute(http.MethodGet, "/api/web-projects/eligible-users", management, eligibleUsers)
	data.AddRoute(http.MethodPost, "/api/web-projects/browser-login", management, browserLogin)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id", management, getProject)
	data.AddRoute(http.MethodPatch, "/api/web-projects/:id", management, updateProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/disable", management, disableProject)
	data.AddRoute(http.MethodDelete, "/api/web-projects/:id", management, deleteProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/restore", management, restoreProject)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/releases", management, uploadRelease)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id/releases", management, listReleases)
	data.AddRoute(http.MethodPost, "/api/web-projects/:id/publish", management, publishRelease)
	data.AddRoute(http.MethodGet, "/api/web-projects/:id/releases/:release_id/download", management, downloadRelease)
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/p/:slug", "/p/:slug/*path"}, serveProjectContent)
	return nil
}
