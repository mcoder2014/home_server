package manuals

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	application "github.com/mcoder2014/home_server/app/manuals"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func getManualPassword(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	principal := currentPrincipal(c)
	state, err := application.Default.PasswordState(ginfmt.RPCContext(c), principal.UserID, manualID)
	respond(c, http.StatusOK, state, err)
}

func putManualPassword(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input resourcepasswords.UpdateInput
	if !bindJSON(c, &input, 4096) {
		return
	}
	principal := currentPrincipal(c)
	state, err := application.Default.SetPassword(ginfmt.RPCContext(c), principal.UserID, manualID, input, principal)
	respond(c, http.StatusOK, state, err)
}

func unlockManual(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if !middleware.IsHTTPS(c) || !middleware.BrowserOriginAllowed(c.Request) {
		ginfmt.Fail(c, apperrors.ErrForbidden)
		return
	}
	var input resourcepasswords.UnlockInput
	if !bindJSON(c, &input, 4096) {
		return
	}
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	manual, err := application.Default.ReadableManual(ginfmt.RPCContext(c), manualID, principalUserID(principal))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	state, grant, err := resourcepasswords.Default.Unlock(ginfmt.RPCContext(c), resourcepasswords.ResourceManual, manual.ID, input.Password, middleware.TrustedClientIP(c.Request))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if state.PasswordProtected {
		http.SetCookie(c.Writer, resourcepasswords.GrantCookie(resourcepasswords.ResourceManual, manual.ID, grant))
	} else {
		http.SetCookie(c.Writer, resourcepasswords.ExpiredGrantCookie(resourcepasswords.ResourceManual, manual.ID))
	}
	ginfmt.Success(c, http.StatusOK, state)
}
