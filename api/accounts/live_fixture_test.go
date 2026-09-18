package accounts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestExportAccountCenterLiveFixture explicitly exports only synthetic identities
// and a protected configuration for an isolated loopback HTTP/browser smoke test.
func TestExportAccountCenterLiveFixture(t *testing.T) {
	dir := os.Getenv("ACCOUNTS_LIVE_FIXTURE_DIR")
	if dir == "" {
		t.Skip("opt-in synthetic live fixture")
	}
	dir = filepath.Clean(dir)
	if !strings.HasPrefix(dir, "/tmp/home-server-account-center-") {
		t.Fatal("unsafe live fixture path")
	}
	f := newHTTPFixture(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	conf := f.conf
	conf.Auth.SiteOrigin = "https://localhost:18443"
	conf.Auth.TrustedProxyCIDRs = []string{"127.0.0.1/32"}
	conf.WebProjects.SiteOrigin = conf.Auth.SiteOrigin
	conf.WebProjects.StorageRoot = filepath.Join(dir, "web")
	conf.WebDAV.SharePath = filepath.Join(dir, "dav")
	conf.Passport.RedirectLoginPath = conf.Auth.SiteOrigin + "/login"
	for _, path := range []string{conf.WebProjects.StorageRoot, conf.WebDAV.SharePath} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := yaml.Marshal(conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	identities, _ := json.Marshal(map[string]string{"admin": f.owner, "member": f.member, "guest": f.guest, "password": integrationPassword})
	if err := os.WriteFile(filepath.Join(dir, "identities.json"), identities, 0600); err != nil {
		t.Fatal(err)
	}
}
