package webprojects

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestServeReleaseDownloadBuildsCompleteZIPBeforeSuccess(t *testing.T) {
	conf, release, contentRoot := downloadFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(contentRoot, "index.html"), []byte("index"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(contentRoot, "assets"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(contentRoot, "assets", "app.js"), []byte("script"), 0600))
	release.FileCount = 2
	release.TotalBytes = int64(len("index") + len("script"))

	recorder := serveDownloadForTest(&conf, release)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/zip", recorder.Header().Get("Content-Type"))
	zr, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 2)
	contents := make(map[string]string, len(zr.File))
	for _, file := range zr.File {
		reader, openErr := file.Open()
		require.NoError(t, openErr)
		data, readErr := io.ReadAll(reader)
		require.NoError(t, readErr)
		require.NoError(t, reader.Close())
		contents[file.Name] = string(data)
	}
	require.Equal(t, "index", contents["index.html"])
	require.Equal(t, "script", contents["assets/app.js"])
	require.Empty(t, matchingDownloadTemps(t, conf.StorageRoot))
}

func TestServeReleaseDownloadDoesNotReturnSuccessForMissingRootOrEntry(t *testing.T) {
	conf, release, contentRoot := downloadFixture(t)
	require.NoError(t, os.RemoveAll(filepath.Dir(contentRoot)))
	recorder := serveDownloadForTest(&conf, release)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, matchingDownloadTemps(t, conf.StorageRoot))

	conf, release, contentRoot = downloadFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(contentRoot, "asset.js"), []byte("asset"), 0600))
	release.FileCount = 1
	release.TotalBytes = int64(len("asset"))
	recorder = serveDownloadForTest(&conf, release)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, matchingDownloadTemps(t, conf.StorageRoot))
}

func TestServeReleaseDownloadDoesNotReturnSuccessOnFileReadFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied fixture requires a non-root test process")
	}
	conf, release, contentRoot := downloadFixture(t)
	entryPath := filepath.Join(contentRoot, "index.html")
	require.NoError(t, os.WriteFile(entryPath, []byte("index"), 0600))
	brokenPath := filepath.Join(contentRoot, "broken.js")
	require.NoError(t, os.WriteFile(brokenPath, []byte("secret"), 0600))
	require.NoError(t, os.Chmod(brokenPath, 0000))
	t.Cleanup(func() { _ = os.Chmod(brokenPath, 0600) })
	release.FileCount = 2
	release.TotalBytes = int64(len("index") + len("secret"))

	recorder := serveDownloadForTest(&conf, release)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, matchingDownloadTemps(t, conf.StorageRoot))
}

func serveDownloadForTest(conf *config.WebProjectsConfig, release *model.WebProjectRelease) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/web-projects/1/releases/2/download", nil)
	serveReleaseDownload(c, conf, release)
	return recorder
}

func downloadFixture(t *testing.T) (config.WebProjectsConfig, *model.WebProjectRelease, string) {
	t.Helper()
	root := t.TempDir()
	projectID, releaseID := int64(101), int64(201)
	storageKey := filepath.ToSlash(filepath.Join("projects", fmt.Sprint(projectID), "releases", fmt.Sprint(releaseID), "content"))
	contentRoot := filepath.Join(root, filepath.FromSlash(storageKey))
	require.NoError(t, os.MkdirAll(contentRoot, 0700))
	return config.WebProjectsConfig{Enabled: true, StorageRoot: root}, &model.WebProjectRelease{ID: releaseID, ProjectID: projectID, UploadedBy: 11, StorageKey: storageKey, EntryFile: "index.html"}, contentRoot
}

func matchingDownloadTemps(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "11", "upload", "html", ".staging", "download-*.zip"))
	require.NoError(t, err)
	return matches
}
