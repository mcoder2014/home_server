package accounts

import (
	"strconv"

	"github.com/gin-gonic/gin"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func sessionPagination(c *gin.Context) (accountservice.SessionFilter, bool) {
	filter := accountservice.SessionFilter{Cursor: c.Query("cursor"), Limit: 20}
	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			respond(c, nil, apperrors.ErrInvalid)
			return filter, false
		}
		filter.Limit = limit
	}
	return filter, true
}

// listSessions reads effective personal logins; the authenticated token, rather
// than a client-supplied ID, determines the separately displayed current login.
func listSessions(c *gin.Context) {
	filter, ok := sessionPagination(c)
	if !ok {
		return
	}
	page, err := accountservice.ListSessions(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken), filter)
	respond(c, page, err)
}

// revokeSessions rejects the whole selection if any target is not visible.
// BrowserWrite and RequireAccount run before this handler and the transaction
// rechecks the same acting token before changing owned session rows.
func revokeSessions(c *gin.Context) {
	var input struct {
		SessionIDs []string `json:"session_ids"`
	}
	if !bind(c, &input, 16<<10) {
		return
	}
	result, err := accountservice.RevokeSessions(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken), input.SessionIDs)
	respond(c, result, err)
}

// revokeOtherSessions includes effective sessions beyond the loaded list page,
// preserving the acting login, account auth version and application credentials.
func revokeOtherSessions(c *gin.Context) {
	result, err := accountservice.RevokeOtherSessions(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken))
	respond(c, result, err)
}

// adminUserSessions is read-only. Current administrator role and acting session
// are revalidated inside the same snapshot as the target's list and counts.
func adminUserSessions(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	filter, ok := sessionPagination(c)
	if !ok {
		return
	}
	page, err := accountservice.AdminUserSessions(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken), id, filter)
	respond(c, page, err)
}
