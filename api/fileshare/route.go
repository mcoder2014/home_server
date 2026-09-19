package fileshare

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func InitRouter() error {
	read := middleware.RequireIdentity("files:read", false)
	write := middleware.RequireIdentity("files:write", false)
	data.AddRoute(http.MethodGet, "/api/files", requireModule, read, listFiles)
	data.AddRoute(http.MethodPost, "/api/files", requireModule, write, uploadFile)
	data.AddRoute(http.MethodGet, "/api/files/eligible-users", requireModule, read, eligibleUsers)
	data.AddRoute(http.MethodGet, "/api/files/:id", requireModule, read, getFile)
	data.AddRoute(http.MethodDelete, "/api/files/:id", requireModule, write, deleteFile)
	data.AddRoute(http.MethodGet, "/api/files/:id/shares", requireModule, read, listShares)
	data.AddRoute(http.MethodPost, "/api/files/:id/shares", requireModule, write, createShare)
	data.AddRoute(http.MethodDelete, "/api/files/:id/shares/:share_id", requireModule, write, revokeShare)
	data.AddRoute(http.MethodGet, "/api/file-shares/:token", requireModule, getPublicShare)
	data.AddRoute(http.MethodPost, "/api/file-shares/:token/unlock", requireModule, unlockPublicShare)
	data.AddRoute(http.MethodPost, "/api/file-shares/:token/download", requireModule, downloadPublicShare)
	return nil
}

func requireModule(c *gin.Context) {
	middleware.EnableReadSnapshot(c)
	enabled, err := accounts.ModuleEnabled(ginfmt.RPCContext(c), "file_sharing")
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		c.Abort()
		return
	}
	if !enabled {
		if strings.HasPrefix(c.Request.URL.Path, "/api/file-shares/") {
			ginfmt.Fail(c, service.ErrNotFound)
		} else {
			ginfmt.Fail(c, service.ErrForbidden)
		}
		c.Abort()
		return
	}
	c.Next()
}
