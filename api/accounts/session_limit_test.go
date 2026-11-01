package accounts_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"testing"

	passportapi "github.com/mcoder2014/home_server/api/passport"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
)

// newSessionLimitHTTPFixture opts into the production default rather than the
// larger limit explicitly used by unrelated pagination/authentication fixtures.
func newSessionLimitHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	f := newHTTPFixture(t)
	defaults, err := config.BuildRuntimeSnapshot(f.conf, 0, nil, config.DefaultRuntimeValues(f.conf))
	if err != nil || defaults.AccountPolicy.MaxActiveSessions != 5 {
		t.Fatalf("expected the production default of five sessions: policy=%v error=%v", defaults.AccountPolicy, err)
	}
	snapshot := config.Runtime()
	snapshot.AccountPolicy.MaxActiveSessions = defaults.AccountPolicy.MaxActiveSessions
	if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHTTPSessionLimitDefaultReplacesSixthAndExemptsApplicationsAndBasic(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	logins := []*browserSession{}
	for i := 0; i < 5; i++ {
		logins = append(logins, f.login(f.member, integrationPassword))
	}
	oldest := logins[0]
	latest := f.login(f.member, integrationPassword)
	requireDenied(t, f.request(http.MethodGet, "/api/auth/me", oldest, nil, nil))
	logins = append(logins[1:], latest)
	for _, login := range logins {
		f.me(login)
	}
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", logins[0], nil, nil))
	if number(page["total_count"]) != 5 || number(page["max_active_sessions"]) != 5 {
		t.Fatal("session page did not expose the applied default limit")
	}
	admin := f.login(f.owner, integrationPassword)
	requireSuccess(t, f.adminAction(admin, logins[0].ID, "webdav-permission", http.MethodPut, map[string]interface{}{"permission": "read"}))
	token := f.applicationToken(logins[0], []string{"web-projects:read", "webdav:read"})
	bearer := map[string]string{"Authorization": "Bearer " + token}
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword))}
	for i := 0; i < 3; i++ {
		requireSuccess(t, f.request(http.MethodGet, "/api/web-share", nil, nil, bearer))
		for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
			response := f.request(http.MethodGet, path, nil, nil, basic)
			if response.Status != http.StatusOK || string(response.Raw) != "private fixture file" {
				t.Fatalf("session limit blocked authorized Basic access: status=%d", response.Status)
			}
		}
	}
	page = requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", logins[0], nil, nil))
	if number(page["total_count"]) != 5 {
		t.Fatal("application/Basic access consumed browser session slots")
	}
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke-others", logins[0], nil, nil))
	if number(result["revoked_count"]) != 4 {
		t.Fatal("user could not release occupied session slots")
	}
	f.login(f.member, integrationPassword)
	requireSuccess(t, f.request(http.MethodGet, "/api/web-share", nil, nil, bearer))
}

func TestHTTPSessionLimitLegacyReplacesOldestAndCookieExchangeDoesNotConsumeSlots(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	f.router.POST("/passport/login", passportapi.Login)
	public, _, err := passport.GetLoginRsa(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(public)
	if block == nil {
		t.Fatal("legacy login public key was missing")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := rsa.EncryptPKCS1v15(rand.Reader, key.(*rsa.PublicKey), []byte(integrationPassword))
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{"user_name": f.member, "crypt_passwd": base64.StdEncoding.EncodeToString(cipher)}
	legacy := requireSuccess(t, f.request(http.MethodPost, "/passport/login", nil, payload, nil))
	token := legacy["token"].(string)
	for i := 0; i < 4; i++ {
		f.login(f.member, integrationPassword)
	}
	legacy = requireSuccess(t, f.request(http.MethodPost, "/passport/login", nil, payload, nil))
	requireDenied(t, f.request(http.MethodGet, "/api/account/sessions", nil, nil, map[string]string{"passport": token}))
	token = legacy["token"].(string)
	f.login(f.member, integrationPassword)
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", nil, nil, map[string]string{"passport": token}))
	currentID := object(page["current_session"])["id"]
	for _, path := range []string{"/api/auth/browser-login", "/api/web-share/browser-login", "/api/web-projects/browser-login"} {
		response := f.request(http.MethodPost, path, nil, nil, map[string]string{"passport": token})
		requireSuccess(t, response)
		var cookie *http.Cookie
		for _, candidate := range response.Cookies {
			if candidate.MaxAge > 0 {
				cookie = candidate
			}
		}
		if cookie == nil {
			t.Fatal("cookie exchange failed at the session limit")
		}
		browser := &browserSession{Cookie: cookie}
		page = requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", browser, nil, nil))
		if number(page["total_count"]) != 5 || object(page["current_session"])["id"] != currentID {
			t.Fatal("cookie exchange created another session at the limit")
		}
	}
}

func TestHTTPSessionLimitAdministratorChangesApplyToNextLoginWithoutRevokingOld(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	logins := []*browserSession{}
	for i := 0; i < 5; i++ {
		logins = append(logins, f.login(f.member, integrationPassword))
	}
	policy := requireSuccess(t, f.request(http.MethodGet, "/api/admin/config/account_policy", admin, nil, nil))
	values := object(policy["values"])
	values["max_active_sessions"] = 6
	requireSuccess(t, f.publish(admin, "account_policy", values, "session-limit-raise-six"))
	logins = append(logins, f.login(f.member, integrationPassword))
	policy = requireSuccess(t, f.request(http.MethodGet, "/api/admin/config/account_policy", admin, nil, nil))
	values = object(policy["values"])
	values["max_active_sessions"] = 2
	requireSuccess(t, f.publish(admin, "account_policy", values, "session-limit-lower-two"))
	for _, login := range logins {
		f.me(login)
	}
	for _, request := range []struct {
		path    string
		session *browserSession
	}{{"/api/account/sessions", logins[0]}, {"/api/admin/users/" + logins[0].ID + "/sessions", admin}} {
		page := requireSuccess(t, f.request(http.MethodGet, request.path, request.session, nil, nil))
		if number(page["total_count"]) != 6 || number(page["max_active_sessions"]) != 2 {
			t.Fatal("lowered session limit hid or invalidated existing logins")
		}
	}
	latest := f.login(f.member, integrationPassword)
	requireDenied(t, f.request(http.MethodGet, "/api/auth/me", logins[0], nil, nil))
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", latest, nil, nil))
	if number(page["total_count"]) != 2 {
		t.Fatal("next login did not replace enough old sessions after lowering the cap")
	}
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke-others", latest, nil, nil))
	if number(result["revoked_count"]) != 1 {
		t.Fatal("could not release slots after lowering the session limit")
	}
	f.login(f.member, integrationPassword)
	latest = f.login(f.member, integrationPassword)
	page = requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", latest, nil, nil))
	if number(page["total_count"]) != 2 {
		t.Fatal("repeated login exceeded the lowered cap")
	}
}
