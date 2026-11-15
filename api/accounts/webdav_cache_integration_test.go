package accounts_test

import (
	"encoding/base64"
	"testing"
)

// TestHTTPWebDAVWarmCachePreservesCurrentAccountChecks warms both DAV aliases
// before exercising real account mutations, so cached password work cannot
// replace current permissions, password validity or module configuration.
func TestHTTPWebDAVWarmCachePreservesCurrentAccountChecks(t *testing.T) {
	for _, scenario := range []string{"password change", "ban", "permission revoked", "module disabled", "temporary password expired"} {
		t.Run(scenario, func(t *testing.T) {
			f := newHTTPFixture(t)
			owner := f.login(f.owner, integrationPassword)
			member := f.login(f.member, integrationPassword)
			requireSuccess(t, f.adminAction(owner, member.ID, "webdav-permission", "PUT", map[string]interface{}{"permission": "read"}))
			basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword))}
			for i := 0; i < 2; i++ {
				for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
					response := f.request("GET", path, nil, nil, basic)
					if response.Status != 200 || string(response.Raw) != "private fixture file" {
						t.Fatalf("warming DAV credentials failed: HTTP %d", response.Status)
					}
				}
			}
			switch scenario {
			case "password change":
				requireSuccess(t, f.request("POST", "/api/auth/change-password", member, map[string]interface{}{"current_password": integrationPassword, "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
			case "ban":
				requireSuccess(t, f.adminAction(owner, member.ID, "ban", "POST", map[string]interface{}{}))
			case "permission revoked":
				requireSuccess(t, f.adminAction(owner, member.ID, "webdav-permission", "PUT", map[string]interface{}{"permission": "none"}))
			case "module disabled":
				requireSuccess(t, f.publish(owner, "webdav", map[string]interface{}{"enabled": false}, "warm-cache-close-webdav"))
			case "temporary password expired":
				if _, err := f.database.Exec("UPDATE user_account SET must_change_password=1, password_expires_at=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id=?", member.ID); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
				requireDenied(t, f.request("GET", path, nil, nil, basic))
			}
			if scenario == "password change" {
				basic["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationNextPassword))
				if response := f.request("GET", "/webdav/fixture.txt", nil, nil, basic); response.Status != 200 {
					t.Fatalf("changed password could not authenticate: HTTP %d", response.Status)
				}
			}
		})
	}
}
