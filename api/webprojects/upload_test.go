package webprojects

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/stretchr/testify/require"
)

func TestReceiveMultipartUploadCleansTemporaryFileOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "staging"), 0700))
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
	c.Request = httptest.NewRequest("POST", "/api/web-projects/1/releases", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, _, _, err = receiveMultipartUpload(c, &conf)
	require.Error(t, err)
	matches, globErr := filepath.Glob(filepath.Join(root, "staging", "request-*.upload"))
	require.NoError(t, globErr)
	require.Empty(t, matches)
}
