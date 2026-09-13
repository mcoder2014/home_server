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

func requireDatabase(c *gin.Context) {
	if !accountservice.DatabaseMode() {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "账号管理尚未启用"))
		c.Abort()
		return
	}
	c.Next()
}

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
	data.AddRoute(http.MethodGet, "/api/account/invitations", user, requireDatabase, listInvitations)
	data.AddRoute(http.MethodPost, "/api/account/invitations", user, write, requireDatabase, createInvitation)
	data.AddRoute(http.MethodPost, "/api/account/invitations/:id/revoke", user, write, requireDatabase, revokeInvitation)
	data.AddRoute(http.MethodGet, "/api/admin/users", admin, listUsers)
	data.AddRoute(http.MethodPost, "/api/admin/users", admin, write, createUser)
	data.AddRoute(http.MethodGet, "/api/admin/users/:id", admin, getUser)
	for _, action := range []string{"ban", "unban", "delete", "reset-password", "logout-all"} {
		current := action
		data.AddRoute(http.MethodPost, "/api/admin/users/:id/"+current, admin, write, func(c *gin.Context) { changeUser(c, current) })
	}
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
	for _, action := range []string{"block", "delete", "restore", "unblock"} {
		current := action
		data.AddRoute(http.MethodPost, "/api/admin/web-share/:id/"+current, admin, write, func(c *gin.Context) { changeWebProject(c, current) })
	}
	data.AddRouteV2([]string{http.MethodGet, http.MethodHead}, []string{"/api/admin/web-share/:id/releases/:release_id/preview/*path"}, admin, previewWebProject)
	return nil
}
