package webprojects

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var (
	cleanupDBOnce    sync.Once
	cleanupDBInitErr error
	cleanupIDCounter int64
)

func TestRemoveRetiredReleasesDeletesDirectoryThenDatabaseRow(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, model.WebProjectReleaseDeleting, 10, time.Now())
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})
	releaseDir := createCleanupReleaseDirectory(t, conf.StorageRoot, project.ID, release.ID)

	require.NoError(t, RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release}))
	_, statErr := os.Stat(releaseDir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	require.Empty(t, queryCleanupReleaseStatus(t, database, release.ID))
}

// TestRemoveRetiredReleasesRejectsSymlinkAncestorAndRetries 验证祖先符号链接阻止删除并保留数据库状态，修正目录后可以重试清理。
func TestRemoveRetiredReleasesRejectsSymlinkAncestorAndRetries(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, model.WebProjectReleaseDeleting, 10, time.Now())
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})

	outsideProject := filepath.Join(t.TempDir(), "outside-project")
	outsideRelease := filepath.Join(outsideProject, "releases", fmt.Sprint(release.ID))
	require.NoError(t, os.MkdirAll(outsideRelease, 0700))
	marker := filepath.Join(outsideRelease, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("keep"), 0600))
	projectsDir := filepath.Join(conf.StorageRoot, "projects")
	require.NoError(t, os.MkdirAll(projectsDir, 0700))
	projectLink := filepath.Join(projectsDir, fmt.Sprint(project.ID))
	require.NoError(t, os.Symlink(outsideProject, projectLink))

	err := RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release})
	require.Error(t, err)
	require.FileExists(t, marker)
	require.Equal(t, model.WebProjectReleaseDeleting, queryCleanupReleaseStatus(t, database, release.ID))

	require.NoError(t, os.Remove(projectLink))
	createCleanupReleaseDirectory(t, conf.StorageRoot, project.ID, release.ID)
	require.NoError(t, RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release}))
	require.Empty(t, queryCleanupReleaseStatus(t, database, release.ID))
}

func TestRemoveRetiredReleasesRejectsUnexpectedStorageKey(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, model.WebProjectReleaseDeleting, 10, time.Now())
	release.StorageKey = "projects/wrong/releases/wrong/content"
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})
	releaseDir := createCleanupReleaseDirectory(t, conf.StorageRoot, project.ID, release.ID)

	err := RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release})
	require.Error(t, err)
	require.DirExists(t, releaseDir)
	require.Equal(t, model.WebProjectReleaseDeleting, queryCleanupReleaseStatus(t, database, release.ID))
}

func TestRemoveRetiredReleasesTreatsMissingDirectoryAsCleaned(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, model.WebProjectReleaseDeleting, 10, time.Now())
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})

	require.NoError(t, RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release}))
	require.Empty(t, queryCleanupReleaseStatus(t, database, release.ID))
}

// TestCleanupExpiredProjectsOnlyRemovesExpiredDeletedProjects 验证仅清理已过保留期的删除项目，并通过修订号阻止旧快照恢复。
func TestCleanupExpiredProjectsOnlyRemovesExpiredDeletedProjects(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	conf.DeleteRetentionDays = 7
	baseID := nextCleanupTestID()
	expiredAt := time.Now().Add(-8 * 24 * time.Hour)
	freshAt := time.Now().Add(-6 * 24 * time.Hour)

	expiredReleaseID := baseID + 1
	expired := newCleanupProject(baseID, ProjectStatusDeleted, &expiredReleaseID, &expiredAt)
	freshReleaseID := baseID + 11
	fresh := newCleanupProject(baseID+10, ProjectStatusDeleted, &freshReleaseID, &freshAt)
	restoredReleaseID := baseID + 21
	restored := newCleanupProject(baseID+20, ProjectStatusDisabled, &restoredReleaseID, nil)
	seedCleanupRecords(t, database, expired, []*model.WebProjectRelease{newCleanupRelease(expiredReleaseID, expired.ID, ReleaseStatusReady, 10, time.Now())})
	seedCleanupRecords(t, database, fresh, []*model.WebProjectRelease{newCleanupRelease(freshReleaseID, fresh.ID, ReleaseStatusReady, 10, time.Now())})
	seedCleanupRecords(t, database, restored, []*model.WebProjectRelease{newCleanupRelease(restoredReleaseID, restored.ID, ReleaseStatusReady, 10, time.Now())})
	expiredDir := createCleanupReleaseDirectory(t, conf.StorageRoot, expired.ID, expiredReleaseID)
	freshDir := createCleanupReleaseDirectory(t, conf.StorageRoot, fresh.ID, freshReleaseID)
	restoredDir := createCleanupReleaseDirectory(t, conf.StorageRoot, restored.ID, restoredReleaseID)

	require.NoError(t, CleanupExpiredProjects(&conf))
	_, statErr := os.Stat(expiredDir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	require.Empty(t, queryCleanupReleaseStatus(t, database, expiredReleaseID))
	require.Equal(t, ReleaseStatusReady, queryCleanupReleaseStatus(t, database, freshReleaseID))
	require.Equal(t, ReleaseStatusReady, queryCleanupReleaseStatus(t, database, restoredReleaseID))
	require.DirExists(t, freshDir)
	require.DirExists(t, restoredDir)

	var currentReleaseID *int64
	require.NoError(t, database.Table(dal.WebProjectTable).Select("current_release_id").Where("id = ?", expired.ID).Scan(&currentReleaseID).Error)
	require.Nil(t, currentReleaseID)
	updated, err := dal.UpdateProjectFields(database, expired.OwnerUserID, expired.ID, expired.Revision, map[string]interface{}{"status": ProjectStatusDisabled, "deleted_at": nil})
	require.NoError(t, err)
	require.False(t, updated, "cleanup must invalidate a restore that read the old project revision")
}

func TestCleanupExpiredProjectsRetriesDeletingReleaseForActiveProject(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, releaseStatusDeleting, 10, time.Now())
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})
	releaseDir := createCleanupReleaseDirectory(t, conf.StorageRoot, project.ID, release.ID)

	require.NoError(t, CleanupExpiredProjects(&conf))
	_, statErr := os.Stat(releaseDir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	require.Empty(t, queryCleanupReleaseStatus(t, database, release.ID))
}

func requireCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_CLEANUP_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_CLEANUP_TEST_DSN is required for cleanup integration tests")
	}
	cleanupDBOnce.Do(func() {
		cleanupDBInitErr = db.InitDatabase(dsn)
	})
	require.NoError(t, cleanupDBInitErr)
	return db.MasterDB()
}

func nextCleanupTestID() int64 {
	return time.Now().UnixNano()/1000 + atomic.AddInt64(&cleanupIDCounter, 100)
}

func newCleanupProject(id int64, status model.WebProjectStatus, currentReleaseID *int64, deletedAt *time.Time) *model.WebProject {
	now := time.Now()
	return &model.WebProject{ID: id, OwnerUserID: id, Name: "cleanup-test", Description: "cleanup-test", Slug: fmt.Sprintf("cleanup-%d", id), AccessMode: AccessModeOwner, Status: status, CurrentReleaseID: currentReleaseID, Revision: 1, DeletedAt: deletedAt, CreateTime: now, UpdateTime: now}
}

func newCleanupRelease(id, projectID int64, status model.WebProjectReleaseStatus, totalBytes int64, createdAt time.Time) *model.WebProjectRelease {
	storageKey := filepath.ToSlash(filepath.Join("projects", fmt.Sprint(projectID), "releases", fmt.Sprint(id), "content"))
	return &model.WebProjectRelease{ID: id, ProjectID: projectID, UploadedBy: projectID, StorageKey: storageKey, Status: status, EntryFile: "index.html", SHA256: fmt.Sprintf("%064x", id), FileCount: 1, TotalBytes: totalBytes, CreateTime: createdAt, UpdateTime: createdAt}
}

func seedCleanupRecords(t *testing.T, database *gorm.DB, project *model.WebProject, releases []*model.WebProjectRelease) {
	t.Helper()
	require.NoError(t, database.Table(dal.WebProjectTable).Create(project).Error)
	if len(releases) > 0 {
		require.NoError(t, database.Table(dal.WebProjectReleaseTable).Create(releases).Error)
	}
	t.Cleanup(func() {
		database.Table(dal.WebProjectReleaseTable).Where("project_id = ?", project.ID).Delete(&model.WebProjectRelease{})
		database.Table(dal.WebProjectTable).Where("id = ?", project.ID).Delete(&model.WebProject{})
	})
}

func cleanupStorageConfig(t *testing.T) config.WebProjectsConfig {
	t.Helper()
	root := filepath.Join(t.TempDir(), "web-projects")
	require.NoError(t, os.MkdirAll(root, 0700))
	return config.WebProjectsConfig{Enabled: true, StorageRoot: root, MaxProjectBytes: 1 << 20, MaxReleases: 10, DeleteRetentionDays: 7}
}

func createCleanupReleaseDirectory(t *testing.T, root string, projectID, releaseID int64) string {
	t.Helper()
	releaseDir := filepath.Join(root, "projects", fmt.Sprint(projectID), "releases", fmt.Sprint(releaseID))
	require.NoError(t, os.MkdirAll(filepath.Join(releaseDir, "content"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "content", "index.html"), []byte("ok"), 0600))
	return releaseDir
}

func queryCleanupReleaseStatus(t *testing.T, database *gorm.DB, releaseID int64) model.WebProjectReleaseStatus {
	t.Helper()
	var status model.WebProjectReleaseStatus
	err := database.Table(dal.WebProjectReleaseTable).Select("status").Where("id = ?", releaseID).Scan(&status).Error
	require.NoError(t, err)
	return status
}

// TestRetiredReleaseCleanupChecksOwnerForCanonicalStorage 确认发布所有权不一致时保留文件和记录，修复元数据后才允许删除。
func TestRetiredReleaseCleanupChecksOwnerForCanonicalStorage(t *testing.T) {
	database := requireCleanupTestDB(t)
	conf := cleanupStorageConfig(t)
	baseID := nextCleanupTestID()
	project := newCleanupProject(baseID, ProjectStatusDisabled, nil, nil)
	release := newCleanupRelease(baseID+1, project.ID, model.WebProjectReleaseDeleting, 10, time.Now())
	key, err := ReleaseStorageKey(project.OwnerUserID, project.ID, release.ID)
	require.NoError(t, err)
	release.StorageKey = key
	seedCleanupRecords(t, database, project, []*model.WebProjectRelease{release})
	content := filepath.Join(conf.StorageRoot, key)
	require.NoError(t, os.MkdirAll(content, 0700))
	marker := filepath.Join(content, "index.html")
	require.NoError(t, os.WriteFile(marker, []byte("owner content"), 0600))

	// Corrupt owner metadata must never erase another owner's directory.
	require.NoError(t, database.Table(dal.WebProjectReleaseTable).Where("id = ?", release.ID).Update("uploaded_by", baseID+99).Error)
	require.Error(t, RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release}))
	require.FileExists(t, marker)
	require.Equal(t, model.WebProjectReleaseDeleting, queryCleanupReleaseStatus(t, database, release.ID))
	require.NoError(t, database.Table(dal.WebProjectReleaseTable).Where("id = ?", release.ID).Update("uploaded_by", project.OwnerUserID).Error)
	require.NoError(t, RemoveRetiredReleases(&conf, []*model.WebProjectRelease{release}))
	require.NoDirExists(t, filepath.Dir(content))
	require.Empty(t, queryCleanupReleaseStatus(t, database, release.ID))
}
