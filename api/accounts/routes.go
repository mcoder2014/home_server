package accounts

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/data"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

var configService *siteconfig.Service
var configServiceLock sync.RWMutex

func Configure(service *siteconfig.Service) {
	configServiceLock.Lock()
	configService = service
	configServiceLock.Unlock()
}

// requireDatabase 拒绝尚未迁入数据库账号模式的注册和账号管理请求，避免配置用户模式误用新数据模型。
func requireDatabase(c *gin.Context) {
	if !accountservice.DatabaseMode() {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "账号管理尚未启用"))
		c.Abort()
		return
	}
	c.Next()
}

// InitAuthRouter 注册公共账号、个人中心和管理员 HTTP 接口，并为各入口组合身份、HTTPS、Origin 与 CSRF 守卫。
// 管理动作通过固定动作名分派，不接受客户端指定任意服务方法。
func InitAuthRouter() error {
	user := middleware.RequireAccount(false, false)
	limited := middleware.RequireAccount(false, true)
	admin := middleware.RequireAccount(true, false)
	write := middleware.BrowserWrite()
	data.AddRoute(http.MethodGet, "/api/site/bootstrap", publicBootstrap)
	data.AddRoute(http.MethodGet, "/api/auth/registration-policy", registrationPolicy)
	data.AddRoute(http.MethodPost, "/api/auth/login", write, login)
	data.AddRoute(http.MethodPost, "/api/auth/invitations/validate", write, requireDatabase, validateInvitation)
	data.AddRoute(http.MethodPost, "/api/auth/register", write, requireDatabase, register)
	data.AddRoute(http.MethodGet, "/api/auth/me", limited, me)
	data.AddRoute(http.MethodPost, "/api/auth/logout", limited, write, logout)
	data.AddRoute(http.MethodPost, "/api/auth/logout-all", user, write, requireDatabase, logoutAll)
	data.AddRoute(http.MethodPost, "/api/auth/change-password", limited, write, requireDatabase, changePassword)
	data.AddRoute(http.MethodPatch, "/api/account/profile", user, write, requireDatabase, updateProfile)
	data.AddRoute(http.MethodPost, "/api/account/avatar", user, write, requireDatabase, updateAvatar)
	data.AddRoute(http.MethodDelete, "/api/account/avatar", user, write, requireDatabase, updateAvatar)
	data.AddRoute(http.MethodGet, "/api/account/avatars/:user_id/:version", user, requireDatabase, readAvatar)
	data.AddRoute(http.MethodGet, "/api/account/sessions", user, requireDatabase, listSessions)
	data.AddRoute(http.MethodPost, "/api/account/sessions/revoke", user, write, requireDatabase, revokeSessions)
	data.AddRoute(http.MethodPost, "/api/account/sessions/revoke-others", user, write, requireDatabase, revokeOtherSessions)
	data.AddRoute(http.MethodGet, "/api/account/invitations", user, requireDatabase, listInvitations)
	data.AddRoute(http.MethodPost, "/api/account/invitations", user, write, requireDatabase, createInvitation)
	data.AddRoute(http.MethodPost, "/api/account/invitations/:id/revoke", user, write, requireDatabase, revokeInvitation)
	data.AddRoute(http.MethodGet, "/api/admin/users", admin, listUsers)
	data.AddRoute(http.MethodPost, "/api/admin/users", admin, write, createUser)
	data.AddRoute(http.MethodGet, "/api/admin/users/:id", admin, getUser)
	data.AddRoute(http.MethodGet, "/api/admin/users/:id/sessions", admin, requireDatabase, adminUserSessions)
	// POST 动作依次提供封禁、恢复、删除、重置密码和踢出网站会话，均交由 changeUser 执行权限及版本校验。
	for _, action := range []string{"ban", "unban", "delete", "reset-password", "logout-all", "reset-profile"} {
		current := action
		data.AddRoute(http.MethodPost, "/api/admin/users/:id/"+current, admin, write, func(c *gin.Context) { changeUser(c, current) })
	}
	// PUT 动作分别更新管理员身份、共享藏书权限和 WebDAV 读写权限，三种授权互不隐式继承。
	for _, action := range []string{"role", "library-permission", "webdav-permission"} {
		current := action
		data.AddRoute(http.MethodPut, "/api/admin/users/:id/"+current, admin, write, func(c *gin.Context) { changeUser(c, current) })
	}
	data.AddRoute(http.MethodPost, "/api/admin/users/:id/invitations/:invitation_id/revoke", admin, write, adminRevokeInvitation)
	data.AddRoute(http.MethodGet, "/api/admin/audit-logs", admin, auditLogs)
	data.AddRoute(http.MethodGet, "/api/admin/config/schema", admin, configurationSchema)
	data.AddRoute(http.MethodGet, "/api/admin/config/status", admin, configurationStatus)
	data.AddRoute(http.MethodGet, "/api/admin/config", admin, configurationList)
	data.AddRoute(http.MethodGet, "/api/admin/config/:namespace", admin, configurationGet)
	data.AddRoute(http.MethodPut, "/api/admin/config/:namespace", admin, write, configurationPublish)
	data.AddRoute(http.MethodPost, "/api/admin/config/:namespace/validate", admin, write, configurationValidate)
	data.AddRoute(http.MethodPost, "/api/admin/config/:namespace/rollback", admin, write, configurationRollback)
	data.AddRoute(http.MethodGet, "/api/admin/config/:namespace/history", admin, configurationHistory)
	data.AddRoute(http.MethodGet, "/api/admin/web-share", admin, listWebProjects)
	data.AddRoute(http.MethodGet, "/api/admin/web-share/:id", admin, getWebProject)
	data.AddRoute(http.MethodGet, "/api/admin/web-share/:id/releases", admin, getWebReleases)
	// POST 审核动作分别下架锁定、删除、恢复或解除锁定，由 changeWebProject 统一转交审核用例。
	for _, action := range []string{"block", "delete", "restore", "unblock"} {
		current := action
		data.AddRoute(http.MethodPost, "/api/admin/web-share/:id/"+current, admin, write, func(c *gin.Context) { changeWebProject(c, current) })
	}
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/api/admin/web-share/:id/releases/:release_id/preview/*path"}, admin, previewWebProject)
	return nil
}
