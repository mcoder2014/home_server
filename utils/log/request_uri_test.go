package log

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRejectedCredentialQueriesAreRedactedFromAccessLogs(t *testing.T) {
	original := "/api/applications?CLIENT_SECRET=private-sk&%61ccess_token=private-token&cursor=20"
	result := RedactedURI(original)
	require.NotContains(t, result, "private-sk")
	require.NotContains(t, result, "private-token")
	require.Contains(t, result, "cursor=20")
	require.Equal(t, "/api/web-projects?cursor=20&limit=10", RedactedURI("/api/web-projects?cursor=20&limit=10"))
	require.Equal(t, "/api/auth/token?[redacted]", RedactedURI("/api/auth/token?client_secret=%XXprivate-sk"))
}

func TestTokenEndpointHidesEveryQueryField(t *testing.T) {
	require.Equal(t, "/api/auth/token?[redacted]", RedactedURI("/api/auth/token?foo=sk_cq_synthetic&client_id=ak_cq_synthetic"))
	require.NotContains(t, RedactedURI("/api/applications?foo=at_cq_synthetic"), "at_cq_synthetic")
	require.NotContains(t, RedactedURI("/api/applications?client_id=identifier"), "identifier")
	require.NotContains(t, RedactedURI("/p/at_cq_synthetic/"), "at_cq_synthetic")
	require.Equal(t, "/api/file-shares/[redacted]/download", RedactedURI("/api/file-shares/unguessable-share-token/download"))
}

func TestWebpageQueryPasswordRedacted(t *testing.T) {
	for _, uri := range []string{"/p/report/?code=1234&view=wide", "/p/report/?%63ode=1234&code=5678&view=wide", "/p/report/?CODE=1234&view=wide"} {
		result := RedactedURI(uri)
		require.NotContains(t, result, "1234")
		require.NotContains(t, result, "5678")
		require.Contains(t, result, "view=wide")
	}
}
