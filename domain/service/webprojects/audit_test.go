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

func TestAuditStorageListsOnlySafeOldUnreferencedDirectories(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := time.Unix(now.Add(-2*time.Hour).Unix(), 0)
	conf := config.WebProjectsConfig{StorageRoot: root}

	missingPath := createAuditReleaseDirectory(t, root, 101, 201, old)
	mismatchPath := createAuditReleaseDirectory(t, root, 101, 202, old)
	referencedPath := createAuditReleaseDirectory(t, root, 101, 203, old)
	recentPath := createAuditReleaseDirectory(t, root, 101, 204, now.Add(-5*time.Minute))
	symlinkPath := filepath.Join(root, "projects", "101", "releases", "206")
	require.NoError(t, os.Symlink(referencedPath, symlinkPath))
	abnormalPath := filepath.Join(root, "projects", "101", "releases", "not-generated")
	require.NoError(t, os.MkdirAll(abnormalPath, 0700))

	report, err := auditStorageAt(&conf, time.Hour, now, func(ids []int64) ([]*model.WebProjectRelease, error) {
		return []*model.WebProjectRelease{
			{ID: 202, ProjectID: 101, StorageKey: "projects/101/releases/999/content"},
			{ID: 203, ProjectID: 101, StorageKey: "projects/101/releases/203/content"},
		}, nil
	})
	require.NoError(t, err)
	require.Len(t, report.Candidates, 2)
	require.Equal(t, "101", report.Candidates[0].ProjectID)
	require.Equal(t, "201", report.Candidates[0].ReleaseID)
	require.Equal(t, "projects/101/releases/201", report.Candidates[0].RelativePath)
	require.Equal(t, "projects/101/releases/201/content", report.Candidates[0].ExpectedStorageKey)
	require.Equal(t, AuditReasonMissingDatabaseReference, report.Candidates[0].Reason)
	require.WithinDuration(t, old, report.Candidates[0].DirectoryModifyTime, time.Second)
	require.Equal(t, "101", report.Candidates[1].ProjectID)
	require.Equal(t, "202", report.Candidates[1].ReleaseID)
	require.Equal(t, "projects/101/releases/202", report.Candidates[1].RelativePath)
	require.Equal(t, "projects/101/releases/202/content", report.Candidates[1].ExpectedStorageKey)
	require.Equal(t, "projects/101/releases/999/content", report.Candidates[1].DatabaseStorageKey)
	require.Equal(t, AuditReasonStorageKeyMismatch, report.Candidates[1].Reason)
	require.WithinDuration(t, old, report.Candidates[1].DirectoryModifyTime, time.Second)
	require.Equal(t, 1, report.Stats.ReferencedReleaseDirectories)
	require.Equal(t, 1, report.Stats.SkippedRecentDirectories)
	require.Equal(t, 1, report.Stats.SkippedSymlinks)
	require.Equal(t, 1, report.Stats.SkippedAbnormalEntries)
	for _, path := range []string{missingPath, mismatchPath, referencedPath, recentPath, symlinkPath, abnormalPath} {
		_, statErr := os.Lstat(path)
		require.NoError(t, statErr, "audit must not delete or rewrite %s", path)
	}
}

func TestAuditStorageStopsOnDatabaseErrorWithoutDeleting(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	releasePath := createAuditReleaseDirectory(t, root, 301, 401, now.Add(-2*time.Hour))

	report, err := auditStorageAt(&conf, time.Hour, now, func(ids []int64) ([]*model.WebProjectRelease, error) {
		return nil, errors.New("database unavailable")
	})
	require.ErrorIs(t, err, ErrAuditDatabase)
	require.Nil(t, report, "a database failure must not be reported as an empty database")
	require.DirExists(t, releasePath)
}

func TestAuditStorageBatchesDatabaseReferenceQueries(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	conf := config.WebProjectsConfig{StorageRoot: root}
	for releaseID := int64(1); releaseID <= auditReferenceBatchSize+1; releaseID++ {
		createAuditReleaseDirectory(t, root, 501, releaseID, now.Add(-2*time.Hour))
	}

	queryCount := 0
	report, err := auditStorageAt(&conf, time.Hour, now, func(ids []int64) ([]*model.WebProjectRelease, error) {
		queryCount++
		require.LessOrEqual(t, len(ids), int(auditReferenceBatchSize))
		releases := make([]*model.WebProjectRelease, 0, len(ids))
		for _, releaseID := range ids {
			releases = append(releases, &model.WebProjectRelease{
				ID:         releaseID,
				ProjectID:  501,
				StorageKey: fmt.Sprintf("projects/501/releases/%d/content", releaseID),
			})
		}
		return releases, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, queryCount)
	require.Equal(t, 2, report.Stats.DatabaseBatches)
	require.Equal(t, int(auditReferenceBatchSize+1), report.Stats.ReferencedReleaseDirectories)
	require.Empty(t, report.Candidates)
}

func createAuditReleaseDirectory(t *testing.T, root string, projectID, releaseID int64, modifyTime time.Time) string {
	t.Helper()
	releasePath := filepath.Join(root, "projects", fmt.Sprint(projectID), "releases", fmt.Sprint(releaseID))
	require.NoError(t, os.MkdirAll(filepath.Join(releasePath, "content"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(releasePath, "content", "index.html"), []byte("must remain unread"), 0600))
	require.NoError(t, os.Chtimes(releasePath, modifyTime, modifyTime))
	return releasePath
}
