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

// bind 按接口指定上限解码一个 JSON 对象；拒绝未知字段和尾随第二个对象，失败时写出统一参数错误。
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

// currentUser 读取账号中间件已经确认的用户快照；不从请求参数接受操作者身份。
func currentUser(c *gin.Context) *model.UserAccount {
	value, exists := c.Get(middleware.AccountContextKey)
	if !exists {
		return nil
	}
	user, _ := value.(*model.UserAccount)
	return user
}

// positiveID 解析命名路径参数中的正整数资源 ID；非法值直接返回参数错误，避免后续查库。
func positiveID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return 0, false
	}
	return id, true
}

// revision 解析唯一的 If-Match 正整数版本，供账号和管理接口检测并发修改。
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

// pagination 为账号管理类列表解析游标和每页条数，限制单次查询在 1 到 100 条之间。
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

// respond 统一输出账号接口的数据或错误，并禁止浏览器缓存账号与配置响应。
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
	SessionPolicy     map[string]int  `json:"session_policy"`
}

func view(user *model.UserAccount, token string) accountView {
	runtime := config.Runtime()
	return accountView{UserAccount: user, CSRFToken: middleware.CSRFToken(token), Capabilities: map[string]bool{"library": user.LibraryEnabled && runtime.LibraryEnabled, "webdav": user.WebDAVPermission != model.WebDAVNone && runtime.WebDAVEnabled, "applications": runtime.Auth.ApplicationsEnabled, "web_projects": runtime.WebProjects.Enabled, "manuals": runtime.Manuals.Enabled}, PasswordPolicy: map[string]int{"min_length": runtime.AccountPolicy.MinPasswordLength}, ApplicationPolicy: map[string]int{"default_credential_ttl_days": runtime.Auth.DefaultCredentialTTLDays, "max_credential_ttl_days": runtime.Auth.MaxCredentialTTLDays, "max_applications_per_user": runtime.Auth.MaxApplicationsPerUser}, SessionPolicy: map[string]int{"max_active_sessions": runtime.AccountPolicy.MaxActiveSessions}}
}

// login 处理 POST /api/auth/login：验证用户名和密码，建立 HttpOnly 浏览器会话并返回本人资料与 CSRF 信息。
// 数据库模式交由账号服务签发受版本约束的会话；配置模式保留旧用户和令牌兼容。
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
		metadata := accountservice.NewSessionMetadata(middleware.TrustedClientIP(c.Request), c.Request.UserAgent(), "web")
		user, token, session, err := accountservice.Login(ctx, input.UserName, input.Password, metadata)
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

// me 处理 GET /api/auth/me：返回已认证用户的资料、当前可用能力、密码策略及 CSRF 信息。
func me(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		respond(c, nil, apperrors.ErrUnauthorized)
		return
	}
	respond(c, view(user, c.GetString(utils.CtxKeyLoginToken)), nil)
}

// logout 处理 POST /api/auth/logout：撤销当前用户会话，只有服务端撤销成功才清除浏览器 Cookie。
func logout(c *gin.Context) {
	err := passport.DeleteToken(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken))
	if err == nil {
		utils.ClearBrowserSession(c)
	}
	respond(c, nil, err)
}

// logoutAll 处理 POST /api/auth/logout-all：使本人全部已有网站会话失效，成功后清理当前浏览器 Cookie。
func logoutAll(c *gin.Context) {
	user := currentUser(c)
	err := accountservice.LogoutAll(ginfmt.RPCContext(c), user.ID, user.AuthVersion)
	if err == nil {
		utils.ClearBrowserSession(c)
	}
	respond(c, nil, err)
}

// changePassword 处理 POST /api/auth/change-password：验证当前密码与新密码确认，完成改密及凭证撤销后退出当前浏览器会话。
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

// updateProfile 处理 PATCH /api/account/profile：按用户版本更新本人的白名单资料字段，并返回新的资料快照。
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

// registrationValues 生成公开注册规则；数据库模式实时读取注册开关，固定月额度和邀请码有效期不由客户端决定。
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

// registrationPolicy 处理 GET /api/auth/registration-policy：向登录页和注册页提供注册是否开放及密码、邀请码规则。
func registrationPolicy(c *gin.Context) {
	value, err := registrationValues(c)
	respond(c, value, err)
}

// publicBootstrap 处理 GET /api/site/bootstrap：返回匿名可读的站点标题、公告和注册规则，不暴露用户资料或启动密钥。
func publicBootstrap(c *gin.Context) {
	value, err := registrationValues(c)
	if err != nil {
		respond(c, nil, err)
		return
	}
	runtime := config.Runtime()
	respond(c, map[string]interface{}{"site": map[string]string{"title": runtime.SiteTitle, "notice": runtime.SiteNotice}, "registration": value, "modules": map[string]bool{"manuals": runtime.Manuals.Enabled}}, nil)
}

// validateInvitation 处理 POST /api/auth/invitations/validate：在来源限流后校验邀请码，仅返回有效状态与到期时间，不消费邀请码。
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

// register 处理 POST /api/auth/register：限制请求体与来源频率，交由账号服务原子创建受邀用户并消费单次邀请码。
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

// listInvitations 处理 GET /api/account/invitations：列出本人的邀请码状态、本月剩余额度及下次重置时间。
func listInvitations(c *gin.Context) {
	page, err := accountservice.ListInvitations(ginfmt.RPCContext(c), currentUser(c).ID)
	respond(c, page, err)
}

// createInvitation 处理 POST /api/account/invitations：按请求幂等键生成本人邀请码，并以已验证的 Origin 组成一次性分享链接。
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

// revokeInvitation 处理 POST /api/account/invitations/:id/revoke：撤销本人尚未使用的邀请码，不能操作其他邀请人的记录。
func revokeInvitation(c *gin.Context) {
	id, ok := positiveID(c, "id")
	if !ok {
		return
	}
	user := currentUser(c)
	err := accountservice.RevokeInvitation(ginfmt.RPCContext(c), user.ID, user.AuthVersion, id)
	respond(c, nil, err)
}
