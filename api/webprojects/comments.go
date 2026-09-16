package webprojects

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/webcomments"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// commentContext is the only anonymous comment-related API. Public viewers get
// login/navigation state, never discussion data or mutation credentials.
func commentContext(c *gin.Context) {
	id, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	p, identityErr := middleware.ResolveIdentity(c, "web-comments:read", true, false)
	if identityErr != nil {
		p = nil
	}
	project, err := webcomments.Authorize(db.MasterDB().WithContext(c.Request.Context()), id, p, false, false)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	explicitCredentials := c.GetHeader("Authorization") != "" || c.GetHeader(middleware.HeaderKey) != ""
	if identityErr != nil && explicitCredentials {
		ginfmt.Fail(c, identityErr)
		return
	}
	if identityErr != nil && errors.Is(identityErr, service.ErrDependency) {
		ginfmt.Fail(c, identityErr)
		return
	}
	if identityErr != nil {
		utils.ClearBrowserSession(c)
	}
	release, err := dal.QueryWebProjectRelease(id, *project.CurrentReleaseID)
	if err != nil || release == nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	result := gin.H{"user_id": "", "display_name": "未登录", "can_comment": false, "csrf_token": "", "release_id": strconv.FormatInt(release.ID, 10), "entry_file": release.EntryFile, "container_mode": project.ContainerMode, "owner_user_id": strconv.FormatInt(project.OwnerUserID, 10), "project_name": project.Name}
	if p != nil {
		result["user_id"] = strconv.FormatInt(p.UserID, 10)
		result["display_name"] = "用户 " + strconv.FormatInt(p.UserID, 10)
		if accounts.DatabaseMode() {
			if u, e := accounts.GetByID(c.Request.Context(), p.UserID); e == nil && u != nil {
				if u.DisplayName != "" {
					result["display_name"] = u.DisplayName
				} else {
					result["display_name"] = u.Username
				}
			}
		}
		result["can_comment"] = project.ContainerMode == "enhanced" && (p.Kind == "user" || p.Allows("web-comments:write"))
		if token := c.GetString(utils.CtxKeyLoginToken); token != "" {
			result["csrf_token"] = middleware.ScopedCSRFToken(token, "web-comments")
		}
	}
	c.Header("Cache-Control", "no-store")
	ginfmt.Success(c, http.StatusOK, result)
}

// comments handles the bounded reader APIs and explicit thread actions. All
// authorization and mutation rules live in webcomments, shared with AK/SK apps.
func comments(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	threadID := int64(0)
	if c.Param("thread_id") != "" {
		threadID, err = service.ParsePositiveID(c.Param("thread_id"))
		if err != nil {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
	}
	scope := "web-comments:read"
	if c.Request.Method == http.MethodPost {
		scope = "web-comments:write"
	}
	p, identityErr := middleware.ResolveIdentity(c, scope, true, false)
	if identityErr != nil {
		if errors.Is(identityErr, service.ErrForbidden) || errors.Is(identityErr, service.ErrDependency) {
			ginfmt.Fail(c, identityErr)
			return
		}
		// Resolve visibility as anonymous before returning an identity error. This
		// keeps private, missing and disabled projects indistinguishable as 404.
		if _, visibleErr := webcomments.Authorize(db.MasterDB().WithContext(c.Request.Context()), id, nil, false, false); visibleErr != nil {
			ginfmt.Fail(c, visibleErr)
			return
		}
		ginfmt.Fail(c, identityErr)
		return
	}
	if c.Request.Method == http.MethodPost && c.GetHeader("Authorization") == "" && c.GetHeader(middleware.HeaderKey) == "" {
		middleware.BrowserScopedWrite("web-comments")(c)
		if c.IsAborted() {
			return
		}
	}
	if c.Request.Method == http.MethodGet {
		cursorValue := c.Query("cursor")
		if strings.HasSuffix(c.FullPath(), "/events") && c.Query("after_seq") != "" {
			cursorValue = c.Query("after_seq")
		}
		cursor, e := optionalID(cursorValue)
		if e != nil {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
		limit, e := optionalLimit(c.Query("limit"))
		if e != nil {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
		if limit > 100 {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
		var result interface{}
		switch {
		case strings.HasSuffix(c.FullPath(), "/events"):
			result, err = webcomments.Events(c.Request.Context(), id, threadID, p, cursor, limit, c.Query("request_id"))
		case threadID > 0:
			result, err = webcomments.Detail(c.Request.Context(), id, threadID, p)
		default:
			result, err = webcomments.List(c.Request.Context(), id, p, c.Query("status"), cursor, limit, c.Query("request_id"))
		}
		if err != nil {
			ginfmt.Fail(c, err)
			return
		}
		ginfmt.Success(c, http.StatusOK, result)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var input webcomments.Input
	if err = c.ShouldBindJSON(&input); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	action := "comment"
	revision := int64(0)
	if threadID > 0 {
		parts := strings.Split(c.FullPath(), "/")
		action = parts[len(parts)-1]
		if action == "replies" {
			action = "reply"
		}
	}
	if action != "comment" && action != "reply" {
		revision, err = parseIfMatch(c.GetHeader("If-Match"))
		if err != nil {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
	}
	result, err := webcomments.Mutate(c.Request.Context(), id, threadID, revision, p, action, input)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	status := http.StatusOK
	if action == "comment" {
		status = http.StatusCreated
	}
	ginfmt.Success(c, status, result)
}
