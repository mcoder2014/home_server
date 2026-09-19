package manuals

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func InitRouter() error {
	read := middleware.RequireIdentity("manuals:read", false)
	write := middleware.RequireIdentity("manuals:write", false)
	data.AddRoute(http.MethodGet, "/api/manuals", requireModule, listManuals)
	data.AddRoute(http.MethodGet, "/api/manuals/categories", requireModule, listCategories)
	data.AddRoute(http.MethodPost, "/api/manuals", requireModule, write, createManual)
	data.AddRoute(http.MethodGet, "/api/manuals/:id", requireModule, getManual)
	data.AddRoute(http.MethodGet, "/api/manuals/:id/password", requireModule, read, getManualPassword)
	data.AddRoute(http.MethodPut, "/api/manuals/:id/password", requireModule, write, putManualPassword)
	data.AddRoute(http.MethodPost, "/api/manuals/:id/unlock", requireModule, unlockManual)
	data.AddRoute(http.MethodPatch, "/api/manuals/:id", requireModule, write, updateManual)
	data.AddRoute(http.MethodDelete, "/api/manuals/:id", requireModule, write, deleteManual)
	data.AddRoute(http.MethodPost, "/api/manuals/:id/items", requireModule, write, addInlineItem)
	data.AddRoute(http.MethodPost, "/api/manuals/:id/files", requireModule, write, addFileItem)
	data.AddRoute(http.MethodDelete, "/api/manuals/:id/items/:item_id", requireModule, write, deleteItem)
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/api/manuals/:id/items/:item_id/content", "/api/manuals/:id/items/:item_id/thumbnail"}, requireModule, serveItemResource)
	return nil
}

func requireModule(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	middleware.EnableReadSnapshot(c)
	enabled, err := accounts.ModuleEnabled(ginfmt.RPCContext(c), "manuals")
	if err != nil {
		ginfmt.Fail(c, apperrors.ErrDependency)
		c.Abort()
		return
	}
	if !enabled {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			ginfmt.Fail(c, apperrors.ErrNotFound)
		} else {
			ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "功能暂未开放"))
		}
		c.Abort()
		return
	}
	c.Next()
}
