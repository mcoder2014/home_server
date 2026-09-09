package webprojects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	releaseStatusDeleting      = "deleting"
	expiredProjectBatchSize    = 100
	expiredReleaseBatchSize    = 100
	maintenanceCleanupInterval = time.Hour
)

// PruneProjectReleases selects old non-current releases while the caller holds
// the project row lock. It only changes database state; disk deletion happens
// after the surrounding upload transaction commits.
func PruneProjectReleases(tx *gorm.DB, project *model.WebProject, conf *config.WebProjectsConfig, incomingBytes int64) ([]*model.WebProjectRelease, error) {
	if tx == nil || project == nil || conf == nil || incomingBytes < 0 || conf.MaxProjectBytes <= 0 || conf.MaxReleases <= 0 {
		return nil, ErrInvalid
	}
	usage, err := dal.QueryReadyWebProjectReleaseUsage(tx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: query release usage: %v", ErrDependency, err)
	}
	if usage.TotalBytes+incomingBytes <= conf.MaxProjectBytes && usage.ReleaseCount+1 <= int64(conf.MaxReleases) {
		return nil, nil
	}

	candidates, err := dal.ListOldestPrunableWebProjectReleases(tx, project.ID, project.CurrentReleaseID, int(usage.ReleaseCount))
	if err != nil {
		return nil, fmt.Errorf("%w: list releases for pruning: %v", ErrDependency, err)
	}
	remainingBytes := usage.TotalBytes
	remainingCount := usage.ReleaseCount
	retired := make([]*model.WebProjectRelease, 0, len(candidates))
	for _, release := range candidates {
		if remainingBytes+incomingBytes <= conf.MaxProjectBytes && remainingCount+1 <= int64(conf.MaxReleases) {
			break
		}
		retired = append(retired, release)
		remainingBytes -= release.TotalBytes
		if remainingBytes < 0 {
			remainingBytes = 0
		}
		remainingCount--
	}
	if remainingBytes+incomingBytes > conf.MaxProjectBytes {
		return nil, ErrTooLarge
	}
	if remainingCount+1 > int64(conf.MaxReleases) {
		return nil, ErrRateLimited
	}

	releaseIDs := make([]int64, 0, len(retired))
	for _, release := range retired {
		releaseIDs = append(releaseIDs, release.ID)
	}
	rows, err := dal.MarkWebProjectReleasesDeleting(tx, project.ID, releaseIDs, true)
	if err != nil {
		return nil, fmt.Errorf("%w: mark pruned releases: %v", ErrDependency, err)
	}
	if rows != int64(len(retired)) {
		return nil, fmt.Errorf("%w: release state changed while pruning", ErrConflict)
	}
	for _, release := range retired {
		release.Status = releaseStatusDeleting
	}
	return retired, nil
}

// RemoveRetiredReleases removes only database-confirmed deleting releases. A
// failure leaves the row in deleting so the next maintenance run can retry.
func RemoveRetiredReleases(conf *config.WebProjectsConfig, releases []*model.WebProjectRelease) error {
	if conf == nil || conf.StorageRoot == "" {
		return ErrInvalid
	}
	var result error
	for _, candidate := range releases {
		if candidate == nil || candidate.ProjectID <= 0 || candidate.ID <= 0 {
			result = errors.Join(result, fmt.Errorf("%w: invalid retired release identity", ErrInvalid))
			continue
		}
		release, err := dal.QueryDeletingWebProjectRelease(candidate.ProjectID, candidate.ID)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%w: query deleting release: %v", ErrDependency, err))
			continue
		}
		if release == nil {
			continue
		}
		releaseDir, missing, err := validatedReleaseDirectory(conf, release)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if !missing {
			if err := os.RemoveAll(releaseDir); err != nil {
				result = errors.Join(result, fmt.Errorf("remove release %d: %w", release.ID, err))
				continue
			}
		}
		deleted, err := dal.DeleteDeletingWebProjectRelease(release.ProjectID, release.ID)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%w: delete release row: %v", ErrDependency, err))
			continue
		}
		if !deleted {
			result = errors.Join(result, fmt.Errorf("%w: release %d is no longer removable", ErrConflict, release.ID))
		}
	}
	return result
}

// CleanupExpiredProjects processes a bounded set of deleted projects that
// still have release rows. Project state is checked again under a row lock so
// a concurrent restore cannot race with cleanup.
func CleanupExpiredProjects(conf *config.WebProjectsConfig) error {
	if conf == nil || conf.DeleteRetentionDays <= 0 || conf.StorageRoot == "" {
		return ErrInvalid
	}
	if !conf.Enabled {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(conf.DeleteRetentionDays) * 24 * time.Hour)
	projectIDs, err := dal.ListExpiredWebProjectIDs(cutoff, expiredProjectBatchSize)
	var result error
	processedReleaseIDs := make(map[int64]struct{})
	if err != nil {
		result = errors.Join(result, fmt.Errorf("%w: list expired projects: %v", ErrDependency, err))
	} else {
		for _, projectID := range projectIDs {
			releases, prepareErr := prepareExpiredProjectCleanup(projectID, cutoff)
			if prepareErr != nil {
				result = errors.Join(result, prepareErr)
				continue
			}
			for _, release := range releases {
				processedReleaseIDs[release.ID] = struct{}{}
			}
			if err := RemoveRetiredReleases(conf, releases); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup expired project %d: %w", projectID, err))
			}
		}
	}

	retryReleases, err := dal.ListDeletingWebProjectReleases(expiredReleaseBatchSize)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("%w: list deleting releases: %v", ErrDependency, err))
		return result
	}
	pendingRetry := make([]*model.WebProjectRelease, 0, len(retryReleases))
	for _, release := range retryReleases {
		if _, processed := processedReleaseIDs[release.ID]; !processed {
			pendingRetry = append(pendingRetry, release)
		}
	}
	if err := RemoveRetiredReleases(conf, pendingRetry); err != nil {
		result = errors.Join(result, fmt.Errorf("retry deleting releases: %w", err))
	}
	return result
}

func StartMaintenance(conf config.WebProjectsConfig) {
	if !conf.Enabled {
		return
	}
	go func() {
		if err := CleanupExpiredProjects(&conf); err != nil {
			logrus.WithError(err).Error("web project maintenance cleanup failed")
		}
		ticker := time.NewTicker(maintenanceCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			if err := CleanupExpiredProjects(&conf); err != nil {
				logrus.WithError(err).Error("web project maintenance cleanup failed")
			}
		}
	}()
}

func prepareExpiredProjectCleanup(projectID int64, cutoff time.Time) ([]*model.WebProjectRelease, error) {
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		project, err := dal.LockWebProjectByID(tx, projectID)
		if err != nil {
			return err
		}
		if project == nil || project.Status != ProjectStatusDeleted || project.DeletedAt == nil || !project.DeletedAt.Before(cutoff) {
			return nil
		}
		releases, err = dal.ListWebProjectReleasesForCleanup(tx, projectID, expiredReleaseBatchSize)
		if err != nil || len(releases) == 0 {
			return err
		}
		releaseIDs := make([]int64, 0, len(releases))
		for _, release := range releases {
			releaseIDs = append(releaseIDs, release.ID)
		}
		rows, err := dal.MarkWebProjectReleasesDeleting(tx, projectID, releaseIDs, false)
		if err != nil {
			return err
		}
		if rows != int64(len(releases)) {
			return fmt.Errorf("release state changed while preparing cleanup")
		}
		cleared, err := dal.ClearExpiredWebProjectCurrentRelease(tx, projectID, cutoff)
		if err != nil {
			return err
		}
		if !cleared {
			return fmt.Errorf("project state changed while preparing cleanup")
		}
		for _, release := range releases {
			release.Status = releaseStatusDeleting
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: prepare expired project %d: %v", ErrDependency, projectID, err)
	}
	return releases, nil
}

func validatedReleaseDirectory(conf *config.WebProjectsConfig, release *model.WebProjectRelease) (string, bool, error) {
	projectID := strconv.FormatInt(release.ProjectID, 10)
	releaseID := strconv.FormatInt(release.ID, 10)
	expectedStorageKey := filepath.ToSlash(filepath.Join("projects", projectID, "releases", releaseID, "content"))
	if release.StorageKey != expectedStorageKey {
		return "", false, fmt.Errorf("%w: unexpected storage key for release %d", ErrInvalid, release.ID)
	}
	root, err := filepath.Abs(conf.StorageRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve storage root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve storage root symlinks: %w", err)
	}
	components := []string{"projects", projectID, "releases", releaseID}
	current := root
	for _, component := range components {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return filepath.Join(root, filepath.Join(components...)), true, nil
		}
		if statErr != nil {
			return "", false, fmt.Errorf("inspect release directory: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("%w: symlink in release directory ancestry", ErrInvalid)
		}
		if !info.IsDir() {
			return "", false, fmt.Errorf("%w: release directory ancestry is not a directory", ErrInvalid)
		}
	}
	return current, false, nil
}
