package accounts_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
)

func TestHTTPWebProjectPasswordProtectsContentAndComments(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	project, release := f.privatePage(owner)
	id := project["id"].(string)
	base := "/api/web-share/" + id
	project = requireSuccess(t, f.request(http.MethodPatch, base, owner, map[string]interface{}{"access_mode": "public", "container_mode": "raw"}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))

	status := requireSuccess(t, f.request(http.MethodGet, base+"/password", owner, nil, nil))
	if status["password_protected"] != false || number(status["version"]) != 0 {
		t.Fatalf("unexpected initial password state: %v", status)
	}
	if response := f.request(http.MethodPut, base+"/password", owner, map[string]interface{}{"password": "short", "version": 0}, nil); response.Status != http.StatusBadRequest {
		t.Fatalf("short password accepted: status=%d code=%d", response.Status, response.Code)
	}
	status = requireSuccess(t, f.request(http.MethodPut, "/api/web-projects/"+id+"/password", owner, map[string]interface{}{"password": "correct horse battery staple", "version": 0}, nil))
	if status["password_protected"] != true || number(status["version"]) != 1 {
		t.Fatalf("password was not enabled: %v", status)
	}
	if response := f.request(http.MethodPut, base+"/password", owner, map[string]interface{}{"password": "another valid password", "version": 0}, nil); response.Status != http.StatusConflict || response.Code != 5 {
		t.Fatalf("stale password version accepted: status=%d code=%d", response.Status, response.Code)
	}

	pagePath := "/p/http-private-fixture/index.html"
	locked := f.request(http.MethodGet, pagePath, nil, nil, map[string]string{"Accept": "text/html"})
	if locked.Status != http.StatusForbidden || locked.Header.Get("X-Resource-Password") != "required" || locked.Header.Get("X-Resource-ID") != id || !strings.Contains(string(locked.Raw), "type=\"password\"") {
		t.Fatalf("direct HTML did not return the native password gate: status=%d headers=%v body=%s", locked.Status, locked.Header, locked.Raw)
	}
	if ownerPage := f.request(http.MethodGet, pagePath, owner, nil, nil); ownerPage.Status != http.StatusOK {
		t.Fatalf("owner did not bypass password after ACL checks: %d", ownerPage.Status)
	}
	for _, projectStatus := range []model.WebProjectStatus{model.WebProjectStatusDisabled, model.WebProjectStatusDeleted} {
		if _, err := f.database.Exec("UPDATE web_project SET status = ? WHERE id = ?", projectStatus, id); err != nil {
			t.Fatal(err)
		}
		if ownerPage := f.request(http.MethodGet, pagePath, owner, nil, nil); ownerPage.Status != http.StatusNotFound {
			t.Fatalf("owner password bypass ignored %s project state: %d", projectStatus.String(), ownerPage.Status)
		}
		if ownerComments := f.request(http.MethodGet, base+"/comment-threads", owner, nil, nil); ownerComments.Status != http.StatusOK {
			t.Fatalf("owner lost %s project comment history: %d", projectStatus.String(), ownerComments.Status)
		}
		if guestComments := f.request(http.MethodGet, base+"/comment-threads", guest, nil, nil); guestComments.Status != http.StatusNotFound {
			t.Fatalf("non-owner read %s project comments: %d", projectStatus.String(), guestComments.Status)
		}
	}
	if _, err := f.database.Exec("UPDATE web_project SET status = ?, moderation_status = 'blocked' WHERE id = ?", model.WebProjectStatusEnabled, id); err != nil {
		t.Fatal(err)
	}
	if ownerPage := f.request(http.MethodGet, pagePath, owner, nil, nil); ownerPage.Status != http.StatusNotFound {
		t.Fatalf("owner password bypass ignored moderation state: %d", ownerPage.Status)
	}
	if _, err := f.database.Exec("UPDATE web_project SET moderation_status = 'normal' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path string }{{http.MethodHead, pagePath}, {http.MethodGet, "/p/http-private-fixture/style.css"}, {http.MethodGet, base + "/view-context"}} {
		response := f.request(request.method, request.path, nil, nil, nil)
		if response.Status != http.StatusForbidden {
			t.Fatalf("password gate bypassed for %s %s: status=%d code=%d", request.method, request.path, response.Status, response.Code)
		}
	}

	wrong := f.request(http.MethodPost, base+"/unlock", nil, map[string]interface{}{"password": "wrong password"}, nil)
	if wrong.Status != http.StatusForbidden || wrong.Code != 3 {
		t.Fatalf("wrong password must be stable 403/code=3: status=%d code=%d", wrong.Status, wrong.Code)
	}
	unlocked := f.request(http.MethodPost, "/api/web-projects/"+id+"/unlock", nil, map[string]interface{}{"password": "correct horse battery staple"}, nil)
	grant := passwordGrantCookie(t, unlocked.Cookies)
	if unlocked.Status != http.StatusOK || !grant.Secure || !grant.HttpOnly || grant.Path != "/" || grant.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe unlock response: status=%d cookie=%+v", unlocked.Status, grant)
	}
	cookieHeader := grant.Name + "=" + grant.Value
	if response := f.request(http.MethodGet, "/p/http-private-fixture/style.css", nil, nil, map[string]string{"Cookie": cookieHeader}); response.Status != http.StatusOK {
		t.Fatalf("valid grant did not unlock an asset: %d", response.Status)
	}
	if response := f.request(http.MethodGet, base+"/view-context", nil, nil, map[string]string{"Cookie": cookieHeader}); response.Status != http.StatusOK {
		t.Fatalf("valid grant did not unlock view context: %d", response.Status)
	}
	project = requireSuccess(t, f.request(http.MethodPatch, base, owner, map[string]interface{}{"container_mode": "enhanced"}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	comment := map[string]interface{}{"request_id": "password-comment", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "page"}, "body": "protected feedback"}
	guestCookies := guest.Cookie.Name + "=" + guest.Cookie.Value + "; " + cookieHeader
	if response := f.request(http.MethodPost, base+"/comment-threads", guest, comment, map[string]string{"Cookie": guestCookies}); response.Status != http.StatusCreated {
		t.Fatalf("unlocked comment write failed: status=%d body=%s", response.Status, response.Raw)
	}
	if response := f.request(http.MethodGet, base+"/comment-threads", guest, nil, nil); response.Status != http.StatusForbidden || response.Code != 3 {
		t.Fatalf("comment read bypassed password: status=%d code=%d", response.Status, response.Code)
	}

	status = requireSuccess(t, f.request(http.MethodPut, base+"/password", owner, map[string]interface{}{"password": "replacement password", "version": 1}, nil))
	if number(status["version"]) != 2 {
		t.Fatalf("password version did not advance: %v", status)
	}
	if response := f.request(http.MethodGet, pagePath, nil, nil, map[string]string{"Cookie": cookieHeader}); response.Status != http.StatusForbidden {
		t.Fatalf("old cookie survived password change: %d", response.Status)
	}
	status = requireSuccess(t, f.request(http.MethodPut, base+"/password", owner, map[string]interface{}{"password": "", "version": 2}, nil))
	if status["password_protected"] != false || number(status["version"]) != 3 {
		t.Fatalf("clear did not retain and advance version: %v", status)
	}
	if response := f.request(http.MethodGet, pagePath, nil, nil, nil); response.Status != http.StatusOK {
		t.Fatalf("cleared password still blocked content: %d", response.Status)
	}
}

func passwordGrantCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, "__Host-cq_read_") && cookie.MaxAge > 0 {
			return cookie
		}
	}
	t.Fatal("unlock did not issue a resource grant cookie")
	return nil
}

func TestHTTPFourBytePolicyAndQueryPasswordGate(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	current := requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", owner, nil, nil))
	values := object(current["values"])
	values["min_password_length"], values["min_share_password_length"], values["share_code_length"] = 4, 4, 4
	requireSuccess(t, f.publish(owner, "account_policy", values, "four-byte-password-policy"))
	bootstrap := requireSuccess(t, f.request("GET", "/api/site/bootstrap", nil, nil, nil))
	if number(object(bootstrap["share_password_policy"])["min_length"]) != 4 || number(object(bootstrap["registration"])["min_password_length"]) != 4 {
		t.Fatal("bootstrap omitted active password lengths")
	}
	project, _ := f.privatePage(owner)
	id := project["id"].(string)
	base := "/api/web-share/" + id
	queryPath := "/p/http-private-fixture/index.html?code=1234&view=wide"
	if response := f.request("GET", queryPath, nil, nil, map[string]string{"Accept": "text/html"}); response.Status != http.StatusNotFound || !strings.Contains(string(response.Raw), "history.replaceState") || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("private page must retain 404 while clearing the query password")
	}
	requireSuccess(t, f.request("PATCH", base, owner, map[string]interface{}{"access_mode": "public", "container_mode": "raw"}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	if response := f.request("PUT", base+"/password", owner, map[string]interface{}{"password": "123", "version": 0}, nil); response.Status != http.StatusBadRequest {
		t.Fatal("three-byte password accepted")
	}
	requireSuccess(t, f.request("PUT", base+"/password", owner, map[string]interface{}{"password": "1234", "version": 0}, nil))
	unlocked := f.request("POST", base+"/unlock", nil, map[string]interface{}{"password": "1234"}, nil)
	requireSuccess(t, unlocked)
	grant := passwordGrantCookie(t, unlocked.Cookies)
	for _, headers := range []map[string]string{nil, {"Cookie": grant.Name + "=" + grant.Value}, {"Cookie": owner.Cookie.Name + "=" + owner.Cookie.Value}} {
		page := f.request("GET", queryPath, nil, nil, headers)
		if page.Status != http.StatusForbidden || !strings.Contains(string(page.Raw), "history.replaceState") || strings.Contains(string(page.Raw), "code=1234") {
			t.Fatalf("query did not use isolated gate: %d", page.Status)
		}
	}
	values["min_share_password_length"] = 8
	requireSuccess(t, f.publish(owner, "account_policy", values, "restore-share-password-policy"))
	requireSuccess(t, f.request("POST", base+"/unlock", nil, map[string]interface{}{"password": "1234"}, nil))
	if bad := f.request("POST", base+"/unlock", nil, map[string]interface{}{"password": "4321"}, nil); bad.Status != http.StatusForbidden {
		t.Fatal("incorrect password accepted")
	}
}
