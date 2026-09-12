package webprojects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestAuditStorageVerifiesOwnerForLegacyAndCanonicalDirectories(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := time.Unix(now.Add(-2*time.Hour).Unix(), 0)
	conf := config.WebProjectsConfig{StorageRoot: root}

	legacyPath := createLegacyAuditReleaseDirectory(t, root, 101, 201, old)
	canonicalPath := createCanonicalAuditReleaseDirectory(t, root, 71, 101, 202, old)
	wrongOwnerPath := createCanonicalAuditReleaseDirectory(t, root, 72, 101, 203, old)

	releaseQueries := 0
	projectQueries := 0
	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			releaseQueries++
			return []*model.WebProjectRelease{
				{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"},
				{ID: 202, ProjectID: 101, UploadedBy: 71, StorageKey: "71/upload/html/101/releases/202/content"},
				{ID: 203, ProjectID: 101, UploadedBy: 71, StorageKey: "71/upload/html/101/releases/203/content"},
			}, nil
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			projectQueries++
			require.Equal(t, []int64{101}, ids)
			return []*model.WebProject{{ID: 101, OwnerUserID: 71}}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, releaseQueries)
	require.Equal(t, 1, projectQueries)
	require.Equal(t, 2, report.Stats.ReferencedReleaseDirectories)
	require.Len(t, report.Candidates, 1)
	require.Equal(t, "71", report.Candidates[0].OwnerID)
	require.Equal(t, "72", report.Candidates[0].DirectoryOwnerID)
	require.Equal(t, "101", report.Candidates[0].ProjectID)
	require.Equal(t, "203", report.Candidates[0].ReleaseID)
	require.Equal(t, AuditReasonDirectoryOwnerMismatch, report.Candidates[0].Reason)
	for _, path := range []string{legacyPath, canonicalPath, wrongOwnerPath} {
		require.DirExists(t, path)
	}
}

func TestAuditStorageReportsReferenceAndOwnershipFailures(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	conf := config.WebProjectsConfig{StorageRoot: root}
	createLegacyAuditReleaseDirectory(t, root, 101, 201, old)
	createLegacyAuditReleaseDirectory(t, root, 101, 202, old)
	createLegacyAuditReleaseDirectory(t, root, 101, 203, old)
	createLegacyAuditReleaseDirectory(t, root, 101, 204, old)

	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			return []*model.WebProjectRelease{
				{ID: 202, ProjectID: 999, UploadedBy: 71, StorageKey: "projects/101/releases/202/content"},
				{ID: 203, ProjectID: 101, UploadedBy: 72, StorageKey: "projects/101/releases/203/content"},
				{ID: 204, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/999/content"},
			}, nil
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			return []*model.WebProject{{ID: 101, OwnerUserID: 71}, {ID: 999, OwnerUserID: 71}}, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, report.Candidates, 4)
	require.Equal(t, AuditReasonMissingDatabaseReference, report.Candidates[0].Reason)
	require.Equal(t, AuditReasonProjectMismatch, report.Candidates[1].Reason)
	require.Equal(t, AuditReasonOwnerMismatch, report.Candidates[2].Reason)
	require.Equal(t, AuditReasonStorageKeyMismatch, report.Candidates[3].Reason)
	require.Equal(t, "71", report.Candidates[3].OwnerID)
}

func TestAuditStorageSkipsRecentAbnormalAndSymlinkDirectories(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	recentPath := createLegacyAuditReleaseDirectory(t, root, 101, 201, now.Add(-5*time.Minute))
	symlinkTarget := createLegacyAuditReleaseDirectory(t, root, 101, 202, now.Add(-2*time.Hour))
	symlinkPath := filepath.Join(root, "projects", "101", "releases", "203")
	require.NoError(t, os.Symlink(symlinkTarget, symlinkPath))
	abnormalPath := filepath.Join(root, "projects", "101", "releases", "not-generated")
	require.NoError(t, os.MkdirAll(abnormalPath, 0700))

	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			return []*model.WebProjectRelease{{ID: 202, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/202/content"}}, nil
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			return []*model.WebProject{{ID: 101, OwnerUserID: 71}}, nil
		},
	})
	require.NoError(t, err)
	require.Empty(t, report.Candidates)
	require.Equal(t, 1, report.Stats.ReferencedReleaseDirectories)
	require.Equal(t, 1, report.Stats.SkippedRecentDirectories)
	require.Equal(t, 1, report.Stats.SkippedSymlinks)
	require.Equal(t, 1, report.Stats.SkippedAbnormalEntries)
	require.DirExists(t, recentPath)
	require.DirExists(t, symlinkTarget)
}

func TestAuditStorageDoesNotFollowOwnerUploadSymlink(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	createCanonicalAuditReleaseDirectory(t, root, 71, 101, 201, now.Add(-2*time.Hour))
	require.NoError(t, os.Mkdir(filepath.Join(root, "72"), 0700))
	require.NoError(t, os.Symlink(filepath.Join(root, "71", "upload"), filepath.Join(root, "72", "upload")))

	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			require.Equal(t, []int64{201}, ids, "symlinked owner tree must not be scanned twice")
			return []*model.WebProjectRelease{{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "71/upload/html/101/releases/201/content"}}, nil
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			return []*model.WebProject{{ID: 101, OwnerUserID: 71}}, nil
		},
	})
	require.NoError(t, err)
	require.Empty(t, report.Candidates)
	require.Equal(t, 1, report.Stats.ReferencedReleaseDirectories)
	require.Equal(t, 1, report.Stats.SkippedSymlinks)
}

func TestAuditStorageStopsOnDatabaseErrorWithoutDeleting(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	releasePath := createLegacyAuditReleaseDirectory(t, root, 301, 401, now.Add(-2*time.Hour))

	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			return nil, errors.New("database unavailable")
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			return nil, errors.New("must not be reached")
		},
	})
	require.ErrorIs(t, err, ErrAuditDatabase)
	require.Nil(t, report)
	require.DirExists(t, releasePath)
}

func TestAuditStorageBatchesReleaseAndProjectQueries(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	for id := int64(1); id <= auditReferenceBatchSize+1; id++ {
		createLegacyAuditReleaseDirectory(t, root, id, id, now.Add(-2*time.Hour))
	}

	releaseQueryCount := 0
	projectQueryCount := 0
	report, err := auditStorageAt(&conf, time.Hour, now, auditReferenceQueries{
		queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
			releaseQueryCount++
			require.LessOrEqual(t, len(ids), int(auditReferenceBatchSize))
			releases := make([]*model.WebProjectRelease, 0, len(ids))
			for _, id := range ids {
				releases = append(releases, &model.WebProjectRelease{ID: id, ProjectID: id, UploadedBy: id, StorageKey: fmt.Sprintf("projects/%d/releases/%d/content", id, id)})
			}
			return releases, nil
		},
		queryProjects: func(ids []int64) ([]*model.WebProject, error) {
			projectQueryCount++
			require.LessOrEqual(t, len(ids), int(auditReferenceBatchSize))
			projects := make([]*model.WebProject, 0, len(ids))
			for _, id := range ids {
				projects = append(projects, &model.WebProject{ID: id, OwnerUserID: id})
			}
			return projects, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, releaseQueryCount)
	require.Equal(t, 2, projectQueryCount)
	require.Equal(t, 4, report.Stats.DatabaseBatches)
	require.Equal(t, int(auditReferenceBatchSize+1), report.Stats.ReferencedReleaseDirectories)
	require.Empty(t, report.Candidates)
}

func createLegacyAuditReleaseDirectory(t *testing.T, root string, projectID, releaseID int64, modifyTime time.Time) string {
	t.Helper()
	return createAuditReleaseDirectoryAt(t, root, filepath.Join("projects", fmt.Sprint(projectID), "releases", fmt.Sprint(releaseID)), modifyTime)
}

func createCanonicalAuditReleaseDirectory(t *testing.T, root string, ownerID, projectID, releaseID int64, modifyTime time.Time) string {
	t.Helper()
	return createAuditReleaseDirectoryAt(t, root, filepath.Join(fmt.Sprint(ownerID), "upload", "html", fmt.Sprint(projectID), "releases", fmt.Sprint(releaseID)), modifyTime)
}

func createAuditReleaseDirectoryAt(t *testing.T, root, relativePath string, modifyTime time.Time) string {
	t.Helper()
	releasePath := filepath.Join(root, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Join(releasePath, "content"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(releasePath, "content", "index.html"), []byte("must remain unread"), 0600))
	require.NoError(t, os.Chtimes(releasePath, modifyTime, modifyTime))
	return releasePath
}
