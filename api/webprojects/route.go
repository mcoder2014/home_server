package webprojects

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func InitRouter() error {
	read := middleware.RequireIdentity("web-projects:read", false)
	write := middleware.RequireIdentity("web-projects:write", false)
	for _, prefix := range []string{"/api/web-share", "/api/web-projects"} {
		data.AddRoute(http.MethodGet, prefix, requireModule, read, listProjects)
		data.AddRoute(http.MethodPost, prefix, requireModule, write, createProject)
		data.AddRoute(http.MethodGet, prefix+"/eligible-users", requireModule, read, eligibleUsers)
		data.AddRoute(http.MethodGet, prefix+"/:id", requireModule, read, getProject)
		data.AddRoute(http.MethodPatch, prefix+"/:id", requireModule, write, updateProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/disable", requireModule, write, disableProject)
		data.AddRoute(http.MethodDelete, prefix+"/:id", requireModule, write, deleteProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/restore", requireModule, write, restoreProject)
		data.AddRoute(http.MethodPost, prefix+"/:id/releases", requireModule, write, uploadRelease)
		data.AddRoute(http.MethodGet, prefix+"/:id/releases", requireModule, read, listReleases)
		data.AddRoute(http.MethodPost, prefix+"/:id/publish", requireModule, write, publishRelease)
		data.AddRoute(http.MethodGet, prefix+"/:id/releases/:release_id/download", requireModule, read, downloadRelease)
	}
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/p/:slug", "/p/:slug/*path"}, requireModule, serveProjectContent)
	return nil
}

// requireModule 逐请求检查网页托管模块；内容路径关闭时返回不可见结果，管理请求使用功能关闭错误。
func requireModule(c *gin.Context) {
	enabled, err := accounts.ModuleEnabled(c.Request.Context(), "web_projects")
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		c.Abort()
		return
	}
	if !enabled {
		ginfmt.Fail(c, service.ErrForbidden)
		c.Abort()
		return
	}
	c.Next()
}
