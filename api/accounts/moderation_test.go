package accounts_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestHTTPUnblockRequiresOwnerPublish 仅操作合成库和临时网页，验证解除审核锁保持停用、保留修订号与审计约束，直到所有者显式发布才恢复公开访问。
func TestHTTPUnblockRequiresOwnerPublish(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	owner := f.login(f.member, integrationPassword)
	project, releaseID := f.privatePage(owner)
	id := project["id"].(string)
	project = requireSuccess(t, f.request("PATCH", "/api/web-share/"+id, owner,
		map[string]interface{}{"access_mode": "public"},
		map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	publicPath := "/p/http-private-fixture/index.html"
	initial := f.request("GET", publicPath, nil, nil, nil)
	if initial.Status != 200 || !strings.Contains(string(initial.Raw), "private review fixture") {
		t.Fatalf("synthetic public page was not initially readable: status=%d", initial.Status)
	}
	blocked := requireSuccess(t, f.request("POST", "/api/admin/web-share/"+id+"/block", admin,
		map[string]interface{}{"reason": "synthetic moderation block", "current_password": integrationPassword},
		map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	requireDenied(t, f.request("GET", publicPath, nil, nil, nil))
	unblockPath := "/api/admin/web-share/" + id + "/unblock"
	unblockInput := map[string]interface{}{"reason": "synthetic unlock only", "current_password": integrationPassword}
	currentRevision := map[string]string{"If-Match": strconv.FormatInt(number(blocked["revision"]), 10)}
	requireDenied(t, f.request("POST", unblockPath, owner, unblockInput, currentRevision))
	requireDenied(t, f.request("POST", unblockPath, admin, unblockInput,
		map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	unblocked := requireSuccess(t, f.request("POST", unblockPath, admin, unblockInput, currentRevision))
	if unblocked["status"] != "disabled" || unblocked["moderation_status"] != "normal" {
		t.Fatalf("unblock must remove only the moderation lock and keep content disabled: status=%v moderation=%v", unblocked["status"], unblocked["moderation_status"])
	}
	if number(unblocked["revision"]) != number(blocked["revision"])+1 || unblocked["current_release_id"] != releaseID {
		t.Fatal("unblock must advance the revision once and preserve the published release reference")
	}
	requireDenied(t, f.request("GET", publicPath, nil, nil, nil))
	var auditCount int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM admin_audit_log WHERE action='web_project_unblock' AND target_id=?", id).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("only the successful administrator unblock must be audited: count=%d err=%v", auditCount, err)
	}
	var rawSummary string
	if err := f.database.QueryRow("SELECT after_summary FROM admin_audit_log WHERE action='web_project_unblock' AND target_id=? AND actor_user_id=?", id, admin.ID).Scan(&rawSummary); err != nil {
		t.Fatal(err)
	}
	var summary map[string]interface{}
	if err := json.Unmarshal([]byte(rawSummary), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["status"] != "disabled" || summary["moderation_status"] != "normal" || number(summary["revision"]) != number(unblocked["revision"]) {
		t.Fatalf("unblock audit must record the committed disabled state: %s", rawSummary)
	}
	published := requireSuccess(t, f.request("POST", "/api/web-share/"+id+"/publish", owner,
		map[string]interface{}{"release_id": releaseID},
		map[string]string{"If-Match": strconv.FormatInt(number(unblocked["revision"]), 10)}))
	if published["status"] != "enabled" {
		t.Fatalf("explicit owner publication must enable the project: status=%v", published["status"])
	}
	final := f.request("GET", publicPath, nil, nil, nil)
	if final.Status != 200 || !strings.Contains(string(final.Raw), "private review fixture") {
		t.Fatalf("explicit owner publication did not restore public access: status=%d", final.Status)
	}
}
