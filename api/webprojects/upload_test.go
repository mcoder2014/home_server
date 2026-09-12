package webprojects

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

func TestReceiveMultipartUploadCleansTemporaryFileOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	conf := config.WebProjectsConfig{StorageRoot: root, MaxUploadBytes: 8}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "page.html")
	require.NoError(t, err)
	_, err = part.Write([]byte(strings.Repeat("x", 9)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(utils.CtxKeyLoginUseID, int64(11))
	c.Request = httptest.NewRequest("POST", "/api/web-projects/1/releases", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, _, _, err = receiveMultipartUpload(c, &conf)
	require.Error(t, err)
	matches, globErr := filepath.Glob(filepath.Join(root, "11/upload/html/.staging", "request-*.upload"))
	require.NoError(t, globErr)
	require.Empty(t, matches)
}

func TestReceiveMultipartUploadUsesAuthenticatedOwner(t *testing.T) {
	root := t.TempDir()
	conf := config.WebProjectsConfig{StorageRoot: root, MaxUploadBytes: 1024}
	for _, owner := range []int64{11, 22} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "../../other-user.html")
		require.NoError(t, err)
		_, err = part.Write([]byte("<h1>ok</h1>"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(utils.CtxKeyLoginUseID, owner)
		c.Request = httptest.NewRequest("POST", "/api/web-share/101/releases?user_id=999", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		temp, _, _, err := receiveMultipartUpload(c, &conf)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, fmt.Sprint(owner), "upload/html/.staging"), filepath.Dir(temp))
		info, err := os.Stat(temp)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		require.NoError(t, os.Remove(temp))
	}
	require.NoDirExists(t, filepath.Join(root, "999"))
}
