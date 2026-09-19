package manuals

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestManualPasswordProtectsDetailFilesAndListSummary(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	created := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, map[string]interface{}{
		"name": "Protected manual", "description": "hidden summary", "categories": []string{"Kitchen"}, "access_mode": "public", "client_request_id": "password-manual",
	}, nil))
	manualID := created["id"].(string)
	pngBytes := manualPNG(t)
	upload, contentType := fixture.multipart(map[string]string{"title": "hidden cover", "client_request_id": "password-image"}, "cover.png", pngBytes)
	uploaded := requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals/"+manualID+"/files", fixture.owner, upload, map[string]string{"Content-Type": contentType}))
	imageID := uploaded["item"].(map[string]interface{})["id"].(string)
	active := requireManualSuccess(t, fixture.request(http.MethodPatch, "/api/manuals/"+manualID, fixture.owner, map[string]interface{}{"revision": 2, "status": "active", "cover_item_id": imageID}, nil))
	require.Equal(t, int64(3), manualRevision(active))

	base := "/api/manuals/" + manualID
	state := requireManualSuccess(t, fixture.request(http.MethodGet, base+"/password", fixture.owner, nil, nil))
	require.Equal(t, false, state["password_protected"])
	require.Equal(t, int64(0), int64(state["version"].(float64)))
	state = requireManualSuccess(t, fixture.request(http.MethodPut, base+"/password", fixture.owner, map[string]interface{}{"password": "manual password", "version": 0}, nil))
	require.Equal(t, true, state["password_protected"])
	require.Equal(t, int64(1), int64(state["version"].(float64)))

	listed := requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals", nil, nil, nil))
	items := listed["items"].([]interface{})
	require.Len(t, items, 1)
	lockedView := items[0].(map[string]interface{})
	require.Equal(t, true, lockedView["password_protected"])
	require.Empty(t, lockedView["description"])
	require.Empty(t, lockedView["cover_url"])
	require.Nil(t, lockedView["cover"])

	contentPath := base + "/items/" + imageID + "/content"
	thumbnailPath := base + "/items/" + imageID + "/thumbnail"
	for _, request := range []struct {
		method, path string
		headers      map[string]string
	}{
		{http.MethodGet, base, nil},
		{http.MethodGet, contentPath, nil},
		{http.MethodHead, contentPath, nil},
		{http.MethodGet, contentPath, map[string]string{"Range": "bytes=0-7"}},
		{http.MethodGet, thumbnailPath, nil},
	} {
		response := fixture.request(request.method, request.path, nil, nil, request.headers)
		require.Equal(t, http.StatusForbidden, response.status, "%s %s", request.method, request.path)
		if request.method != http.MethodHead {
			require.Equal(t, 3, response.code)
		}
	}
	require.Equal(t, http.StatusOK, fixture.request(http.MethodGet, contentPath, fixture.owner, nil, nil).status)
	_, err := fixture.database.Exec("UPDATE manuals SET status = ? WHERE id = ?", model.ManualStatusDeleted, manualID)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, fixture.request(http.MethodGet, contentPath, fixture.owner, nil, nil).status)
	_, err = fixture.database.Exec("UPDATE manuals SET status = ? WHERE id = ?", model.ManualStatusActive, manualID)
	require.NoError(t, err)

	wrong := fixture.request(http.MethodPost, base+"/unlock", nil, map[string]interface{}{"password": "wrong password"}, nil)
	require.Equal(t, http.StatusForbidden, wrong.status)
	require.Equal(t, 3, wrong.code)
	unlocked := fixture.request(http.MethodPost, base+"/unlock", nil, map[string]interface{}{"password": "manual password"}, nil)
	require.Equal(t, http.StatusOK, unlocked.status)
	setCookies := unlocked.header.Values("Set-Cookie")
	require.NotEmpty(t, setCookies)
	grant := strings.SplitN(setCookies[0], ";", 2)[0]
	if !strings.HasPrefix(grant, "__Host-cq_read_manual_") {
		t.Fatalf("unexpected unlock cookie: %s", grant)
	}
	headers := map[string]string{"Cookie": grant}
	detail := requireManualSuccess(t, fixture.request(http.MethodGet, base, nil, nil, headers))
	require.Equal(t, true, detail["password_protected"])
	require.Equal(t, "hidden summary", detail["description"])
	require.NotEmpty(t, detail["items"])
	require.Equal(t, http.StatusOK, fixture.request(http.MethodGet, contentPath, nil, nil, headers).status)
	require.Equal(t, http.StatusOK, fixture.request(http.MethodHead, contentPath, nil, nil, headers).status)
	require.Equal(t, http.StatusPartialContent, fixture.request(http.MethodGet, contentPath, nil, nil, map[string]string{"Cookie": grant, "Range": "bytes=0-7"}).status)
	require.Equal(t, http.StatusOK, fixture.request(http.MethodGet, thumbnailPath, nil, nil, headers).status)

	state = requireManualSuccess(t, fixture.request(http.MethodPut, base+"/password", fixture.owner, map[string]interface{}{"password": "replacement password", "version": 1}, nil))
	require.Equal(t, int64(2), int64(state["version"].(float64)))
	require.Equal(t, http.StatusForbidden, fixture.request(http.MethodGet, contentPath, nil, nil, headers).status)
	state = requireManualSuccess(t, fixture.request(http.MethodPut, base+"/password", fixture.owner, map[string]interface{}{"password": "", "version": 2}, nil))
	require.Equal(t, int64(3), int64(state["version"].(float64)))
	require.Equal(t, false, state["password_protected"])
	require.Equal(t, http.StatusOK, fixture.request(http.MethodGet, contentPath, nil, nil, nil).status)

	if response := fixture.request(http.MethodPut, base+"/password", fixture.owner, map[string]interface{}{"password": strings.Repeat("界", 25), "version": 3}, nil); response.status != http.StatusBadRequest {
		t.Fatalf("password above bcrypt byte limit accepted: status=%d code=%d", response.status, response.code)
	}
	if response := fixture.request(http.MethodPut, base+"/password", fixture.owner, map[string]interface{}{"password": "12345678", "version": 1}, nil); response.status != http.StatusConflict {
		t.Fatalf("stale password version accepted: status=%d code=%d", response.status, response.code)
	}
}
