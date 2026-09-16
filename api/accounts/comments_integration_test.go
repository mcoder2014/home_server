package accounts_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/config"
)

// Comments exercise real cookies, project ACLs and durable thread transitions.
func TestHTTPCommentsLifecycle(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	project, release := f.privatePage(owner)
	id := project["id"].(string)
	base := "/api/web-share/" + id
	context := f.request("GET", base+"/view-context", owner, nil, nil)
	if context.Status != 200 || context.Data["can_comment"] != true {
		t.Fatalf("missing authenticated container context: %d %v", context.Status, context.Data)
	}
	if context.Data["csrf_token"] == owner.CSRF {
		t.Fatal("hosted page received account-wide CSRF token")
	}
	if r := f.request("GET", "/p/http-private-fixture/", nil, nil, map[string]string{"Accept": "text/html"}); r.Status != 404 || !strings.Contains(string(r.Raw), "<h1>404</h1>") {
		t.Fatalf("private anonymous page must be an HTML 404: %d", r.Status)
	}
	if r := f.request("GET", "/p/http-private-fixture/", guest, nil, map[string]string{"Accept": "text/html"}); r.Status != 404 || !strings.Contains(string(r.Raw), "<h1>404</h1>") {
		t.Fatalf("private invisible user page must be an HTML 404: %d", r.Status)
	}
	if r := f.request("GET", base+"/comment-threads", guest, nil, nil); r.Status != 404 {
		t.Fatalf("invisible comments: %d", r.Status)
	}
	input := map[string]interface{}{"request_id": "first-comment", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "text", "exact": "private review fixture"}, "body": "Review this paragraph"}
	thread := requireSuccess(t, f.request("POST", base+"/comment-threads", owner, input, nil))
	tid := thread["id"].(string)
	duplicate := requireSuccess(t, f.request("POST", base+"/comment-threads", owner, input, nil))
	if duplicate["id"] != tid {
		t.Fatal("retry duplicated thread")
	}
	input["body"] = "different payload"
	if r := f.request("POST", base+"/comment-threads", owner, input, nil); r.Status != 409 {
		t.Fatalf("idempotency conflict: %d", r.Status)
	}
	oldReleaseID, _ := strconv.ParseInt(release, 10, 64)
	newReleaseID := oldReleaseID + 1
	var oldStorageKey string
	if err := f.database.QueryRow("SELECT storage_key FROM web_project_release WHERE project_id=? AND id=?", id, oldReleaseID).Scan(&oldStorageKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("INSERT INTO web_project_release (id,project_id,uploaded_by,storage_key,status,entry_file,sha256,file_count,total_bytes,extra) VALUES (?,?,?,?,2,'index.html',?,1,1,'{}')",
		newReleaseID, id, owner.ID, "synthetic/version/"+strconv.FormatInt(newReleaseID, 10), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("UPDATE web_project SET current_release_id=? WHERE id=?", newReleaseID, id); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	concurrent := make(chan apiResponse, 2)
	for range []int{0, 1} {
		go func() {
			<-start
			concurrent <- f.request("POST", base+"/comment-threads", owner, map[string]interface{}{
				"request_id": "concurrent-comment", "release_id": strconv.FormatInt(newReleaseID, 10), "page_key": "path:index.html", "page_path": "index.html",
				"anchor": map[string]interface{}{"kind": "page"}, "body": "Concurrent retry",
			}, nil)
		}()
	}
	close(start)
	firstConcurrent, secondConcurrent := requireSuccess(t, <-concurrent), requireSuccess(t, <-concurrent)
	if firstConcurrent["id"] != secondConcurrent["id"] {
		t.Fatal("concurrent retry duplicated thread")
	}
	if _, err := f.database.Exec("DELETE FROM web_project_release WHERE project_id=? AND id=?", id, oldReleaseID); err != nil {
		t.Fatal(err)
	}
	path := base + "/comment-threads/" + tid
	thread = requireSuccess(t, f.request("POST", path+"/replies", owner, map[string]interface{}{"request_id": "reply-1", "release_id": release, "body": "Fixed in next release"}, nil))
	if r := f.request("POST", path+"/replies", owner, map[string]interface{}{"request_id": "unknown-release", "release_id": "9223372036854775806", "body": "must not forge trace metadata"}, nil); r.Status != http.StatusConflict {
		t.Fatalf("unknown source release accepted: %d", r.Status)
	}
	legacyToken := f.applicationToken(owner, []string{"web-projects:write"})
	if r := f.request("POST", path+"/replies", nil, map[string]interface{}{"request_id": "legacy-scope", "body": "must not be accepted"}, map[string]string{"Authorization": "Bearer " + legacyToken}); r.Status != http.StatusForbidden {
		t.Fatalf("project scope granted comments: %d", r.Status)
	}
	commentToken := f.applicationToken(owner, []string{"web-comments:write"})
	thread = requireSuccess(t, f.request("POST", path+"/replies", nil, map[string]interface{}{"request_id": "app-reply", "body": "Application response"}, map[string]string{"Authorization": "Bearer " + commentToken}))
	for _, action := range []string{"resolve", "reopen"} {
		thread = requireSuccess(t, f.request("POST", path+"/"+action, owner, map[string]interface{}{"request_id": action + "-1", "release_id": release}, map[string]string{"If-Match": fmt.Sprint(number(thread["revision"]))}))
	}
	events := requireSuccess(t, f.request("GET", path+"/events", owner, nil, nil))
	items := events["items"].([]interface{})
	if len(items) != 5 {
		t.Fatal("comment/reply/state history lost")
	}
	appEvent := object(items[2])
	if object(items[1])["source_release_id"] != release {
		t.Fatalf("stale page reply lost its source release: %v", object(items[1]))
	}
	for _, index := range []int{3, 4} {
		if object(items[index])["source_release_id"] != release {
			t.Fatalf("stale page state event lost its source release: %v", object(items[index]))
		}
	}
	if appEvent["actor_application_id"] == "0" || appEvent["actor_name_snapshot"] != "HTTP fixture application" {
		t.Fatalf("application attribution lost: %v", appEvent)
	}
	if thread["status"] != "open" {
		t.Fatal("reopen failed")
	}
	if _, err := f.database.Exec("INSERT INTO web_project_release (id,project_id,uploaded_by,storage_key,status,entry_file,sha256,file_count,total_bytes,extra) VALUES (?,?,?,?,2,'index.html',?,1,1,'{}')",
		oldReleaseID, id, owner.ID, oldStorageKey, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec("UPDATE web_project SET current_release_id=? WHERE id=?", oldReleaseID, id); err != nil {
		t.Fatal(err)
	}
	// Public readers may comment when logged in; anonymous users may only read the page.
	requireSuccess(t, f.request("PATCH", base, owner, map[string]interface{}{"access_mode": "public"}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	if r := f.request("GET", base+"/view-context", nil, nil, nil); r.Status != 200 || r.Data["can_comment"] != false {
		t.Fatal("anonymous comment capability exposed")
	}
	if r := f.request("GET", base+"/comment-threads", nil, nil, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous comments leaked: %d", r.Status)
	}
	requireSuccess(t, f.request("POST", path+"/replies", guest, map[string]interface{}{"request_id": "guest-reply", "body": "Readable feedback"}, nil))
	if r := f.request("POST", path+"/resolve", guest, map[string]interface{}{"request_id": "guest-resolve"}, map[string]string{"If-Match": "5"}); r.Status != 403 {
		t.Fatalf("guest changed another thread: %d", r.Status)
	}
	page := f.request("GET", "/p/http-private-fixture/", nil, nil, nil)
	if page.Status != 200 || !strings.Contains(string(page.Raw), "/api/web-share/container.js") {
		t.Fatal("enhanced HTML lacks platform runtime")
	}
}

func TestHTTPCommentWriteRechecksApplicationInLegacyIdentityMode(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	project, release := f.privatePage(owner)
	id := project["id"].(string)
	token := f.applicationToken(owner, []string{"web-comments:write"})
	var applicationID int64
	if err := f.database.QueryRow("SELECT id FROM application ORDER BY id DESC LIMIT 1").Scan(&applicationID); err != nil {
		t.Fatal(err)
	}
	before := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(before) })
	legacy := before
	legacy.IdentitySource = "file"
	payload, _ := json.Marshal(map[string]interface{}{
		"request_id": "paused-legacy-comment", "release_id": release, "page_key": "path:index.html", "page_path": "index.html",
		"anchor": map[string]interface{}{"kind": "page"}, "body": "must be rejected after revoke",
	})
	blocked, done := f.beginPausedRequest("/api/web-share/"+id+"/comment-threads", token, "application/json", payload)
	config.SetGlobalConfig(legacy)
	if _, err := f.database.Exec("UPDATE application SET status=2, revision=revision+1 WHERE id=?", applicationID); err != nil {
		t.Fatal(err)
	}
	response := f.finishPausedRequest(blocked, done)
	if response.Status < 400 || response.Status >= 500 {
		t.Fatalf("revoked legacy-mode application write status=%d code=%d", response.Status, response.Code)
	}
	var count int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM web_comment_thread WHERE project_id=?", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("revoked application committed %d comment threads", count)
	}
}
