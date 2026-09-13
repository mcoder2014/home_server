package accounts

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/dal"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func listUsers(c *gin.Context) {
	cursor, limit, ok := pagination(c)
	if !ok {
		return
	}
	filter := dal.AccountFilter{Cursor: cursor, Limit: limit, Query: c.Query("query"), Status: c.Query("status"), Role: c.Query("role"), WebDAVPermission: c.Query("webdav_permission")}
	if raw := c.Query("library_enabled"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			respond(c, nil, apperrors.ErrInvalid)
			return
		}
		filter.LibraryEnabled = &v
	}
	page, err := accountservice.ListUsers(ginfmt.RPCContext(c), filter)
	respond(c, page, err)
}

func getUser(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	detail, err := accountservice.UserDetail(ginfmt.RPCContext(c), id)
	respond(c, detail, err)
}

func createUser(c *gin.Context) {
	var input accountservice.CreateInput
	if !bind(c, &input, 16<<10) {
		return
	}
	actor := currentUser(c)
	result, err := accountservice.AdminCreate(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, input)
	respond(c, result, err)
}

func changeUser(c *gin.Context, action string) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	var input accountservice.AdminInput
	if !bind(c, &input, 16<<10) {
		return
	}
	actor := currentUser(c)
	result, err := accountservice.AdminChange(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id, rev, action, input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	if action == "reset-password" {
		respond(c, result, nil)
	} else {
		respond(c, result.User, nil)
	}
}

func adminRevokeInvitation(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	inviteID, ok := positiveID(c, "invitation_id")
	if !ok {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	var input accountservice.AdminInput
	if !bind(c, &input, 16<<10) {
		return
	}
	actor := currentUser(c)
	err := accountservice.AdminRevokeInvitation(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, id, rev, inviteID, input)
	respond(c, nil, err)
}

func auditLogs(c *gin.Context) {
	cursor, limit, ok := pagination(c)
	if !ok {
		return
	}
	targetID := int64(0)
	if raw := c.Query("target_id"); raw != "" {
		var err error
		targetID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || targetID <= 0 {
			respond(c, nil, apperrors.ErrInvalid)
			return
		}
	}
	page, err := accountservice.ListAudit(ginfmt.RPCContext(c), cursor, limit, c.Query("target_type"), targetID)
	respond(c, page, err)
}
