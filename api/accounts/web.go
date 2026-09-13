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

func getWebProject(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	actor := currentUser(c)
	detail, err := adminweb.Get(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id)
	respond(c, detail, err)
}

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
