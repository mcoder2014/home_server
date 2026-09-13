package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

const (
	HeaderKey = "passport"
)

// ValidateLogin preserves user-token behavior and explicitly enables scoped
// application identity for the existing library routes listed in legacyScope.
func ValidateLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := ResolveIdentity(c, legacyScope(c), true, false); err != nil {
			if c.GetHeader("Authorization") != "" {
				ginfmt.Fail(c, err)
			} else {
				ginfmt.FormatWithError(c, err)
			}
			c.Abort()
			return
		}
		if c.GetHeader(HeaderKey) == "" && c.GetHeader("Authorization") == "" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			guard := BrowserWrite()
			guard(c)
			if c.IsAborted() {
				return
			}
		}
		c.Next()
	}
}

// User login tokens can revoke only their own browser session. Applications
// have separate lifecycle endpoints and must never enter account operations.
func ValidateUserLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := ResolveIdentity(c, "", false, true); err != nil {
			if c.GetHeader("Authorization") != "" {
				ginfmt.Fail(c, err)
			} else {
				ginfmt.FormatWithError(c, err)
			}
			c.Abort()
			return
		}
		c.Next()
	}
}

// ValidateBasicAuth 多数 webdav client 仅支持基础身份验证
func ValidateBasicAuth() gin.HandlerFunc {

	unauhtorized := func(c *gin.Context) {
		c.Writer.Header().Set("WWW-Authenticate", `basic realm="Restricted"`)
		c.Writer.WriteHeader(http.StatusUnauthorized)
		c.Abort()
	}

	// 在一次 WebDAV 请求中选择 Basic 或 Bearer 通道，校验账号与方法权限后再放行文件操作。
	return func(c *gin.Context) {
		if accounts.DatabaseMode() && !IsHTTPS(c) {
			ginfmt.Fail(c, apperrors.ErrForbidden)
			c.Abort()
			return
		}
		if len(c.Request.Header.Values("Authorization")) > 1 || c.GetHeader(HeaderKey) != "" {
			ginfmt.Fail(c, apperrors.ErrInvalid)
			c.Abort()
			return
		}
		// Preserve existing Basic clients; Bearer apps use scoped authentication
		// and never fall back to username/password when token validation fails.
		if strings.HasPrefix(strings.ToLower(c.GetHeader("Authorization")), "bearer ") {
			if _, err := ResolveIdentity(c, webDAVScope(c.Request.Method), false, false); err != nil {
				ginfmt.Fail(c, err)
				c.Abort()
				return
			}
			c.Next()
			return
		}
		c.Set(accounts.PasswordBudgetScopeKey, accounts.PasswordBudgetWebDAV)
		c.Set(accounts.PasswordSourceIPKey, TrustedClientIP(c.Request))
		ctx := ginfmt.RPCContext(c)
		username, password, ok := c.Request.BasicAuth()
		if !ok {
			unauhtorized(c)
			log.Ctx(ctx).Infof("Not login")
			return
		}

		res, err := passport.ValidateUser(ctx, username, password)
		if err != nil {
			if errors.Is(err, apperrors.ErrRateLimited) {
				ginfmt.Fail(c, err)
				c.Abort()
				return
			}
			unauhtorized(c)
			log.Ctx(ctx).Warn("basic authentication failed")
			return
		}
		if res == nil {
			unauhtorized(c)
			log.Ctx(ctx).Warn("basic authentication failed")
			return
		}
		if accounts.DatabaseMode() {
			enabled, e := accounts.ModuleEnabled(ctx, "webdav")
			if e != nil {
				ginfmt.Fail(c, e)
				c.Abort()
				return
			}
			// Native DAV clients cannot complete website password-change screens.
			// ValidateUser still checks account state and temporary-password expiry.
			if !enabled || !accounts.WebDAVAllowed(res.WebDAVPermission, c.Request.Method) {
				ginfmt.Fail(c, apperrors.ErrForbidden)
				c.Abort()
				return
			}
		}

		logrus.Infof("basic auth success, user_id:%d", res.ID)
		c.Set(utils.CtxKeyLoginUseID, res.ID)
		c.Next()
	}
}
