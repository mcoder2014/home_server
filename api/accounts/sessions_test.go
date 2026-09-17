package accounts_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"testing"
	"time"

	passportapi "github.com/mcoder2014/home_server/api/passport"
	passportservice "github.com/mcoder2014/home_server/domain/service/passport"
)

// TestHTTPSessionAccessUsesBrowserGuards exercises browser policy separately
// from the selected-session ownership and idempotency transaction tests.
func TestHTTPSessionAccessUsesBrowserGuards(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	current := f.login(f.member, integrationPassword)
	other := f.login(f.member, integrationPassword)
	otherPage := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", other, nil, nil))
	otherID, _ := object(otherPage["current_session"])["id"].(string)
	missingCSRF := f.request(http.MethodPost, "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": []string{otherID}}, map[string]string{"X-CSRF-Token": ""})
	if missingCSRF.Status != http.StatusForbidden {
		t.Fatal("selected logout bypassed BrowserWrite")
	}
	requireDenied(t, f.request(http.MethodGet, "/api/account/sessions", current, nil, map[string]string{"Authorization": "Bearer synthetic-application-token"}))
	requireDenied(t, f.request(http.MethodGet, "/api/admin/users/"+admin.ID+"/sessions", current, nil, nil))
	requireDenied(t, f.request(http.MethodPost, "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": []int64{1}}, nil))
	f.me(current)
	f.me(other)
	f.me(admin)
}

// TestHTTPSessionRevokeOthersCoversUnloadedPages checks the visible list, user
// summary and detail after clearing more sessions than the first page contains.
func TestHTTPSessionRevokeOthersCoversUnloadedPages(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	current := f.login(f.member, integrationPassword)
	var others []*browserSession
	for i := 0; i < 21; i++ {
		others = append(others, f.login(f.member, integrationPassword))
	}
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions?limit=20", current, nil, nil))
	if number(page["total_count"]) != 22 || page["has_more"] != true || len(page["items"].([]interface{})) != 20 || page["next_cursor"] == "" {
		t.Fatal("session page must expose a two-part continuation cursor")
	}
	last := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions?limit=20&cursor="+page["next_cursor"].(string), current, nil, nil))
	if len(last["items"].([]interface{})) != 1 || last["has_more"] != false {
		t.Fatal("second page must contain the remaining effective login")
	}
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke-others", current, map[string]interface{}{}, nil))
	if number(result["revoked_count"]) != 21 {
		t.Fatal("logout others must cover effective sessions outside the loaded page")
	}
	for _, other := range others {
		requireDenied(t, f.request(http.MethodGet, "/api/auth/me", other, nil, nil))
	}
	f.me(current)
	page = requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", current, nil, nil))
	if number(page["total_count"]) != 1 || len(page["items"].([]interface{})) != 0 {
		t.Fatal("only the current login should remain effective")
	}
	detail := requireSuccess(t, f.request(http.MethodGet, "/api/admin/users/"+current.ID, admin, nil, nil))
	if number(detail["active_session_count"]) != 1 || number(detail["restricted_session_count"]) != 0 || number(detail["session_warning_threshold"]) != 10 || detail["server_time"] == nil {
		t.Fatal("administrator detail must share session counts and include the warning threshold")
	}
	users := requireSuccess(t, f.request(http.MethodGet, "/api/admin/users", admin, nil, nil))
	for _, raw := range users["items"].([]interface{}) {
		user := object(raw)
		if user["id"] == current.ID && number(user["active_session_count"]) != 1 {
			t.Fatal("administrator user page session aggregate disagrees with detail")
		}
	}
}

func TestHTTPSessionLimitedPasswordCannotReachManagement(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	created := requireSuccess(t, f.request(http.MethodPost, "/api/admin/users", admin, map[string]interface{}{"user_name": "sessions_initial_password", "generate_password": true}, nil))
	password := created["initial_password"].(string)
	limited := f.login("sessions_initial_password", password)
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/account/sessions"},
		{http.MethodPost, "/api/account/sessions/revoke"},
		{http.MethodPost, "/api/account/sessions/revoke-others"},
	} {
		response := f.request(request.method, request.path, limited, map[string]interface{}{"session_ids": []string{"1"}}, nil)
		if response.Status != http.StatusForbidden {
			t.Fatalf("limited password escaped through %s: status %d", request.path, response.Status)
		}
	}
	page := requireSuccess(t, f.request(http.MethodGet, "/api/admin/users/"+limited.ID+"/sessions", admin, nil, nil))
	if number(page["total_count"]) != 1 || number(page["restricted_count"]) != 1 {
		t.Fatal("administrator must count the valid restricted password-change login")
	}
	f.me(limited)
}

func TestHTTPSessionSelectiveLogoutKeepsExistingApplicationToken(t *testing.T) {
	f := newHTTPFixture(t)
	current := f.login(f.member, integrationPassword)
	other := f.login(f.member, integrationPassword)
	token := f.applicationToken(other, []string{"web-projects:read"})
	bearer := map[string]string{"Authorization": "Bearer " + token}
	requireSuccess(t, f.request(http.MethodGet, "/api/web-share", nil, nil, bearer))
	requireDenied(t, f.request(http.MethodGet, "/api/account/sessions", nil, nil, bearer))
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", current, nil, nil))
	if number(page["total_count"]) != 2 {
		t.Fatal("application issuance must not add a device session")
	}
	otherPage := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", other, nil, nil))
	otherID := object(otherPage["current_session"])["id"].(string)
	requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": []string{otherID}}, nil))
	requireDenied(t, f.request(http.MethodGet, "/api/auth/me", other, nil, nil))
	requireSuccess(t, f.request(http.MethodGet, "/api/web-share", nil, nil, bearer))
	another := f.login(f.member, integrationPassword)
	requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke-others", current, map[string]interface{}{}, nil))
	requireDenied(t, f.request(http.MethodGet, "/api/auth/me", another, nil, nil))
	requireSuccess(t, f.request(http.MethodGet, "/api/web-share", nil, nil, bearer))
	f.me(current)
}

// TestHTTPSessionLegacyMetadataSurvivesCookieExchange covers the actual RSA
// login and all cookie-exchange aliases without creating a second session or
// replacing issuance metadata with the exchanging browser's UA/IP.
func TestHTTPSessionLegacyMetadataSurvivesCookieExchange(t *testing.T) {
	f := newHTTPFixture(t, false)
	f.ip = "198.51.100.216:45006"
	f.router.POST("/passport/login", passportapi.Login)
	public, _, err := passportservice.GetLoginRsa(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(public)
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := rsa.EncryptPKCS1v15(rand.Reader, key.(*rsa.PublicKey), []byte(integrationPassword))
	if err != nil {
		t.Fatal(err)
	}
	ua := "curl/8.4.0"
	login := requireSuccess(t, f.request(http.MethodPost, "/passport/login", nil, map[string]string{"user_name": f.member, "crypt_passwd": base64.StdEncoding.EncodeToString(cipher)}, map[string]string{"User-Agent": ua, "X-Forwarded-For": "203.0.113.99"}))
	token := login["token"].(string)
	headers := map[string]string{"passport": token}
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", nil, nil, headers))
	current := object(page["current_session"])
	if number(page["total_count"]) != 1 || current["login_ip"] != "198.51.100.216" || current["user_agent"] != ua || current["login_source"] != "legacy" {
		t.Fatal("legacy login lost issuance metadata or trusted a forged proxy header")
	}
	f.ip = "198.51.100.217:45007"
	for _, path := range []string{"/api/auth/browser-login", "/api/web-share/browser-login", "/api/web-projects/browser-login"} {
		response := f.request(http.MethodPost, path, nil, nil, map[string]string{"passport": token, "User-Agent": "Mozilla/5.0 (iPhone)"})
		requireSuccess(t, response)
		var cookie *http.Cookie
		for _, candidate := range response.Cookies {
			if candidate.MaxAge > 0 {
				cookie = candidate
			}
		}
		if cookie == nil {
			t.Fatal("cookie exchange did not preserve its browser login contract")
		}
		browser := &browserSession{Cookie: cookie}
		after := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", browser, nil, nil))
		value := object(after["current_session"])
		if number(after["total_count"]) != 1 || value["id"] != current["id"] || value["user_agent"] != ua || value["login_ip"] != current["login_ip"] || value["authenticated_at"] != current["authenticated_at"] || value["expire_time"] != current["expire_time"] {
			t.Fatal("cookie exchange added a login or rewrote the original issuance metadata/deadline")
		}
	}
}

// TestHTTPSessionRevokeRechecksTokenAfterAuthentication pauses JSON consumption
// after middleware authentication and withdraws the acting session before the
// handler may update anything. A different live login must remain untouched.
func TestHTTPSessionRevokeRechecksTokenAfterAuthentication(t *testing.T) {
	f := newHTTPFixture(t)
	current := f.login(f.member, integrationPassword)
	other := f.login(f.member, integrationPassword)
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", other, nil, nil))
	otherID := object(page["current_session"])["id"].(string)
	payload, err := json.Marshal(map[string]interface{}{"session_ids": []string{otherID}})
	if err != nil {
		t.Fatal(err)
	}
	body := &blockedRequestBody{reader: bytes.NewReader(payload), entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	done := make(chan apiResponse, 1)
	go func() {
		defer close(body.finished)
		done <- f.request(http.MethodPost, "/api/account/sessions/revoke", current, body, nil)
	}()
	t.Cleanup(func() {
		body.releaseOnce.Do(func() { close(body.release) })
		select {
		case <-body.finished:
		case <-time.After(5 * time.Second):
			t.Error("paused session request did not exit before fixture cleanup")
		}
	})
	select {
	case <-body.entered:
	case response := <-done:
		t.Fatalf("request ended before its authenticated body read: %d", response.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("session request did not reach its authenticated body read")
	}
	requireSuccess(t, f.request(http.MethodPost, "/api/auth/logout", current, nil, nil))
	response := f.finishPausedRequest(body, done)
	if response.Status != http.StatusUnauthorized {
		t.Fatalf("revoked acting token must be rejected at transaction time: %d", response.Status)
	}
	f.me(other)
}
