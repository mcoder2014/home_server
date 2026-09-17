package accounts_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"strconv"
	"strings"
	"testing"
)

func avatarMultipart(t *testing.T, raw []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func TestHTTPAvatarVersionsResetAndExistingAuthentication(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 64, 32))); err != nil {
		t.Fatal(err)
	}
	body, kind := avatarMultipart(t, imageBytes.Bytes())
	url := "/api/account/avatar"
	updated := requireSuccess(t, f.request("POST", url, member, body, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(member.Revision, 10)}))
	if updated["csrf_token"] != member.CSRF || number(updated["avatar_version"]) != member.Revision+1 {
		t.Fatal("upload lost csrf or did not advance the image version")
	}
	avatarURL := updated["avatar_url"].(string)
	response := f.request("GET", avatarURL, guest, nil, nil)
	if response.Status != 200 || response.Header.Get("Content-Type") != "image/jpeg" || response.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header.Get("Cache-Control"), "no-store") {
		t.Fatal("image response missing authorization-safe headers")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(response.Raw))
	if err != nil || decoded.Bounds().Dx() != 256 {
		t.Fatal("image was not normalized")
	}
	requireDenied(t, f.request("GET", avatarURL, nil, nil, nil))
	requireDenied(t, f.request("POST", url, member, body, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(member.Revision, 10)}))
	currentRev := number(updated["revision"])
	nick := requireSuccess(t, f.request("PATCH", "/api/account/profile", member, map[string]string{"display_name": "自定义名称"}, map[string]string{"If-Match": strconv.FormatInt(currentRev, 10)}))
	reset := map[string]interface{}{"reset_display_name": true, "reset_avatar": true}
	requireSuccess(t, f.adminAction(admin, member.ID, "reset-profile", "POST", reset))
	latest := f.me(member)
	if latest["display_name"] != "" || latest["avatar_url"] != "" || number(latest["avatar_version"]) != 0 || number(latest["revision"]) != number(nick["revision"])+1 {
		t.Fatal("reset did not atomically restore default identity")
	}
	requireDenied(t, f.request("GET", avatarURL, guest, nil, nil))
	deleteResult := requireSuccess(t, f.request("DELETE", url, member, nil, map[string]string{"If-Match": strconv.FormatInt(number(latest["revision"]), 10)}))
	if number(deleteResult["revision"]) != number(latest["revision"]) {
		t.Fatal("removing a missing avatar must be idempotent")
	}
	audit := requireSuccess(t, f.request("GET", "/api/admin/audit-logs?target_type=user&target_id="+member.ID, admin, nil, nil))
	logs, _ := audit["items"].([]interface{})
	if len(logs) == 0 || object(logs[0])["action"] != "reset-profile" || object(object(logs[0])["before"])["display_name"] != "自定义名称" {
		t.Fatal("reset profile audit lacks the actual prior identity")
	}
}

func TestHTTPAvatarAndResetRejectInvalidAndUnavailableAccounts(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	for _, raw := range [][]byte{[]byte("<svg/>"), bytes.Repeat([]byte("x"), (2<<20)+1)} {
		body, kind := avatarMultipart(t, raw)
		requireDenied(t, f.request("POST", "/api/account/avatar", member, body, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(member.Revision, 10)}))
	}
	requireDenied(t, f.adminAction(admin, member.ID, "reset-profile", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("POST", "/api/admin/users/"+admin.ID+"/reset-profile", member, map[string]interface{}{"reset_display_name": true, "reason": "fixture", "current_password": integrationPassword}, map[string]string{"If-Match": "1"}))
	requireSuccess(t, f.adminAction(admin, member.ID, "ban", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("GET", "/api/account/sessions", member, nil, nil))
	page := requireSuccess(t, f.request("GET", "/api/admin/users/"+member.ID+"/sessions", admin, nil, nil))
	if number(page["total_count"]) != 0 {
		t.Fatal("banned credentials were counted as usable sessions")
	}
}

func TestHTTPSessionsPreserveCurrentAndRejectCrossUserBatchAtomically(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	current := f.login(f.member, integrationPassword)
	other := f.login(f.member, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	page := requireSuccess(t, f.request("GET", "/api/account/sessions", current, nil, nil))
	items, _ := page["items"].([]interface{})
	if number(page["total_count"]) != 2 || len(items) != 1 || page["server_time"] == nil {
		t.Fatal("session page/count did not separate the current login")
	}
	currentID := object(page["current_session"])["id"].(string)
	otherID := object(items[0])["id"].(string)
	if object(items[0])["login_source"] != "web" || object(items[0])["login_ip"] != strings.Split(f.ip, ":")[0] {
		t.Fatal("issuance metadata missing or not the trusted login IP")
	}
	guestPage := requireSuccess(t, f.request("GET", "/api/account/sessions", guest, nil, nil))
	guestID := object(guestPage["current_session"])["id"].(string)
	for _, ids := range [][]string{{currentID, otherID}, {otherID, guestID}} {
		requireDenied(t, f.request("POST", "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": ids}, nil))
		f.me(other)
	}
	result := requireSuccess(t, f.request("POST", "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": []string{otherID}}, nil))
	if number(result["revoked_count"]) != 1 {
		t.Fatal("chosen session was not revoked")
	}
	requireDenied(t, f.request("GET", "/api/auth/me", other, nil, nil))
	f.me(current)
	f.me(guest)
	result = requireSuccess(t, f.request("POST", "/api/account/sessions/revoke", current, map[string]interface{}{"session_ids": []string{otherID}}, nil))
	if number(result["revoked_count"]) != 0 || number(result["already_inactive_count"]) != 1 {
		t.Fatal("retrying revocation is not idempotent")
	}
	third := f.login(f.member, integrationPassword)
	requireSuccess(t, f.request("POST", "/api/account/sessions/revoke-others", current, nil, nil))
	requireDenied(t, f.request("GET", "/api/auth/me", third, nil, nil))
	f.me(current)
	userPage := requireSuccess(t, f.request("GET", "/api/admin/users", admin, nil, nil))
	rows, _ := userPage["items"].([]interface{})
	for _, row := range rows {
		if object(row)["id"] == current.ID && number(object(row)["active_session_count"]) != 1 {
			t.Fatal("admin aggregate counts rejected sessions as active")
		}
	}
	requireDenied(t, f.request("GET", "/api/admin/users/"+current.ID+"/sessions", guest, nil, nil))
}

func TestHTTPProfileResetAuditFailureRollsBackNameImageAndRevision(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	body, kind := avatarMultipart(t, raw.Bytes())
	uploaded := requireSuccess(t, f.request("POST", "/api/account/avatar", member, body, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(member.Revision, 10)}))
	named := requireSuccess(t, f.request("PATCH", "/api/account/profile", member, map[string]string{"display_name": "保留名字"}, map[string]string{"If-Match": strconv.FormatInt(number(uploaded["revision"]), 10)}))
	if _, err := f.database.Exec("DROP TABLE admin_audit_log"); err != nil {
		t.Fatal(err)
	}
	response := f.adminAction(admin, member.ID, "reset-profile", "POST", map[string]interface{}{"reset_display_name": true, "reset_avatar": true})
	if response.Status < 500 {
		t.Fatal("missing audit must reject reset")
	}
	latest := f.me(member)
	if latest["display_name"] != "保留名字" || latest["avatar_url"] != uploaded["avatar_url"] || number(latest["revision"]) != number(named["revision"]) {
		t.Fatal("failed audit committed profile reset")
	}
	if f.request("GET", latest["avatar_url"].(string), admin, nil, nil).Status != 200 {
		t.Fatal("failed audit removed image bytes")
	}
}
