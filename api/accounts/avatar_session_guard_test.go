package accounts_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// beginPausedAccountRequest waits for the handler to read an authenticated
// browser request body, so session revocation deterministically precedes writing.
func (f *httpFixture) beginPausedAccountRequest(path string, session *browserSession, payload []byte, headers map[string]string) (*blockedRequestBody, <-chan apiResponse) {
	f.t.Helper()
	body := &blockedRequestBody{reader: bytes.NewReader(payload), entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	done := make(chan apiResponse, 1)
	go func() {
		defer close(body.finished)
		done <- f.request(http.MethodPost, path, session, body, headers)
	}()
	f.t.Cleanup(func() {
		body.releaseOnce.Do(func() { close(body.release) })
		select {
		case <-body.finished:
		case <-time.After(5 * time.Second):
			f.t.Error("paused account request did not exit before fixture cleanup")
		}
	})
	select {
	case <-body.entered:
	case response := <-done:
		f.t.Fatalf("request ended before authenticated body read: status=%d code=%d", response.Status, response.Code)
	case <-time.After(5 * time.Second):
		f.t.Fatal("account request did not reach authenticated body read")
	}
	return body, done
}

func TestHTTPAvatarWriteRechecksRevokedActingSession(t *testing.T) {
	f := newHTTPFixture(t)
	acting := f.login(f.member, integrationPassword)
	surviving := f.login(f.member, integrationPassword)
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", acting, nil, nil))
	actingID := object(page["current_session"])["id"].(string)
	var beforeRevision, beforeAuthVersion int64
	if err := f.database.QueryRow("SELECT revision,auth_version FROM user_account WHERE id=?", acting.ID).Scan(&beforeRevision, &beforeAuthVersion); err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	payload, kind := avatarMultipart(t, raw.Bytes())
	blocked, done := f.beginPausedAccountRequest("/api/account/avatar", acting, payload, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(beforeRevision, 10)})
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke", surviving, map[string]interface{}{"session_ids": []string{actingID}}, nil))
	if number(result["revoked_count"]) != 1 {
		t.Fatal("acting session was not selectively revoked")
	}
	response := f.finishPausedRequest(blocked, done)
	var revision, authVersion, avatarVersion, avatarRows int64
	if err := f.database.QueryRow("SELECT revision,auth_version,avatar_version FROM user_account WHERE id=?", acting.ID).Scan(&revision, &authVersion, &avatarVersion); err != nil {
		t.Fatal(err)
	}
	if err := f.database.QueryRow("SELECT COUNT(*) FROM user_avatar WHERE user_id=?", acting.ID).Scan(&avatarRows); err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusUnauthorized || revision != beforeRevision || authVersion != beforeAuthVersion || avatarVersion != 0 || avatarRows != 0 {
		t.Fatalf("revoked avatar write must leave the account/image unchanged: status=%d revision=%d/%d auth_version=%d/%d avatar_version=%d rows=%d", response.Status, revision, beforeRevision, authVersion, beforeAuthVersion, avatarVersion, avatarRows)
	}
	f.me(surviving)
}

func TestHTTPProfileResetRechecksRevokedActingSession(t *testing.T) {
	f := newHTTPFixture(t)
	acting := f.login(f.owner, integrationPassword)
	surviving := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", acting, nil, nil))
	actingID := object(page["current_session"])["id"].(string)
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	upload, kind := avatarMultipart(t, raw.Bytes())
	imageUser := requireSuccess(t, f.request(http.MethodPost, "/api/account/avatar", member, upload, map[string]string{"Content-Type": kind, "If-Match": strconv.FormatInt(member.Revision, 10)}))
	before := requireSuccess(t, f.request(http.MethodPatch, "/api/account/profile", member, map[string]string{"display_name": "必须保留的资料"}, map[string]string{"If-Match": strconv.FormatInt(number(imageUser["revision"]), 10)}))
	avatarURL := before["avatar_url"].(string)
	beforeImage := f.request(http.MethodGet, avatarURL, surviving, nil, nil)
	if beforeImage.Status != http.StatusOK {
		t.Fatal("fixture avatar was not readable")
	}
	payload, err := json.Marshal(map[string]interface{}{"reset_display_name": true, "reset_avatar": true, "reason": "isolated revoked-session verification", "current_password": integrationPassword})
	if err != nil {
		t.Fatal(err)
	}
	blocked, done := f.beginPausedAccountRequest("/api/admin/users/"+member.ID+"/reset-profile", acting, payload, map[string]string{"If-Match": strconv.FormatInt(number(before["revision"]), 10)})
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke", surviving, map[string]interface{}{"session_ids": []string{actingID}}, nil))
	if number(result["revoked_count"]) != 1 {
		t.Fatal("acting administrator session was not selectively revoked")
	}
	response := f.finishPausedRequest(blocked, done)
	latest := f.me(member)
	var audits int64
	if err := f.database.QueryRow("SELECT COUNT(*) FROM admin_audit_log WHERE action=? AND target_id=?", "reset-profile", member.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	imageAfter := f.request(http.MethodGet, avatarURL, surviving, nil, nil)
	if response.Status != http.StatusUnauthorized || latest["display_name"] != before["display_name"] || latest["avatar_url"] != avatarURL || number(latest["avatar_version"]) != number(before["avatar_version"]) || number(latest["revision"]) != number(before["revision"]) || audits != 0 || imageAfter.Status != http.StatusOK || !bytes.Equal(imageAfter.Raw, beforeImage.Raw) {
		t.Fatalf("revoked profile reset must preserve identity, image, revision and audit: status=%d before=%v after=%v audits=%d image_status=%d", response.Status, before, latest, audits, imageAfter.Status)
	}
	f.me(surviving)
}
