package accounts

import (
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/app/adminweb"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// listWebProjects 处理 GET /api/admin/web-share：管理员按所有者、发布状态、可见性及审核状态分页查看全站网页。
func listWebProjects(c *gin.Context) {
	cursor, limit, ok := pagination(c)
	if !ok {
		return
	}
	filter := adminweb.Filter{Cursor: cursor, Limit: limit, Query: c.Query("query"), Status: c.Query("status"), AccessMode: c.Query("access_mode"), ModerationStatus: c.Query("moderation_status")}
	if raw := c.Query("owner_user_id"); raw != "" {
		var err error
		filter.OwnerUserID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.OwnerUserID <= 0 {
			respond(c, nil, apperrors.ErrInvalid)
			return
		}
	}
	actor := currentUser(c)
	page, err := adminweb.List(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, filter)
	respond(c, page, err)
}

// getWebProject 处理 GET /api/admin/web-share/:id：读取管理侧网页详情和版本信息，不受普通读者可见范围限制。
func getWebProject(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	actor := currentUser(c)
	detail, err := adminweb.Get(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id)
	respond(c, detail, err)
}

// getWebReleases 处理 GET /api/admin/web-share/:id/releases：为管理员列出目标网页的版本元信息，实际内容仍通过独立预览入口读取。
func getWebReleases(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	actor := currentUser(c)
	detail, err := adminweb.Get(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id)
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, map[string]interface{}{"items": detail.Releases}, nil)
}

// changeWebProject 承接 POST /api/admin/web-share/:id/{block,delete,restore,unblock}，携带版本、原因及管理员凭据执行审核状态变更。
func changeWebProject(c *gin.Context, action string) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	var input adminweb.MutationRequest
	if !bind(c, &input, 16<<10) {
		return
	}
	actor := currentUser(c)
	result, err := adminweb.Change(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id, rev, action, input)
	respond(c, result, err)
}

// previewWebProject 处理 GET/HEAD /api/admin/web-share/:id/releases/:release_id/preview/*path。
// 复核管理员并检查安全路径后输出版本文件，文档预览须先落审计；禁止缓存和 Service Worker，不提供脚本隔离。
func previewWebProject(c *gin.Context) {
	if c.GetHeader("Service-Worker") != "" {
		respond(c, nil, apperrors.ErrForbidden)
		return
	}
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	releaseID, ok := positiveID(c, "release_id")
	if !ok {
		return
	}
	actor := currentUser(c)
	file, err := adminweb.OpenPreview(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id, releaseID, strings.TrimPrefix(c.Param("path"), "/"))
	if err != nil {
		respond(c, nil, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		respond(c, nil, apperrors.ErrDependency)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(file.Name())))
	if contentType != "" {
		c.Header("Content-Type", contentType)
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}
