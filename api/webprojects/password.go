package webprojects

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	application "github.com/mcoder2014/home_server/app/webprojects"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	"github.com/mcoder2014/home_server/domain/service/webcomments"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func getProjectPassword(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	state, err := application.Default.PasswordState(ginfmt.RPCContext(c), currentUserID(c), projectID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, state)
}

func putProjectPassword(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input resourcepasswords.UpdateInput
	if !decodePasswordJSON(c, &input) {
		return
	}
	state, err := application.Default.SetPassword(ginfmt.RPCContext(c), currentUserID(c), projectID, input, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, state)
}

func unlockProject(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if !middleware.IsHTTPS(c) || !middleware.BrowserOriginAllowed(c.Request) {
		ginfmt.Fail(c, apperrors.ErrForbidden)
		return
	}
	var input resourcepasswords.UnlockInput
	if !decodePasswordJSON(c, &input) {
		return
	}
	principal, err := optionalProjectReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	project, err := webcomments.Authorize(db.MasterDB().WithContext(c.Request.Context()), projectID, principal, false, false)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	state, grant, err := resourcepasswords.Default.Unlock(ginfmt.RPCContext(c), resourcepasswords.ResourceWebProject, project.ID, input.Password, middleware.TrustedClientIP(c.Request))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if state.PasswordProtected {
		http.SetCookie(c.Writer, resourcepasswords.GrantCookie(resourcepasswords.ResourceWebProject, project.ID, grant))
	} else {
		http.SetCookie(c.Writer, resourcepasswords.ExpiredGrantCookie(resourcepasswords.ResourceWebProject, project.ID))
	}
	ginfmt.Success(c, http.StatusOK, state)
}

func decodePasswordJSON(c *gin.Context, value interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginfmt.Fail(c, service.ErrInvalid)
		return false
	}
	return true
}

func optionalProjectReader(c *gin.Context) (*utils.Principal, error) {
	explicit := len(c.Request.Header.Values("Authorization")) > 0 || len(c.Request.Header.Values(middleware.HeaderKey)) > 0
	hasSession := utils.BrowserSessionToken(c.Request) != ""
	if !explicit && !hasSession {
		return nil, nil
	}
	principal, err := middleware.ResolveIdentity(c, "web-projects:read", true, false)
	if err == nil {
		return principal, nil
	}
	if errors.Is(err, service.ErrDependency) || explicit {
		return nil, err
	}
	return nil, nil
}
