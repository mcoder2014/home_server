package accounts

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/dal"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// listUsers 处理 GET /api/admin/users：按账号状态、角色和功能授权筛选用户，使用游标分页返回管理列表与资源统计。
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

// getUser 处理 GET /api/admin/users/:id：返回指定账号的资料、权限及资源明细，供管理员查看和发起后续操作。
func getUser(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	detail, err := accountservice.UserDetail(ginfmt.RPCContext(c), id)
	respond(c, detail, err)
}

// createUser 处理 POST /api/admin/users：在已验证的管理员身份下创建账号，并返回仅此次展示的初始密码及到期时间。
func createUser(c *gin.Context) {
	var input accountservice.CreateInput
	if !bind(c, &input, 16<<10) {
		return
	}
	actor := currentUser(c)
	result, err := accountservice.AdminCreate(ginfmt.RPCContext(c), actor.ID, actor.AuthVersion, input)
	respond(c, result, err)
}

// changeUser 承接 /api/admin/users/:id 的账号状态、角色、功能授权和密码管理动作。
// 解析目标与 If-Match 版本后交给服务层验密和原子变更；仅重置密码响应包含新初始密码。
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
	if action == "reset-profile" {
		input.ActingToken = c.GetString(utils.CtxKeyLoginToken)
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

// adminRevokeInvitation 处理 POST /api/admin/users/:id/invitations/:invitation_id/revoke：以邀请人版本和管理员操作凭据撤销指定邀请码。
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

// auditLogs 处理 GET /api/admin/audit-logs：按目标类型、目标 ID 和游标分页读取管理操作审计，不返回密码或凭据正文。
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
