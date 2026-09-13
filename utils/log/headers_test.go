package log

import (
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestAuthenticationHeadersAreNotWrittenToDiagnostics(t *testing.T) {
	input := http.Header{"Authorization": {"Bearer synthetic-token"}, "Cookie": {"session=synthetic"}, "Passport": {"synthetic-user-token"}, "X-Custom-Secret": {"synthetic-key"}, "Depth": {"1"}}
	result := RedactedHeaders(input)
	for _, name := range []string{"Authorization", "Cookie", "Passport", "X-Custom-Secret"} {
		require.Equal(t, "[redacted]", result.Get(name))
	}
	require.Equal(t, "1", result.Get("Depth"))
	require.Equal(t, "Bearer synthetic-token", input.Get("Authorization"))
}
