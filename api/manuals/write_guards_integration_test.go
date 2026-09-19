package manuals

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	passportservice "github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

// manualBlockedBody pauses the first handler read. Authentication middleware
// has completed at that point, while no request payload has reached a write.
type manualBlockedBody struct {
	reader      io.Reader
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func (body *manualBlockedBody) Read(buffer []byte) (int, error) {
	body.enteredOnce.Do(func() { close(body.entered) })
	<-body.release
	return body.reader.Read(buffer)
}

func (body *manualBlockedBody) unblock() {
	body.releaseOnce.Do(func() { close(body.release) })
}

func beginBlockedManualRequest(t *testing.T, fixture *manualHTTPFixture, method, path string, session *manualHTTPSession, payload []byte, headers map[string]string) (*manualBlockedBody, <-chan manualHTTPResponse) {
	t.Helper()
	body := &manualBlockedBody{reader: bytes.NewReader(payload), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan manualHTTPResponse, 1)
	go func() {
		done <- fixture.request(method, path, session, body, headers)
	}()
	t.Cleanup(body.unblock)
	select {
	case <-body.entered:
	case response := <-done:
		t.Fatalf("manual request ended before authenticated body read: status=%d code=%d", response.status, response.code)
	case <-time.After(5 * time.Second):
		t.Fatal("manual request did not reach authenticated body read")
	}
	return body, done
}

func finishBlockedManualRequest(t *testing.T, body *manualBlockedBody, done <-chan manualHTTPResponse) manualHTTPResponse {
	t.Helper()
	body.unblock()
	select {
	case response := <-done:
		return response
	case <-time.After(10 * time.Second):
		t.Fatal("manual request did not finish after body release")
		return manualHTTPResponse{}
	}
}

func createManualForWriteGuard(t *testing.T, fixture *manualHTTPFixture, requestID string) string {
	t.Helper()
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, map[string]interface{}{
		"name": "并发测试说明书", "categories": []string{"测试"}, "access_mode": "owner", "client_request_id": requestID,
	}, nil))
	return created["id"].(string)
}

func countManualRows(t *testing.T, fixture *manualHTTPFixture, query string, arguments ...interface{}) int64 {
	t.Helper()
	var count int64
	require.NoError(t, fixture.database.QueryRow(query, arguments...).Scan(&count))
	return count
}

func TestManualCreateRejectsSessionRevokedAfterAuthentication(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	payload, err := json.Marshal(map[string]interface{}{
		"name": "不得创建", "categories": []string{}, "access_mode": "owner", "client_request_id": "revoked-create",
	})
	require.NoError(t, err)
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals", fixture.owner, payload, nil)
	require.NoError(t, accountservice.Logout(context.Background(), fixture.owner.cookie.Value))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusUnauthorized, response.status)
	require.Zero(t, countManualRows(t, fixture, "SELECT COUNT(*) FROM manuals"))
}

func TestManualDuplicateCreateDoesNotHideSessionRevocation(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	payload, err := json.Marshal(map[string]interface{}{
		"name": "幂等创建", "categories": []string{"测试"}, "access_mode": "owner", "client_request_id": "duplicate-create",
	})
	require.NoError(t, err)
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, payload, nil))
	require.Equal(t, int64(1), manualRevision(created))
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals", fixture.owner, payload, nil)
	require.NoError(t, accountservice.Logout(context.Background(), fixture.owner.cookie.Value))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusUnauthorized, response.status)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manuals"))
}

func TestManualLegacyCreateRejectsSessionRevokedAfterAuthentication(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	identity, err := json.Marshal([]*model.UserIdentity{{ID: fixture.ownerID, UserName: "legacy-owner", AuthVersion: 1}})
	require.NoError(t, err)
	require.NoError(t, passportservice.GetMockData().LoadConf(string(identity)))
	t.Cleanup(func() { _ = passportservice.GetMockData().LoadConf("[]") })
	_, err = fixture.database.Exec("ALTER TABLE login_token ADD COLUMN token VARCHAR(512) NOT NULL DEFAULT ''")
	require.NoError(t, err)
	legacyToken := "legacy-manual-session-token"
	_, err = fixture.database.Exec("UPDATE login_token SET token = ? WHERE id = ?", legacyToken, fixture.ownerID)
	require.NoError(t, err)
	conf := config.Global()
	conf.IdentitySource = "config"
	config.SetGlobalConfig(conf)
	session := &manualHTTPSession{
		cookie: &http.Cookie{Name: utils.SessionCookieName, Value: legacyToken, Path: "/", Secure: true, HttpOnly: true},
		csrf:   middleware.CSRFToken(legacyToken),
	}
	payload, err := json.Marshal(map[string]interface{}{
		"name": "旧身份不得创建", "categories": []string{}, "access_mode": "owner", "client_request_id": "legacy-revoked-create",
	})
	require.NoError(t, err)
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals", session, payload, nil)
	require.NoError(t, passportservice.DeleteToken(context.Background(), legacyToken))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusUnauthorized, response.status)
	require.Zero(t, countManualRows(t, fixture, "SELECT COUNT(*) FROM manuals"))
}

func TestManualFileRejectsSessionRevokedDuringUpload(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualID := createManualForWriteGuard(t, fixture, "file-revoke-manual")
	upload, contentType := fixture.multipart(map[string]string{"title": "不得写入", "client_request_id": "revoked-file"}, "plate.png", manualPNG(t))
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, upload.Bytes(), map[string]string{"Content-Type": contentType})
	require.NoError(t, accountservice.Logout(context.Background(), fixture.owner.cookie.Value))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusUnauthorized, response.status)
	require.Zero(t, countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id = ?", manualID))
	var revision int64
	require.NoError(t, fixture.database.QueryRow("SELECT revision FROM manuals WHERE id = ?", manualID).Scan(&revision))
	require.Equal(t, int64(1), revision)
	directory := filepath.Join(fixture.storageRoot, strconv.FormatInt(fixture.ownerID, 10), manualID)
	entries, err := os.ReadDir(directory)
	if !os.IsNotExist(err) {
		require.NoError(t, err)
		require.Empty(t, entries, "rejected upload left a stored item directory")
	}
}

func TestManualDuplicateFileDoesNotHideSessionRevocation(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualID := createManualForWriteGuard(t, fixture, "duplicate-revoke-manual")
	pngData := manualPNG(t)
	first, contentType := fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "duplicate-file"}, "plate.png", pngData)
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, first, map[string]string{"Content-Type": contentType}))
	require.Equal(t, int64(2), manualRevision(created))

	replay, replayType := fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "duplicate-file"}, "plate.png", pngData)
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, replay.Bytes(), map[string]string{"Content-Type": replayType})
	require.NoError(t, accountservice.Logout(context.Background(), fixture.owner.cookie.Value))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusUnauthorized, response.status)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id = ?", manualID))
	var revision int64
	require.NoError(t, fixture.database.QueryRow("SELECT revision FROM manuals WHERE id = ?", manualID).Scan(&revision))
	require.Equal(t, int64(2), revision)
	directory := filepath.Join(fixture.storageRoot, strconv.FormatInt(fixture.ownerID, 10), manualID)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1, "duplicate retry left a second stored directory")
}

func TestManualDuplicateFileDoesNotHideConcurrentManualDeletion(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualID := createManualForWriteGuard(t, fixture, "duplicate-delete-manual")
	pngData := manualPNG(t)
	first, contentType := fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "delete-race-file"}, "plate.png", pngData)
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, first, map[string]string{"Content-Type": contentType}))
	require.Equal(t, int64(2), manualRevision(created))

	replay, replayType := fixture.multipart(map[string]string{"title": "铭牌", "client_request_id": "delete-race-file"}, "plate.png", pngData)
	body, done := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, replay.Bytes(), map[string]string{"Content-Type": replayType})
	deleted := requireManualSuccess(t, fixture.request(http.MethodDelete, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 2}, nil))
	require.Equal(t, int64(3), manualRevision(deleted))

	response := finishBlockedManualRequest(t, body, done)
	require.Equal(t, http.StatusNotFound, response.status)
	var status model.ManualStatus
	var revision int64
	require.NoError(t, fixture.database.QueryRow("SELECT status, revision FROM manuals WHERE id = ?", manualID).Scan(&status, &revision))
	require.Equal(t, model.ManualStatusDeleted, status)
	require.Equal(t, int64(3), revision)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id = ?", manualID))
	directory := filepath.Join(fixture.storageRoot, strconv.FormatInt(fixture.ownerID, 10), manualID)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed replay left a second stored directory")
}

func TestManualConcurrentItemQuotaSerializesRevision(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualID := createManualForWriteGuard(t, fixture, "concurrent-quota-manual")
	conf := config.Global()
	conf.Manuals.MaxItemsPerManual = 1
	config.SetGlobalConfig(conf)

	payloads := make([][]byte, 2)
	for index := range payloads {
		payload, err := json.Marshal(map[string]interface{}{
			"kind": "text", "title": "竞争写入", "text": "正文", "client_request_id": "concurrent-item-" + strconv.Itoa(index+1),
		})
		require.NoError(t, err)
		payloads[index] = payload
	}
	firstBody, firstDone := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/items", fixture.owner, payloads[0], nil)
	secondBody, secondDone := beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/items", fixture.owner, payloads[1], nil)
	firstBody.unblock()
	secondBody.unblock()
	responses := []manualHTTPResponse{
		finishBlockedManualRequest(t, firstBody, firstDone),
		finishBlockedManualRequest(t, secondBody, secondDone),
	}

	successes, limited := 0, 0
	for _, response := range responses {
		switch response.status {
		case http.StatusCreated:
			successes++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected concurrent response: status=%d code=%d body=%s", response.status, response.code, response.body)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id = ?", manualID))
	var revision int64
	require.NoError(t, fixture.database.QueryRow("SELECT revision FROM manuals WHERE id = ?", manualID).Scan(&revision))
	require.Equal(t, int64(2), revision)
}
