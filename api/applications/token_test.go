package applications

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTokenRequestAcceptsBasicOrFormCredentials(t *testing.T) {
	basic := newTokenRequest("grant_type=client_credentials", "Basic "+base64.StdEncoding.EncodeToString([]byte("ak_cq_client:sk_cq_secret")))
	request, oauthError := parseTokenRequest(basic)
	require.Nil(t, oauthError)
	require.Equal(t, "ak_cq_client", request.ClientID)
	require.Equal(t, "sk_cq_secret", request.ClientSecret)

	form := newTokenRequest("grant_type=client_credentials&client_id=ak_cq_form&client_secret=sk_cq_form", "")
	request, oauthError = parseTokenRequest(form)
	require.Nil(t, oauthError)
	require.Equal(t, "ak_cq_form", request.ClientID)
	require.Equal(t, "sk_cq_form", request.ClientSecret)
}

func TestParseTokenRequestRejectsMixedDuplicateAndQueryCredentials(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		authorization string
		query         string
	}{
		{name: "mixed", body: "grant_type=client_credentials&client_id=ak_cq_form&client_secret=sk_cq_form", authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("ak_cq_basic:sk_cq_basic"))},
		{name: "duplicate", body: "grant_type=client_credentials&client_id=one&client_id=two&client_secret=secret"},
		{name: "query", body: "grant_type=client_credentials&client_id=one&client_secret=secret", query: "client_secret=leaked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newTokenRequest(test.body, test.authorization)
			request.URL.RawQuery = test.query
			_, oauthError := parseTokenRequest(request)
			require.NotNil(t, oauthError)
			require.Equal(t, "invalid_request", oauthError.Name)
			require.NotContains(t, oauthError.Description, "secret")
		})
	}
}

func TestParseTokenRequestRejectsWrongMediaTypeAndGrant(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader("grant_type=client_credentials"))
	request.Header.Set("Content-Type", "application/json")
	_, oauthError := parseTokenRequest(request)
	require.Equal(t, http.StatusUnsupportedMediaType, oauthError.Status)

	request = newTokenRequest("grant_type=password&client_id=one&client_secret=two", "")
	_, oauthError = parseTokenRequest(request)
	require.Equal(t, "unsupported_grant_type", oauthError.Name)
}

func TestParseIfMatchRequiresPositiveExactRevision(t *testing.T) {
	revision, err := parseIfMatch("7")
	require.NoError(t, err)
	require.Equal(t, int64(7), revision)
	revision, err = parseIfMatch(`"8"`)
	require.NoError(t, err)
	require.Equal(t, int64(8), revision)
	for _, value := range []string{"", "0", "-1", "W/\"8\"", "7,8", "abc"} {
		_, err = parseIfMatch(value)
		require.Error(t, err, value)
	}
}

func newTokenRequest(body, authorization string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	return request
}
