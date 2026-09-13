package accounts

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func bind(c *gin.Context, value interface{}, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return false
	}
	return true
}

func currentUser(c *gin.Context) *model.UserAccount {
	value, exists := c.Get(middleware.AccountContextKey)
	if !exists {
		return nil
	}
	user, _ := value.(*model.UserAccount)
	return user
}

func positiveID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return 0, false
	}
	return id, true
}

func revision(c *gin.Context) (int64, bool) {
	values := c.Request.Header.Values("If-Match")
	if len(values) != 1 {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return 0, false
	}
	id, err := strconv.ParseInt(strings.Trim(values[0], `"`), 10, 64)
	if err != nil || id <= 0 {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return 0, false
	}
	return id, true
}

func pagination(c *gin.Context) (int64, int, bool) {
	cursor := int64(0)
	limit := 20
	var err error
	if raw := c.Query("cursor"); raw != "" {
		cursor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			ginfmt.Fail(c, apperrors.ErrInvalid)
			return 0, 0, false
		}
	}
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			ginfmt.Fail(c, apperrors.ErrInvalid)
			return 0, 0, false
		}
	}
	return cursor, limit, true
}

func respond(c *gin.Context, value interface{}, err error) {
	c.Header("Cache-Control", "no-store")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, value)
}

type accountView struct {
	*model.UserAccount
	CSRFToken         string          `json:"csrf_token"`
	Capabilities      map[string]bool `json:"capabilities"`
	PasswordPolicy    map[string]int  `json:"password_policy"`
	ApplicationPolicy map[string]int  `json:"application_policy"`
}

func view(user *model.UserAccount, token string) accountView {
	runtime := config.Runtime()
	return accountView{UserAccount: user, CSRFToken: middleware.CSRFToken(token), Capabilities: map[string]bool{"library": user.LibraryEnabled && runtime.LibraryEnabled, "webdav": user.WebDAVPermission != model.WebDAVNone && runtime.WebDAVEnabled, "applications": runtime.Auth.ApplicationsEnabled, "web_projects": runtime.WebProjects.Enabled}, PasswordPolicy: map[string]int{"min_length": runtime.AccountPolicy.MinPasswordLength}, ApplicationPolicy: map[string]int{"default_credential_ttl_days": runtime.Auth.DefaultCredentialTTLDays, "max_credential_ttl_days": runtime.Auth.MaxCredentialTTLDays, "max_applications_per_user": runtime.Auth.MaxApplicationsPerUser}}
}

func login(c *gin.Context) {
	var input struct {
		UserName string `json:"user_name"`
		Password string `json:"password"`
	}
	if !bind(c, &input, 16<<10) {
		return
	}
	c.Set(accountservice.PasswordSourceIPKey, middleware.TrustedClientIP(c.Request))
	ctx := ginfmt.RPCContext(c)
	if accountservice.DatabaseMode() {
		user, token, session, err := accountservice.Login(ctx, input.UserName, input.Password)
		if err != nil {
			respond(c, nil, err)
			return
		}
		utils.SetBrowserSession(c, token, session.ExpireTime)
		respond(c, view(user, token), nil)
		return
	}
	identity, err := passport.ValidateUser(ctx, input.UserName, input.Password)
	if err != nil {
		respond(c, nil, err)
		return
	}
	token, err := passport.GenToken(identity)
	if err != nil {
		respond(c, nil, apperrors.ErrDependency)
		return
	}
	utils.SetBrowserSession(c, token, time.Now().Add(passport.TokenExpireTime))
	user := &model.UserAccount{ID: identity.ID, Username: identity.UserName, DisplayName: identity.UserName, ContactEmail: identity.Email, ContactMobile: identity.Mobile, Status: model.AccountActive, Role: model.RoleUser, LibraryEnabled: true, WebDAVPermission: model.WebDAVWrite, Revision: 1}
	respond(c, view(user, token), nil)
}

func me(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		respond(c, nil, apperrors.ErrUnauthorized)
		return
	}
	respond(c, view(user, c.GetString(utils.CtxKeyLoginToken)), nil)
}

func logout(c *gin.Context) {
	err := passport.DeleteToken(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken))
	if err == nil {
		utils.ClearBrowserSession(c)
	}
	respond(c, nil, err)
}

func logoutAll(c *gin.Context) {
	user := currentUser(c)
	err := accountservice.LogoutAll(ginfmt.RPCContext(c), user.ID, user.AuthVersion)
	if err == nil {
		utils.ClearBrowserSession(c)
	}
	respond(c, nil, err)
}

func changePassword(c *gin.Context) {
	var input struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
		Confirm  string `json:"confirm_password"`
	}
	if !bind(c, &input, 16<<10) {
		return
	}
	user := currentUser(c)
	err := accountservice.ChangePassword(ginfmt.RPCContext(c), user.ID, user.AuthVersion, input.Current, input.Password, input.Confirm)
	if err == nil {
		utils.ClearBrowserSession(c)
	}
	respond(c, nil, err)
}

func updateProfile(c *gin.Context) {
	var input accountservice.ProfileInput
	if !bind(c, &input, 16<<10) {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	user := currentUser(c)
	updated, err := accountservice.UpdateProfile(ginfmt.RPCContext(c), user.ID, user.AuthVersion, rev, input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, view(updated, c.GetString(utils.CtxKeyLoginToken)), nil)
}

func registrationValues(c *gin.Context) (map[string]interface{}, error) {
	enabled := false
	if accountservice.DatabaseMode() {
		var err error
		enabled, err = accountservice.ModuleEnabled(ginfmt.RPCContext(c), "registration")
		if err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{"enabled": enabled, "monthly_limit": 3, "ttl_days": 7, "min_password_length": config.Runtime().AccountPolicy.MinPasswordLength}, nil
}

func registrationPolicy(c *gin.Context) {
	value, err := registrationValues(c)
	respond(c, value, err)
}

func publicBootstrap(c *gin.Context) {
	value, err := registrationValues(c)
	if err != nil {
		respond(c, nil, err)
		return
	}
	runtime := config.Runtime()
	respond(c, map[string]interface{}{"site": map[string]string{"title": runtime.SiteTitle, "notice": runtime.SiteNotice}, "registration": value}, nil)
}

func validateInvitation(c *gin.Context) {
	var input struct {
		Code string `json:"code"`
	}
	if !bind(c, &input, 1024) {
		return
	}
	if !middleware.AllowAccountAttempt("invite-probe:"+middleware.TrustedClientIP(c.Request), 30, time.Minute) {
		respond(c, nil, apperrors.ErrRateLimited)
		return
	}
	inv, err := accountservice.ValidateInvitation(ginfmt.RPCContext(c), input.Code)
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, map[string]interface{}{"valid": true, "expires_at": inv.ExpiresAt}, nil)
}

func register(c *gin.Context) {
	var input accountservice.RegistrationInput
	if !bind(c, &input, 16<<10) {
		return
	}
	if !middleware.AllowAccountAttempt("register:"+middleware.TrustedClientIP(c.Request), 10, time.Minute) {
		respond(c, nil, apperrors.ErrRateLimited)
		return
	}
	user, err := accountservice.Register(ginfmt.RPCContext(c), input)
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, map[string]string{"user_name": user.Username}, nil)
}

func listInvitations(c *gin.Context) {
	page, err := accountservice.ListInvitations(ginfmt.RPCContext(c), currentUser(c).ID)
	respond(c, page, err)
}

func createInvitation(c *gin.Context) {
	var input struct {
		Note      string `json:"note"`
		RequestID string `json:"request_id"`
	}
	if !bind(c, &input, 2048) {
		return
	}
	user := currentUser(c)
	inv, err := accountservice.CreateInvitation(ginfmt.RPCContext(c), user.ID, user.AuthVersion, input.Note, input.RequestID)
	if err != nil {
		respond(c, nil, err)
		return
	}
	url := ""
	if inv.Code != "" {
		url = c.GetHeader("Origin") + "/register#invite=" + inv.Code
	}
	respond(c, map[string]interface{}{"invitation": inv.Invitation, "code": inv.Code, "invite_url": url}, nil)
}

func revokeInvitation(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	user := currentUser(c)
	err := accountservice.RevokeInvitation(ginfmt.RPCContext(c), user.ID, user.AuthVersion, id)
	respond(c, nil, err)
}
