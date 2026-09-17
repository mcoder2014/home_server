package accounts_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	passportapi "github.com/mcoder2014/home_server/api/passport"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
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

func requireSessionLimitDenied(t *testing.T, response apiResponse, legacy bool) {
	t.Helper()
	status := http.StatusTooManyRequests
	if legacy {
		status = http.StatusOK
	}
	if response.Status != status || response.Code != int(apperrors.ErrorCodeRateLimited) || !strings.Contains(string(response.Raw), "网站上限") || response.Data["token"] != nil {
		t.Fatalf("full session limit must preserve the response contract and explain how to recover: status=%d code=%d body=%s", response.Status, response.Code, response.Raw)
	}
	for _, cookie := range response.Cookies {
		if cookie.MaxAge > 0 {
			t.Fatal("a rejected login issued a live browser cookie")
		}
	}
}

func TestHTTPSessionLimitDefaultRejectsSixthAndExemptsApplicationsAndBasic(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	logins := []*browserSession{}
	for i := 0; i < 5; i++ {
		logins = append(logins, f.login(f.member, integrationPassword))
	}
	failed := f.request(http.MethodPost, "/api/auth/login", nil, map[string]string{"user_name": f.member, "password": integrationPassword}, nil)
	requireSessionLimitDenied(t, failed, false)
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

func TestHTTPSessionLimitLegacyCannotBypassAndCookieExchangeDoesNotConsumeSlots(t *testing.T) {
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
	requireSessionLimitDenied(t, f.request(http.MethodPost, "/passport/login", nil, payload, nil), true)
	requireSessionLimitDenied(t, f.request(http.MethodPost, "/api/auth/login", nil, map[string]string{"user_name": f.member, "password": integrationPassword}, nil), false)
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
	requireSessionLimitDenied(t, f.request(http.MethodPost, "/api/auth/login", nil, map[string]string{"user_name": f.member, "password": integrationPassword}, nil), false)
	for _, request := range []struct {
		path    string
		session *browserSession
	}{{"/api/account/sessions", logins[0]}, {"/api/admin/users/" + logins[0].ID + "/sessions", admin}} {
		page := requireSuccess(t, f.request(http.MethodGet, request.path, request.session, nil, nil))
		if number(page["total_count"]) != 6 || number(page["max_active_sessions"]) != 2 {
			t.Fatal("lowered session limit hid or invalidated existing logins")
		}
	}
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke-others", logins[0], nil, nil))
	if number(result["revoked_count"]) != 5 {
		t.Fatal("could not release slots after lowering the session limit")
	}
	f.login(f.member, integrationPassword)
	requireSessionLimitDenied(t, f.request(http.MethodPost, "/api/auth/login", nil, map[string]string{"user_name": f.member, "password": integrationPassword}, nil), false)
}
