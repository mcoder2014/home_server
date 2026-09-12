package webprojects

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestUploadStorageIsolatesUsersAndPreservesExistingRelease(t *testing.T) {
	conf := testStorageConfig(t)
	for _, owner := range []string{"11", "22"} {
		artifact, err := StoreUpload(&conf, owner, "101", "201", "index.html", "", bytes.NewBufferString(owner))
		require.NoError(t, err)
		require.Equal(t, owner+"/upload/html/101/releases/201/content", artifact.StorageKey)
		content, err := os.ReadFile(filepath.Join(conf.StorageRoot, artifact.StorageKey, "index.html"))
		require.NoError(t, err)
		require.Equal(t, owner, string(content))
	}
	_, err := StoreUpload(&conf, "11", "101", "201", "index.html", "", bytes.NewBufferString("overwrite"))
	require.Error(t, err)
	content, err := os.ReadFile(filepath.Join(conf.StorageRoot, "11/upload/html/101/releases/201/content/index.html"))
	require.NoError(t, err)
	require.Equal(t, "11", string(content))
	require.NoDirExists(t, filepath.Join(conf.StorageRoot, "projects"))
	require.NoDirExists(t, filepath.Join(conf.StorageRoot, "staging"))
}

func TestUserStorageRejectsUntrustedIdentityAndSymlinkAncestors(t *testing.T) {
	conf := testStorageConfig(t)
	for _, owner := range []string{"0", "-1", "01", "../22", "1/2", "9223372036854775808"} {
		_, err := StoreUpload(&conf, owner, "101", "201", "index.html", "", bytes.NewBufferString("bad"))
		require.Error(t, err, owner)
	}
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(conf.StorageRoot, "11")))
	_, err := UserStagingRoot(&conf, 11)
	require.Error(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestReleasePathsCheckOwnerAndSupportLegacy(t *testing.T) {
	conf := testStorageConfig(t)
	for _, key := range []string{"11/upload/html/101/releases/201/content", "projects/101/releases/201/content"} {
		require.NoError(t, os.MkdirAll(filepath.Join(conf.StorageRoot, key), 0700))
		release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 11, StorageKey: key}
		root, err := ReleaseContentRoot(&conf, release)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(conf.StorageRoot, key), root)
	}
	release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 22, StorageKey: "11/upload/html/101/releases/201/content"}
	_, err := ReleaseContentRoot(&conf, release)
	require.Error(t, err)
	release.UploadedBy = 11
	require.NoError(t, os.Remove(filepath.Join(conf.StorageRoot, release.StorageKey)))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(conf.StorageRoot, release.StorageKey)))
	_, err = ReleaseContentRoot(&conf, release)
	require.Error(t, err)
}

func TestUserStorageRejectsEverySymlinkAncestor(t *testing.T) {
	for _, relative := range []string{"11/upload", "11/upload/html", "11/upload/html/.staging"} {
		t.Run(relative, func(t *testing.T) {
			conf := testStorageConfig(t)
			link := filepath.Join(conf.StorageRoot, relative)
			require.NoError(t, os.MkdirAll(filepath.Dir(link), 0700))
			outside := t.TempDir()
			require.NoError(t, os.Symlink(outside, link))
			_, err := UserStagingRoot(&conf, 11)
			require.Error(t, err)
			entries, err := os.ReadDir(outside)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestMissingVolumeCannotBeTreatedAsCleanedRelease(t *testing.T) {
	conf := testStorageConfig(t)
	release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 11, StorageKey: "11/upload/html/101/releases/201/content"}
	_, missing, err := ReleaseDirectory(&conf, release)
	require.NoError(t, err)
	require.True(t, missing)
	require.NoError(t, os.Remove(conf.StorageRoot))
	_, missing, err = ReleaseDirectory(&conf, release)
	require.ErrorIs(t, err, ErrDependency)
	require.False(t, missing)
}
