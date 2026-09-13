package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/applications"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
)

var trustedAuthProxies []*net.IPNet

// ConfigureAuthentication runs once before registering handlers; request paths
// use parsed networks rather than trusting arbitrary forwarded headers.
func ConfigureAuthentication(conf config.AuthConfig) error {
	cidrs := conf.TrustedProxyCIDRs
	if len(cidrs) == 0 {
		cidrs = []string{"127.0.0.1/32", "::1/128"}
	}
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid auth trusted proxy CIDR")
		}
		networks = append(networks, network)
	}
	if conf.SiteOrigin != "" {
		origin, err := url.Parse(conf.SiteOrigin)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			return fmt.Errorf("auth.site_origin must be an HTTPS origin without path")
		}
	}
	trustedAuthProxies = networks
	return nil
}

// IsHTTPS accepts direct TLS or HTTPS asserted by an explicitly trusted proxy.
// RemoteAddr, not ClientIP/X-Forwarded-For, establishes proxy trust.
func IsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if c.GetHeader("X-Forwarded-Proto") != "https" {
		return false
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, network := range trustedAuthProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func RequireHTTPS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsHTTPS(c) {
			ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "HTTPS is required"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// ResolveIdentity authenticates one explicit credential source. A bad Bearer or
// conflicting headers never fall back to a user session. Application identity
// is separate, but existing resource ownership uses its fixed owner UserID.
func ResolveIdentity(c *gin.Context, scope string, allowSession, userOnly bool) (*utils.Principal, error) {
	passportToken := c.GetHeader(HeaderKey)
	authorization := c.GetHeader("Authorization")
	if len(c.Request.Header.Values(HeaderKey)) > 1 || len(c.Request.Header.Values("Authorization")) > 1 || (passportToken != "" && authorization != "") {
		return nil, apperrors.ErrInvalid
	}
	if authorization != "" {
		fields := strings.Fields(authorization)
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			return nil, apperrors.ErrUnauthorized
		}
		if userOnly {
			return nil, apperrors.ErrForbidden
		}
		if !config.Global().Auth.ApplicationsEnabled || !IsHTTPS(c) {
			return nil, apperrors.ErrUnauthorized
		}
		principal, err := applications.AuthenticateToken(ginfmt.RPCContext(c), fields[1])
		if err != nil {
			return nil, err
		}
		if principal == nil || principal.UserID <= 0 || principal.Kind != "application" || !principal.Allows(scope) {
			return nil, apperrors.ErrForbidden
		}
		setPrincipal(c, principal, "")
		return principal, nil
	}
	if passportToken == "" && allowSession {
		passportToken = utils.BrowserSessionToken(c.Request)
	}
	if passportToken == "" {
		return nil, apperrors.ErrUnauthorized
	}
	user, err := passport.CheckToken(ginfmt.RPCContext(c), passportToken)
	if err != nil {
		var cause *apperrors.Error
		if errors.As(err, &cause) && cause.Code == apperrors.ErrorCodeDbError {
			return nil, apperrors.ErrDependency
		}
		return nil, apperrors.ErrUnauthorized
	}
	if user == nil || user.ID <= 0 {
		return nil, apperrors.ErrUnauthorized
	}
	principal := &utils.Principal{Kind: "user", UserID: user.ID}
	setPrincipal(c, principal, passportToken)
	return principal, nil
}

func RequireIdentity(scope string, userOnly bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := ResolveIdentity(c, scope, false, userOnly); err != nil {
			if c.GetHeader("Authorization") != "" {
				c.Header("WWW-Authenticate", `Bearer realm="CQ Home Server"`)
			}
			ginfmt.Fail(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func setPrincipal(c *gin.Context, principal *utils.Principal, userToken string) {
	c.Set(utils.CtxKeyPrincipal, principal)
	c.Set(utils.CtxKeyLoginUseID, principal.UserID)
	if principal.Kind == "user" {
		c.Set(utils.CtxKeyLoginToken, userToken)
	}
	log.Ctx(ginfmt.RPCContext(c)).WithFields(map[string]interface{}{
		"auth_kind": principal.Kind, "application_id": principal.ApplicationID, "user_id": principal.UserID,
	}).Info("authenticated request")
}

// Application access is opt-in per existing route, never inferred from a path
// prefix. Adding another ValidateLogin route cannot accidentally authorize apps.
func legacyScope(c *gin.Context) string {
	switch c.Request.Method + " " + c.FullPath() {
	case "GET /bookinfo/query", "GET /library/book/query", "GET /library/book/total":
		return "library:read"
	case "POST /library/book/add", "POST /library/address/add":
		return "library:write"
	default:
		return ""
	}
}

func webDAVScope(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND":
		return "webdav:read"
	default:
		return "webdav:write"
	}
}
