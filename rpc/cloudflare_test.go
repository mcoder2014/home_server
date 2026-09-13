package rpc

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGetAllDNSRecord expects HOME_SERVER_TEST_CLOUDFLARE_DOMAIN to contain a
// DNS zone name, not a Cloudflare zone ID. Missing opt-in skips before any RPC.
func TestGetAllDNSRecord(t *testing.T) {
	domain := strings.TrimSuffix(strings.TrimSpace(os.Getenv("HOME_SERVER_TEST_CLOUDFLARE_DOMAIN")), ".")
	if domain == "" {
		t.Skip("set HOME_SERVER_TEST_CLOUDFLARE_DOMAIN to run the Cloudflare integration test")
	}
	records, err := GetAllDNSRecord(context.Background(), domain)
	require.Nil(t, err)
	t.Logf("received %d DNS records", len(records))
}
