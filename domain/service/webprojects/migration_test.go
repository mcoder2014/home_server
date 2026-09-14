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

// TestMigrateLegacyStorageDryRunDoesNotChangeFilesOrDatabase 验证预览模式只生成迁移计划，不创建目标目录、恢复日志或修改存储键。
func TestMigrateLegacyStorageDryRunDoesNotChangeFilesOrDatabase(t *testing.T) {
	root := t.TempDir()
	conf := config.WebProjectsConfig{StorageRoot: root}
	source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
	release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
	queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
		t.Fatal("dry-run must not update the database")
		return false, nil
	})

	report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, false, time.Now(), queries)
	require.NoError(t, err)
	require.False(t, report.Apply)
	require.Len(t, report.Actions, 1)
	require.Equal(t, MigrationStatusPlanned, report.Actions[0].Status)
	require.Equal(t, "71", report.Actions[0].OwnerID)
	require.DirExists(t, source)
	require.NoDirExists(t, filepath.Join(root, "71", "upload", "html", "101", "releases", "201"))
	require.NoDirExists(t, filepath.Join(root, "71", "upload", "html", ".migration"))
	require.Equal(t, "projects/101/releases/201/content", release.StorageKey)
}

// TestMigrateLegacyStorageAppliesRenameAndStorageKeyCAS 核对旧目录移动到所有者目录、引用按旧键更新，并在完成后移除恢复日志。
func TestMigrateLegacyStorageAppliesRenameAndStorageKeyCAS(t *testing.T) {
	root := t.TempDir()
	conf := config.WebProjectsConfig{StorageRoot: root}
	source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
	release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
	casCalls := 0
	queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(releaseID, projectID, uploadedBy int64, oldKey, newKey string) (bool, error) {
		casCalls++
		require.Equal(t, int64(201), releaseID)
		require.Equal(t, int64(101), projectID)
		require.Equal(t, int64(71), uploadedBy)
		require.Equal(t, "projects/101/releases/201/content", oldKey)
		require.Equal(t, "71/upload/html/101/releases/201/content", newKey)
		release.StorageKey = newKey
		return true, nil
	})

	report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), queries)
	require.NoError(t, err)
	require.True(t, report.Apply)
	require.Equal(t, 1, casCalls)
	require.Len(t, report.Actions, 1)
	require.Equal(t, MigrationStatusMigrated, report.Actions[0].Status)
	require.NoDirExists(t, source)
	target := filepath.Join(root, "71", "upload", "html", "101", "releases", "201")
	require.FileExists(t, filepath.Join(target, "content", "index.html"))
	entries, readErr := os.ReadDir(filepath.Join(root, "71", "upload", "html", ".migration"))
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestMigrateLegacyStorageRejectsOwnerMismatchBeforeChangingFiles(t *testing.T) {
	root := t.TempDir()
	conf := config.WebProjectsConfig{StorageRoot: root}
	source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
	release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 72, StorageKey: "projects/101/releases/201/content"}
	queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
		t.Fatal("rejected migration must not update the database")
		return false, nil
	})

	report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), queries)
	require.ErrorIs(t, err, ErrStorageMigrationRejected)
	require.Len(t, report.Actions, 1)
	require.Equal(t, MigrationStatusRejected, report.Actions[0].Status)
	require.Equal(t, AuditReasonOwnerMismatch, report.Actions[0].Reason)
	require.DirExists(t, source)
	require.NoDirExists(t, filepath.Join(root, "71"))
}

// TestMigrateLegacyStorageRejectsTargetCollisionAndSymlink 验证目标冲突、版本内部链接和目标祖先链接均在文件或数据库变更前被拒绝。
func TestMigrateLegacyStorageRejectsTargetCollisionAndSymlink(t *testing.T) {
	t.Run("target collision", func(t *testing.T) {
		root := t.TempDir()
		conf := config.WebProjectsConfig{StorageRoot: root}
		source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
		target := createCanonicalAuditReleaseDirectory(t, root, 71, 101, 201, time.Now())
		release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
		queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
			t.Fatal("collision must not update the database")
			return false, nil
		})

		report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), queries)
		require.ErrorIs(t, err, ErrStorageMigrationRejected)
		require.Equal(t, MigrationReasonTargetCollision, report.Actions[0].Reason)
		require.DirExists(t, source)
		require.DirExists(t, target)
	})

	t.Run("symlink inside release", func(t *testing.T) {
		root := t.TempDir()
		conf := config.WebProjectsConfig{StorageRoot: root}
		source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
		require.NoError(t, os.Symlink(filepath.Join(root, "outside"), filepath.Join(source, "content", "escape")))
		release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
		queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
			t.Fatal("symlink must not update the database")
			return false, nil
		})

		report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), queries)
		require.ErrorIs(t, err, ErrStorageMigrationRejected)
		require.Equal(t, MigrationReasonSymlink, report.Actions[0].Reason)
		require.DirExists(t, source)
		require.NoDirExists(t, filepath.Join(root, "71"))
	})

	t.Run("owner upload ancestry symlink", func(t *testing.T) {
		root := t.TempDir()
		conf := config.WebProjectsConfig{StorageRoot: root}
		source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
		require.NoError(t, os.MkdirAll(filepath.Join(root, "72", "upload", "html"), 0700))
		require.NoError(t, os.Mkdir(filepath.Join(root, "71"), 0700))
		require.NoError(t, os.Symlink(filepath.Join(root, "72", "upload"), filepath.Join(root, "71", "upload")))
		release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
		queries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
			t.Fatal("symlinked owner tree must not update the database")
			return false, nil
		})

		report, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), queries)
		require.ErrorIs(t, err, ErrStorageMigrationRejected)
		require.Equal(t, MigrationReasonSymlink, report.Actions[0].Reason)
		require.DirExists(t, source)
		require.NoDirExists(t, filepath.Join(root, "72", "upload", "html", "101"))
	})
}

// TestMigrateLegacyStorageResumesAfterUncertainDatabaseResult 分别模拟数据库已提交和未提交却返回错误，核对重试能保留文件并完成恢复。
func TestMigrateLegacyStorageResumesAfterUncertainDatabaseResult(t *testing.T) {
	for _, committed := range []bool{false, true} {
		// 第一次迁移在目录移动后返回未知结果，第二次依据数据库状态继续，避免重复更新已提交的引用。
		t.Run(fmt.Sprintf("database_committed_%t", committed), func(t *testing.T) {
			root := t.TempDir()
			conf := config.WebProjectsConfig{StorageRoot: root}
			source := createLegacyAuditReleaseDirectory(t, root, 101, 201, time.Now())
			release := &model.WebProjectRelease{ID: 201, ProjectID: 101, UploadedBy: 71, StorageKey: "projects/101/releases/201/content"}
			databaseUnavailable := errors.New("database result unknown")
			firstQueries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, _ string) (bool, error) {
				if committed {
					release.StorageKey = "71/upload/html/101/releases/201/content"
				}
				return false, databaseUnavailable
			})

			firstReport, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), firstQueries)
			require.ErrorIs(t, err, databaseUnavailable)
			require.Equal(t, MigrationStatusPendingDatabase, firstReport.Actions[0].Status)
			require.NoDirExists(t, source)
			target := filepath.Join(root, "71", "upload", "html", "101", "releases", "201")
			require.DirExists(t, target)
			journalDir := filepath.Join(root, "71", "upload", "html", ".migration")
			entries, readErr := os.ReadDir(journalDir)
			require.NoError(t, readErr)
			require.Len(t, entries, 1)

			secondQueries := storageMigrationQueriesForTest(t, release, &model.WebProject{ID: 101, OwnerUserID: 71}, func(_, _, _ int64, _, newKey string) (bool, error) {
				require.False(t, committed, "a committed but unacknowledged CAS must not be repeated")
				release.StorageKey = newKey
				return true, nil
			})
			secondReport, err := migrateLegacyReleaseStorageAt(&conf, 101, 201, true, time.Now(), secondQueries)
			require.NoError(t, err)
			require.Equal(t, MigrationStatusMigrated, secondReport.Actions[0].Status)
			require.DirExists(t, target)
			entries, readErr = os.ReadDir(journalDir)
			require.NoError(t, readErr)
			require.Empty(t, entries)
		})
	}
}

// storageMigrationQueriesForTest 注入仅匹配指定版本与项目的内存查询，并将引用更新结果交由各场景控制。
func storageMigrationQueriesForTest(
	t *testing.T,
	release *model.WebProjectRelease,
	project *model.WebProject,
	compareAndSwap func(int64, int64, int64, string, string) (bool, error),
) storageMigrationQueries {
	t.Helper()
	return storageMigrationQueries{
		auditReferenceQueries: auditReferenceQueries{
			queryReleases: func(ids []int64) ([]*model.WebProjectRelease, error) {
				if len(ids) == 1 && ids[0] == release.ID {
					copy := *release
					return []*model.WebProjectRelease{&copy}, nil
				}
				return nil, nil
			},
			queryProjects: func(ids []int64) ([]*model.WebProject, error) {
				if len(ids) == 1 && ids[0] == project.ID {
					copy := *project
					return []*model.WebProject{&copy}, nil
				}
				return nil, nil
			},
		},
		compareAndSwapStorageKey: compareAndSwap,
	}
}
