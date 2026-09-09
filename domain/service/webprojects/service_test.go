package webprojects

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/stretchr/testify/require"
)

func TestInitDisabledDoesNotRequireStorageConfiguration(t *testing.T) {
	conf := config.WebProjectsConfig{}
	require.NoError(t, Init(&conf))
}

func TestInitEnabledRequiresHTTPSOrigin(t *testing.T) {
	conf := config.WebProjectsConfig{
		Enabled:     true,
		StorageRoot: t.TempDir(),
		SiteOrigin:  "http://home.example.com",
	}
	require.ErrorContains(t, Init(&conf), "https")
}

func TestInitEnabledAppliesLimitsAndCreatesPrivateRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "web-projects")
	conf := config.WebProjectsConfig{
		Enabled:     true,
		StorageRoot: root,
		SiteOrigin:  "https://home.example.com:18443",
	}
	require.NoError(t, Init(&conf))
	require.EqualValues(t, 50<<20, conf.MaxUploadBytes)
	require.EqualValues(t, 200<<20, conf.MaxExpandedBytes)
	require.Equal(t, 5000, conf.MaxFileCount)
	for _, name := range []string{"staging", "projects"} {
		info, err := os.Stat(filepath.Join(root, name))
		require.NoError(t, err)
		require.True(t, info.IsDir())
	}
}

func TestCanReadProject(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		ownerID  int64
		userID   int64
		isMember bool
		want     bool
	}{
		{name: "public anonymous", mode: AccessModePublic, want: true},
		{name: "authenticated user", mode: AccessModeAuthenticated, userID: 2, want: true},
		{name: "authenticated anonymous", mode: AccessModeAuthenticated, want: false},
		{name: "owner", mode: AccessModeOwner, ownerID: 1, userID: 1, want: true},
		{name: "different owner", mode: AccessModeOwner, ownerID: 1, userID: 2, want: false},
		{name: "explicit member", mode: AccessModeMembers, ownerID: 1, userID: 2, isMember: true, want: true},
		{name: "owner in members mode", mode: AccessModeMembers, ownerID: 1, userID: 1, want: true},
		{name: "nonmember", mode: AccessModeMembers, ownerID: 1, userID: 2, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, CanReadProject(tt.mode, tt.ownerID, tt.userID, tt.isMember))
		})
	}
}

func TestValidateProjectInputCountsNameCharacters(t *testing.T) {
	name := strings.Repeat("项", 60)
	_, err := validateProjectInput(name, "", "valid-slug", AccessModeOwner, nil, true)
	require.NoError(t, err)
}

func TestStoreUploadNormalizesSingleHTMLToIndex(t *testing.T) {
	conf := testStorageConfig(t)
	artifact, err := StoreUpload(&conf, "101", "201", "report.html", "", bytes.NewBufferString("<h1>ok</h1>"))
	require.NoError(t, err)
	require.Equal(t, "index.html", artifact.EntryFile)
	require.Equal(t, 1, artifact.FileCount)
	content, err := os.ReadFile(filepath.Join(conf.StorageRoot, artifact.StorageKey, "index.html"))
	require.NoError(t, err)
	require.Equal(t, "<h1>ok</h1>", string(content))
}

func TestStoreUploadRejectsZIPTraversal(t *testing.T) {
	conf := testStorageConfig(t)
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	w, err := zw.Create("../escape.html")
	require.NoError(t, err)
	_, err = w.Write([]byte("escape"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	_, err = StoreUpload(&conf, "101", "201", "site.zip", "index.html", &body)
	require.ErrorContains(t, err, "path")
	_, statErr := os.Stat(filepath.Join(conf.StorageRoot, "escape.html"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestStoreUploadRejectsMissingEntryFile(t *testing.T) {
	conf := testStorageConfig(t)
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	w, err := zw.Create("assets/app.js")
	require.NoError(t, err)
	_, err = w.Write([]byte("console.log('ok')"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	_, err = StoreUpload(&conf, "101", "201", "site.zip", "index.html", &body)
	require.ErrorContains(t, err, "entry")
}

func TestStoreUploadAcceptsNormalZIPDirectoryEntries(t *testing.T) {
	conf := testStorageConfig(t)
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	_, err := zw.Create("assets/")
	require.NoError(t, err)
	w, err := zw.Create("index.html")
	require.NoError(t, err)
	_, err = w.Write([]byte("<script src=\"./assets/app.js\"></script>"))
	require.NoError(t, err)
	w, err = zw.Create("assets/app.js")
	require.NoError(t, err)
	_, err = w.Write([]byte("console.log('ok')"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	artifact, err := StoreUpload(&conf, "101", "201", "site.zip", "index.html", &body)
	require.NoError(t, err)
	require.Equal(t, 2, artifact.FileCount)
}

func TestStoreUploadEntryMustBeARegularHTMLFile(t *testing.T) {
	conf := testStorageConfig(t)
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	_, err := zw.Create("index.html/")
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	_, err = StoreUpload(&conf, "101", "201", "site.zip", "index.html", &body)
	require.ErrorContains(t, err, "entry")
}

func TestStoreUploadCountsAllZIPEntries(t *testing.T) {
	conf := testStorageConfig(t)
	conf.MaxFileCount = 1
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	_, err := zw.Create("assets/")
	require.NoError(t, err)
	w, err := zw.Create("index.html")
	require.NoError(t, err)
	_, err = w.Write([]byte("ok"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	_, err = StoreUpload(&conf, "101", "201", "site.zip", "index.html", &body)
	require.ErrorContains(t, err, "file limits")
}

func TestStoreUploadStreamsSingleHTML(t *testing.T) {
	conf := testStorageConfig(t)
	reader := &maxReadSizeReader{reader: strings.NewReader(strings.Repeat("x", 128<<10)), max: 64 << 10}
	_, err := StoreUpload(&conf, "101", "201", "page.html", "", reader)
	require.NoError(t, err)
}

func TestClassifyStorageErrorKeepsFilesystemFailureAsDependency(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "storage-root-is-a-file")
	require.NoError(t, os.WriteFile(rootFile, []byte("blocked"), 0600))
	conf := config.WebProjectsConfig{
		Enabled:           true,
		StorageRoot:       rootFile,
		MaxUploadBytes:    1024,
		MaxExpandedBytes:  1024,
		MaxFileBytes:      1024,
		MaxFileCount:      10,
		MaxDirectoryDepth: 4,
	}

	_, storageErr := StoreUpload(&conf, "101", "201", "page.html", "", bytes.NewBufferString("ok"))
	classified := classifyStorageError(storageErr)
	require.ErrorIs(t, classified, ErrDependency)
	var pathErr *os.PathError
	require.ErrorAs(t, classified, &pathErr)
}

func TestClassifyStorageErrorKeepsReaderFailureAsDependency(t *testing.T) {
	conf := testStorageConfig(t)
	readErr := errors.New("forced upload read failure")
	_, storageErr := StoreUpload(&conf, "101", "201", "page.html", "", &failingReader{err: readErr})
	classified := classifyStorageError(storageErr)
	require.ErrorIs(t, classified, ErrDependency)
	require.ErrorIs(t, classified, readErr)
}

func TestClassifyStorageErrorKeepsInvalidZIPAsUnprocessable(t *testing.T) {
	conf := testStorageConfig(t)
	_, storageErr := StoreUpload(&conf, "101", "201", "site.zip", "index.html", bytes.NewBufferString("not a zip"))
	classified := classifyStorageError(storageErr)
	require.ErrorIs(t, classified, ErrUnprocessable)
	require.NotErrorIs(t, classified, ErrDependency)
}

func TestValidateStorageIsolationRejectsWebDAVOverlap(t *testing.T) {
	root := t.TempDir()
	privateRoot := filepath.Join(root, "private")
	require.NoError(t, os.MkdirAll(privateRoot, 0700))
	require.Error(t, ValidateStorageIsolation(privateRoot, root))
	require.Error(t, ValidateStorageIsolation(root, privateRoot))
	require.NoError(t, ValidateStorageIsolation(privateRoot, filepath.Join(root, "share")))
}

func TestResolveContentPathClassifiesContentRootFailures(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing-content")
	_, err := ResolveContentPath(missingRoot, "asset.js")
	require.ErrorIs(t, err, ErrDependency)

	rootFile := filepath.Join(t.TempDir(), "content-is-a-file")
	require.NoError(t, os.WriteFile(rootFile, []byte("broken"), 0600))
	_, err = ResolveContentPath(rootFile, "asset.js")
	require.ErrorIs(t, err, ErrDependency)
}

func TestResolveContentPathKeepsMissingResourceDistinctFromRootFailure(t *testing.T) {
	contentRoot := t.TempDir()
	_, err := ResolveContentPath(contentRoot, "missing.js")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NotErrorIs(t, err, ErrDependency)

	require.NoError(t, os.WriteFile(filepath.Join(contentRoot, "index.html"), []byte("ok"), 0600))
	_, err = ResolveContentPath(contentRoot, "index.html/child")
	require.ErrorIs(t, err, syscall.ENOTDIR)
	require.NotErrorIs(t, err, ErrDependency)

	_, err = ResolveContentPath(contentRoot, "../secret")
	require.ErrorIs(t, err, ErrInvalid)
}

type maxReadSizeReader struct {
	reader *strings.Reader
	max    int
}

type failingReader struct {
	err error
}

func (r *failingReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func (r *maxReadSizeReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		return 0, errors.New("reader requested an unbounded buffer")
	}
	return r.reader.Read(p)
}

func testStorageConfig(t *testing.T) config.WebProjectsConfig {
	t.Helper()
	conf := config.WebProjectsConfig{
		Enabled:     true,
		StorageRoot: filepath.Join(t.TempDir(), "web-projects"),
		SiteOrigin:  "https://home.example.com",
	}
	require.NoError(t, Init(&conf))
	return conf
}
