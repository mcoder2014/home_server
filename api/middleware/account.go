package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

const AccountContextKey = "account"

// TrustedClientIP ignores forwarding headers from direct clients and walks a
// trusted proxy chain from right to left. A caller-controlled left prefix never
// replaces the nearest untrusted hop used by password failure budgets.
func TrustedClientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return "unknown"
	}
	chain := request.Header.Values("X-Forwarded-For")
	if len(chain) == 0 {
		chain = request.Header.Values("X-Real-IP")
	}
	if len(chain) != 1 || len(chain[0]) > 4096 {
		return peer.String()
	}
	hops := strings.Split(chain[0], ",")
	if len(hops) > 32 {
		return peer.String()
	}
	current := peer
	for i := len(hops) - 1; i >= 0; i-- {
		trusted := false
		for _, network := range trustedAuthProxies {
			if network.Contains(current) {
				trusted = true
				break
			}
		}
		if !trusted {
			return current.String()
		}
		next := net.ParseIP(strings.TrimSpace(hops[i]))
		if next == nil {
			return peer.String()
		}
		current = next
	}
	return current.String()
}

func CSRFToken(token string) string {
	digest := sha256.Sum256([]byte("home-server-csrf-v1:" + token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func BrowserOriginAllowed(request *http.Request) bool {
	origins := request.Header.Values("Origin")
	if len(origins) != 1 || origins[0] == "" {
		return false
	}
	conf := config.Global().Auth
	for _, allowed := range append([]string{conf.SiteOrigin}, conf.SiteOrigins...) {
		if allowed != "" && origins[0] == allowed {
			return true
		}
	}
	return false
}

func BrowserWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsHTTPS(c) || !BrowserOriginAllowed(c.Request) {
			ginfmt.Fail(c, apperrors.ErrForbidden)
			c.Abort()
			return
		}
		token := c.GetString(utils.CtxKeyLoginToken)
		if token != "" && (len(c.Request.Header.Values("X-CSRF-Token")) != 1 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(CSRFToken(token))) != 1) {
			ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "请刷新页面后重试"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAccount permits only human account sessions, including the explicitly
// restricted first-password session on me/change-password/logout routes.
func RequireAccount(admin, allowPasswordChange bool) gin.HandlerFunc {
	// 逐请求验证用户会话与当前管理员角色，并将受限改密会话限定在允许的接口内。
	return func(c *gin.Context) {
		if !IsHTTPS(c) {
			ginfmt.Fail(c, apperrors.ErrForbidden)
			c.Abort()
			return
		}
		c.Set(accounts.PasswordSourceIPKey, TrustedClientIP(c.Request))
		if c.GetHeader("Authorization") != "" || len(c.Request.Header.Values(HeaderKey)) > 1 {
			ginfmt.Fail(c, apperrors.ErrForbidden)
			c.Abort()
			return
		}
		token := c.GetHeader(HeaderKey)
		if token == "" {
			token = utils.BrowserSessionToken(c.Request)
		}
		var user *model.UserAccount
		var tokenExpiresAt time.Time
		var err error
		if accounts.DatabaseMode() {
			var session *model.AccountSession
			user, session, err = accounts.CheckSession(ginfmt.RPCContext(c), token, allowPasswordChange)
			if session != nil {
				tokenExpiresAt = session.ExpireTime
			}
		} else {
			var identity *model.UserIdentity
			identity, err = passport.CheckToken(ginfmt.RPCContext(c), token)
			if identity != nil {
				tokenExpiresAt = identity.SessionExpiry
				user = &model.UserAccount{ID: identity.ID, Username: identity.UserName, DisplayName: identity.UserName, ContactEmail: identity.Email, ContactMobile: identity.Mobile, Status: model.AccountActive, Role: model.RoleUser, LibraryEnabled: true, WebDAVPermission: model.WebDAVWrite, Revision: 1}
			}
		}
		if err != nil || user == nil {
			if err == nil {
				err = apperrors.ErrUnauthorized
			}
			ginfmt.Fail(c, err)
			c.Abort()
			return
		}
		if admin && (user.Role != model.RoleAdmin || !accounts.DatabaseMode()) {
			ginfmt.Fail(c, apperrors.ErrForbidden)
			c.Abort()
			return
		}
		principal := &utils.Principal{Kind: "user", UserID: user.ID, AuthVersion: user.AuthVersion, Role: user.Role, LibraryEnabled: user.LibraryEnabled, WebDAVPermission: user.WebDAVPermission, MustChangePassword: user.MustChangePassword, TokenExpiresAt: tokenExpiresAt}
		setPrincipal(c, principal, token)
		c.Set(AccountContextKey, user)
		c.Next()
	}
}

// AuthorizeCapability 按请求所需 scope 检查模块开关、当前账号状态和个人藏书/WebDAV授权。
// 用户会话还需匹配当前 auth_version；管理员身份不会隐式获得业务能力。
func AuthorizeCapability(ctx context.Context, principal *utils.Principal, scope string) error {
	if principal == nil {
		return apperrors.ErrUnauthorized
	}
	module := ""
	if strings.HasPrefix(scope, "library:") {
		module = "library"
	} else if strings.HasPrefix(scope, "webdav:") {
		module = "webdav"
	} else if strings.HasPrefix(scope, "web-projects:") {
		module = "web_projects"
	}
	if module == "" {
		return nil
	}
	enabled, err := accounts.ModuleEnabled(ctx, module)
	if err != nil {
		return err
	}
	if !enabled {
		return apperrors.WithMessage(apperrors.ErrForbidden, "功能暂未开放")
	}
	if !accounts.DatabaseMode() {
		return nil
	}
	user, err := accounts.GetByID(ctx, principal.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.Status != model.AccountActive || user.MustChangePassword {
		return apperrors.ErrUnauthorized
	}
	if principal.Kind == "user" && user.AuthVersion != principal.AuthVersion {
		return apperrors.ErrUnauthorized
	}
	if module == "library" && !user.LibraryEnabled {
		return apperrors.WithMessage(apperrors.ErrForbidden, "家庭藏书未开通，请联系管理员")
	}
	if module == "webdav" && (user.WebDAVPermission == model.WebDAVNone || (strings.HasSuffix(scope, ":write") && user.WebDAVPermission != model.WebDAVWrite)) {
		return apperrors.WithMessage(apperrors.ErrForbidden, "WebDAV权限不足，请联系管理员")
	}
	return nil
}

type rateEntry struct {
	Count   int
	Expires time.Time
}

var accountRateLock sync.Mutex
var accountRates = map[string]rateEntry{}

// AllowAccountAttempt bounds both the rate and memory used by unauthenticated
// login/invite probes. Raw login keys and credentials are never logged.
func AllowAccountAttempt(key string, limit int, window time.Duration) bool {
	digest := sha256.Sum256([]byte(key))
	key = string(digest[:])
	now := time.Now()
	accountRateLock.Lock()
	defer accountRateLock.Unlock()
	entry := accountRates[key]
	if !entry.Expires.After(now) {
		if len(accountRates) >= 4096 {
			for k, v := range accountRates {
				if !v.Expires.After(now) {
					delete(accountRates, k)
				}
			}
		}
		if len(accountRates) >= 4096 {
			return false
		}
		entry = rateEntry{Expires: now.Add(window)}
	}
	if entry.Count >= limit {
		return false
	}
	entry.Count++
	accountRates[key] = entry
	return true
}
