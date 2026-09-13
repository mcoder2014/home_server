package accounts_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Standard DAV clients send Basic credentials with each request. The actual DAV
// handler must work without browser-only Origin, CSRF, cookies or a login call,
// including when registration and application credentials are disabled.
func TestHTTPWebDAVBasicProtocolWithoutBrowserLogin(t *testing.T) {
	f := newHTTPFixture(t, false)
	owner := f.login(f.owner, integrationPassword)
	requireSuccess(t, f.adminAction(owner, "1002", "webdav-permission", "PUT", map[string]interface{}{"permission": "write"}))
	for _, prefix := range []string{"/webdav/", "/webdav_dev/"} {
		// 对每个 WebDAV 别名独立验证目录发现、读写和文件锁协议，不借用浏览器会话。
		t.Run(prefix, func(t *testing.T) {
			request := func(method, path, body string, extra map[string]string) apiResponse {
				headers := map[string]string{
					"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword)),
					"Origin":        "", "Content-Type": "application/xml", "X-CSRF-Token": "",
				}
				for key, value := range extra {
					headers[key] = value
				}
				response := f.request(method, prefix+path, nil, body, headers)
				if response.Header.Get("Location") != "" || len(response.Cookies) != 0 {
					t.Fatal("DAV request was redirected into browser login or issued a browser session")
				}
				return response
			}
			unauthorized := request("PROPFIND", "", "", map[string]string{"Authorization": ""})
			if unauthorized.Status != http.StatusUnauthorized || !strings.HasPrefix(strings.ToLower(unauthorized.Header.Get("WWW-Authenticate")), "basic ") {
				t.Fatalf("missing credentials must receive the Basic challenge, got %d", unauthorized.Status)
			}
			if response := request("OPTIONS", "", "", nil); response.Status != 200 || response.Header.Get("DAV") == "" {
				t.Fatalf("DAV discovery failed: status=%d", response.Status)
			}
			if response := request("PROPFIND", "", "", map[string]string{"Depth": "1"}); response.Status != 207 || !strings.Contains(string(response.Raw), "fixture.txt") {
				t.Fatalf("DAV listing failed: status=%d", response.Status)
			}
			if response := request("GET", "fixture.txt", "", nil); response.Status != 200 || string(response.Raw) != "private fixture file" {
				t.Fatalf("Basic read without browser login failed: status=%d", response.Status)
			}
			if response := request("HEAD", "fixture.txt", "", nil); response.Status != 200 || len(response.Raw) != 0 {
				t.Fatalf("DAV HEAD failed: status=%d", response.Status)
			}
			for _, step := range []struct {
				method, path, body string
				headers            map[string]string
				status             int
			}{
				{"MKCOL", "client", "", nil, 201},
				{"PUT", "client/file.txt", "DAV client bytes", nil, 201},
				{"COPY", "client/file.txt", "", map[string]string{"Destination": integrationOrigin + prefix + "client/copy.txt"}, 201},
				{"MOVE", "client/copy.txt", "", map[string]string{"Destination": integrationOrigin + prefix + "client/moved.txt"}, 201},
			} {
				if response := request(step.method, step.path, step.body, step.headers); response.Status != step.status {
					t.Fatalf("DAV %s failed: status=%d body=%s", step.method, response.Status, response.Raw)
				}
			}
			lockBody := `<D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner>synthetic client</D:owner></D:lockinfo>`
			locked := request("LOCK", "client/file.txt", lockBody, map[string]string{"Timeout": "Second-60"})
			if locked.Status != 200 || locked.Header.Get("Lock-Token") == "" {
				t.Fatalf("DAV LOCK failed: status=%d", locked.Status)
			}
			if response := request("UNLOCK", "client/file.txt", "", map[string]string{"Lock-Token": locked.Header.Get("Lock-Token")}); response.Status != 204 {
				t.Fatalf("DAV UNLOCK failed: status=%d", response.Status)
			}
			if response := request("DELETE", "client", "", nil); response.Status != 204 {
				t.Fatalf("DAV cleanup failed: status=%d", response.Status)
			}
		})
	}
	var sessions int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM login_token WHERE user_id=?", 1002).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("Basic requests unexpectedly created %d website sessions", sessions)
	}
	alias := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+"@example.com:"+integrationPassword)), "Origin": ""}
	if response := f.request("GET", "/webdav/fixture.txt", nil, nil, alias); response.Status != 200 {
		t.Fatalf("imported email alias broke Basic authentication: status=%d", response.Status)
	}
}

// TestHTTPWebDAVBasicSurvivesWebsiteSessionExpiryAndLogout 验证网站会话到期或全部退出不阻断有效 Basic；改密后旧密码拒绝，新密码无需重新网页登录即可使用。
func TestHTTPWebDAVBasicSurvivesWebsiteSessionExpiryAndLogout(t *testing.T) {
	f := newHTTPFixture(t, false)
	owner := f.login(f.owner, integrationPassword)
	requireSuccess(t, f.adminAction(owner, "1002", "webdav-permission", "PUT", map[string]interface{}{"permission": "read"}))
	member := f.login(f.member, integrationPassword)
	if _, err := f.database.Exec("UPDATE login_token SET expire_time=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE user_id=?", 1002); err != nil {
		t.Fatal(err)
	}
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword)), "Origin": ""}
	if response := f.request("GET", "/webdav/fixture.txt", nil, nil, basic); response.Status != 200 {
		t.Fatalf("expired website session blocked Basic: status=%d", response.Status)
	}
	member = f.login(f.member, integrationPassword)
	requireSuccess(t, f.request("POST", "/api/auth/logout-all", member, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", member, nil, nil))
	for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
		if response := f.request("GET", path, nil, nil, basic); response.Status != 200 || string(response.Raw) != "private fixture file" {
			t.Fatalf("website logout blocked Basic: status=%d", response.Status)
		}
	}
	member = f.login(f.member, integrationPassword)
	requireSuccess(t, f.request("POST", "/api/auth/change-password", member, map[string]interface{}{"current_password": integrationPassword, "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
	basic["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationNextPassword))
	if response := f.request("GET", "/webdav/fixture.txt", nil, nil, basic); response.Status != 200 {
		t.Fatalf("new password did not work directly in Basic without a new website session: status=%d", response.Status)
	}
}

// TestHTTPWebDAVBasicPasswordFailuresAreIndependentFromWebsite 验证网页登录与 Basic 的失败预算相互隔离，两个 WebDAV 路由仍共享同一套 Basic 失败限制。
func TestHTTPWebDAVBasicPasswordFailuresAreIndependentFromWebsite(t *testing.T) {
	f := newHTTPFixture(t, false)
	// Failure budgets are process-wide. Give these two synthetic accounts IDs
	// unused by the other HTTP fixtures so intentional lockouts cannot leak.
	for _, table := range []string{"user_account", "user_login_alias"} {
		column := "user_id"
		if table == "user_account" {
			column = "id"
		}
		if _, err := f.database.Exec("UPDATE " + table + " SET " + column + "=" + column + "+8800000 WHERE " + column + " IN (1002,1003)"); err != nil {
			t.Fatal(err)
		}
	}
	owner := f.login(f.owner, integrationPassword)
	for _, id := range []string{"8801002", "8801003"} {
		requireSuccess(t, f.adminAction(owner, id, "webdav-permission", "PUT", map[string]interface{}{"permission": "read"}))
	}
	for i := 0; i < 30; i++ {
		name := f.member
		if i >= 10 {
			name = fmt.Sprintf("website_unknown_%d", i)
		}
		requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": name, "password": "wrong password"}, nil))
	}
	if response := f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword}, nil); response.Status != 429 {
		t.Fatalf("website failure budget was not exercised: status=%d", response.Status)
	}
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(f.member+":"+integrationPassword)), "Origin": ""}
	if response := f.request("GET", "/webdav/fixture.txt", nil, nil, basic); response.Status != 200 {
		t.Fatalf("website password failures blocked valid Basic credentials: status=%d", response.Status)
	}
	f.ip = "198.51.100.237:48000"
	basic["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(f.guest+":wrong password"))
	for i := 0; i < 10; i++ {
		if response := f.request("GET", "/webdav/fixture.txt", nil, nil, basic); response.Status != 401 {
			t.Fatalf("Basic failure %d did not receive a credential challenge: status=%d", i, response.Status)
		}
	}
	basic["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(f.guest+":"+integrationPassword))
	if response := f.request("GET", "/webdav_dev/fixture.txt", nil, nil, basic); response.Status != 429 {
		t.Fatalf("Basic own failure budget was not retained across both routes: status=%d", response.Status)
	}
	f.login(f.guest, integrationPassword)
}

// TestHTTPWebDAVBasicAcceptsGrantedUnexpiredInitialPassword 验证未授权初始账号不能访问 WebDAV，授权后有效初始密码可直接使用，而网站改密与密码到期检查仍在。
func TestHTTPWebDAVBasicAcceptsGrantedUnexpiredInitialPassword(t *testing.T) {
	f := newHTTPFixture(t, false)
	owner := f.login(f.owner, integrationPassword)
	name := f.member + "_dav"
	created := requireSuccess(t, f.request("POST", "/api/admin/users", owner, map[string]interface{}{"user_name": name, "generate_password": true}, nil))
	password := created["initial_password"].(string)
	id := object(created["user"])["id"].(string)
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(name+":"+password)), "Origin": ""}
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
	requireSuccess(t, f.adminAction(owner, id, "webdav-permission", "PUT", map[string]interface{}{"permission": "read"}))
	for _, path := range []string{"/webdav/fixture.txt", "/webdav_dev/fixture.txt"} {
		if response := f.request("GET", path, nil, nil, basic); response.Status != 200 {
			t.Fatalf("website first-password-change flow blocked authorized Basic: status=%d", response.Status)
		}
	}
	session := f.login(name, password)
	if f.me(session)["must_change_password"] != true {
		t.Fatal("Basic unexpectedly removed the website password-change requirement")
	}
	requireDenied(t, f.request("GET", "/api/web-share", session, nil, nil))
	if _, err := f.database.Exec("UPDATE user_account SET password_expires_at=DATE_SUB(NOW(), INTERVAL 1 HOUR) WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	requireDenied(t, f.request("GET", "/webdav/fixture.txt", nil, nil, basic))
}
