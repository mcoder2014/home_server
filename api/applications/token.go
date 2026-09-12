package applications

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	service "github.com/mcoder2014/home_server/domain/service/applications"
	appErrors "github.com/mcoder2014/home_server/errors"
)

const maxTokenRequestBytes = 8 << 10

type tokenRequest struct {
	ClientID     string
	ClientSecret string
}

type oauthError struct {
	Name        string `json:"error"`
	Description string `json:"error_description,omitempty"`
	Code        int    `json:"code,omitempty"`
	Status      int    `json:"-"`
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// Token implements the RFC 6749 client_credentials exchange. The route owner
// applies RequireHTTPS; this handler additionally rejects every query string so
// credentials cannot be copied into URL logs.
func Token(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	request, parseError := parseTokenRequest(c.Request)
	if parseError != nil {
		writeOAuthError(c, parseError)
		return
	}
	applicationService := service.Default()
	if applicationService == nil {
		writeOAuthError(c, &oauthError{Name: "temporarily_unavailable", Description: "authentication service unavailable", Code: int(appErrors.ErrDependency.Code), Status: http.StatusServiceUnavailable})
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
	c.JSON(http.StatusOK, oauthTokenResponse{AccessToken: issued.AccessToken, TokenType: "Bearer", ExpiresIn: issued.ExpiresIn, Scope: strings.Join(issued.Scopes, " ")})
}

func parseTokenRequest(request *http.Request) (*tokenRequest, *oauthError) {
	if request == nil || request.Method != http.MethodPost || request.URL.RawQuery != "" {
		return nil, invalidTokenRequest(http.StatusBadRequest)
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, invalidTokenRequest(http.StatusUnsupportedMediaType)
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxTokenRequestBytes+1))
	if err != nil || len(body) > maxTokenRequestBytes {
		return nil, invalidTokenRequest(http.StatusBadRequest)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values["grant_type"]) != 1 || values.Get("grant_type") == "" {
		return nil, invalidTokenRequest(http.StatusBadRequest)
	}
	for key := range values {
		if key != "grant_type" && key != "client_id" && key != "client_secret" {
			return nil, invalidTokenRequest(http.StatusBadRequest)
		}
	}
	if values.Get("grant_type") != "client_credentials" {
		return nil, &oauthError{Name: "unsupported_grant_type", Description: "grant type is not supported", Code: int(appErrors.ErrInvalid.Code), Status: http.StatusBadRequest}
	}

	authorization := request.Header.Get("Authorization")
	formCredentials := len(values["client_id"]) > 0 || len(values["client_secret"]) > 0
	if authorization != "" {
		if formCredentials {
			return nil, invalidTokenRequest(http.StatusBadRequest)
		}
		clientID, clientSecret, ok := request.BasicAuth()
		if !ok || clientID == "" || clientSecret == "" {
			return nil, invalidTokenRequest(http.StatusBadRequest)
		}
		return &tokenRequest{ClientID: clientID, ClientSecret: clientSecret}, nil
	}
	if len(values["client_id"]) != 1 || len(values["client_secret"]) != 1 || values.Get("client_id") == "" || values.Get("client_secret") == "" {
		return nil, invalidTokenRequest(http.StatusBadRequest)
	}
	return &tokenRequest{ClientID: values.Get("client_id"), ClientSecret: values.Get("client_secret")}, nil
}

func invalidTokenRequest(status int) *oauthError {
	response := &oauthError{Name: "invalid_request", Description: "request parameters are invalid", Code: int(appErrors.ErrInvalid.Code), Status: status}
	return response
}

func writeOAuthError(c *gin.Context, oauthError *oauthError) {
	if oauthError.Status == 0 {
		oauthError.Status = http.StatusBadRequest
	}
	c.JSON(oauthError.Status, oauthError)
}
