package applications

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/applications"
	appErrors "github.com/mcoder2014/home_server/errors"
)

const maxApplicationAccessTokenRequestBytes = 8 << 10

type applicationAccessTokenRequest struct {
	ClientID     string
	ClientSecret string
}

type oauthError struct {
	Name        string `json:"error"`
	Description string `json:"error_description,omitempty"`
	Code        int    `json:"code,omitempty"`
	Status      int    `json:"-"`
}

type applicationAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// IssueApplicationAccessToken implements the RFC 6749 client_credentials exchange. The route owner
// applies RequireHTTPS; this handler additionally rejects every query string so
// credentials cannot be copied into URL logs.
// IssueApplicationAccessToken 处理 POST /api/auth/token：验证 AK/SK 后签发限定 scope 的应用访问令牌，不创建用户会话。
func IssueApplicationAccessToken(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	request, parseError := parseApplicationAccessTokenRequest(c.Request)
	if parseError != nil {
		writeOAuthError(c, parseError)
		return
	}
	applicationService := service.Default()
	if applicationService == nil {
		writeOAuthError(c, &oauthError{Name: "temporarily_unavailable", Description: "authentication service unavailable", Code: int(appErrors.ErrDependency.Code), Status: http.StatusServiceUnavailable})
		return
	}
	enabled, enabledErr := accounts.ModuleEnabled(c.Request.Context(), "auth")
	if enabledErr != nil {
		writeOAuthError(c, &oauthError{Name: "temporarily_unavailable", Description: "authentication service unavailable", Code: int(appErrors.ErrDependency.Code), Status: http.StatusServiceUnavailable})
		return
	}
	if !enabled {
		writeOAuthError(c, &oauthError{Name: "unauthorized_client", Description: "application authentication is disabled", Code: int(appErrors.ErrForbidden.Code), Status: http.StatusForbidden})
		return
	}
	issued, err := applicationService.IssueToken(c.Request.Context(), request.ClientID, request.ClientSecret)
	if err != nil {
		if errors.Is(err, appErrors.ErrUnauthorized) {
			c.Header("WWW-Authenticate", `Basic realm="token"`)
			writeOAuthError(c, &oauthError{Name: "invalid_client", Description: "client authentication failed", Code: int(appErrors.ErrUnauthorized.Code), Status: http.StatusUnauthorized})
			return
		}
		writeOAuthError(c, &oauthError{Name: "temporarily_unavailable", Description: "authentication service unavailable", Code: int(appErrors.ErrDependency.Code), Status: http.StatusServiceUnavailable})
		return
	}
	c.JSON(http.StatusOK, applicationAccessTokenResponse{AccessToken: issued.AccessToken, TokenType: "Bearer", ExpiresIn: issued.ExpiresIn, Scope: strings.Join(issued.Scopes, " ")})
}

// parseApplicationAccessTokenRequest 解析 client_credentials 换 Token 请求，限制表单大小并拒绝 URL 凭据、重复字段及混合 Basic/表单凭据。
func parseApplicationAccessTokenRequest(request *http.Request) (*applicationAccessTokenRequest, *oauthError) {
	if request == nil || request.Method != http.MethodPost || request.URL.RawQuery != "" {
		return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, invalidApplicationAccessTokenRequest(http.StatusUnsupportedMediaType)
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxApplicationAccessTokenRequestBytes+1))
	if err != nil || len(body) > maxApplicationAccessTokenRequestBytes {
		return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values["grant_type"]) != 1 || values.Get("grant_type") == "" {
		return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
	}
	for key := range values {
		if key != "grant_type" && key != "client_id" && key != "client_secret" {
			return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
		}
	}
	if values.Get("grant_type") != "client_credentials" {
		return nil, &oauthError{Name: "unsupported_grant_type", Description: "grant type is not supported", Code: int(appErrors.ErrInvalid.Code), Status: http.StatusBadRequest}
	}

	authorization := request.Header.Get("Authorization")
	formCredentials := len(values["client_id"]) > 0 || len(values["client_secret"]) > 0
	if authorization != "" {
		if formCredentials {
			return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
		}
		clientID, clientSecret, ok := request.BasicAuth()
		if !ok || clientID == "" || clientSecret == "" {
			return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
		}
		return &applicationAccessTokenRequest{ClientID: clientID, ClientSecret: clientSecret}, nil
	}
	if len(values["client_id"]) != 1 || len(values["client_secret"]) != 1 || values.Get("client_id") == "" || values.Get("client_secret") == "" {
		return nil, invalidApplicationAccessTokenRequest(http.StatusBadRequest)
	}
	return &applicationAccessTokenRequest{ClientID: values.Get("client_id"), ClientSecret: values.Get("client_secret")}, nil
}

func invalidApplicationAccessTokenRequest(status int) *oauthError {
	response := &oauthError{Name: "invalid_request", Description: "request parameters are invalid", Code: int(appErrors.ErrInvalid.Code), Status: status}
	return response
}

// writeOAuthError 按 OAuth 错误结构和 HTTP 状态输出换 Token 失败，未指定状态时使用参数错误。
func writeOAuthError(c *gin.Context, oauthError *oauthError) {
	if oauthError.Status == 0 {
		oauthError.Status = http.StatusBadRequest
	}
	c.JSON(oauthError.Status, oauthError)
}
