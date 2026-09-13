package webprojects

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/webprojects"
	"github.com/sirupsen/logrus"
)

const (
	releaseStatusDeleting      = model.WebProjectReleaseDeleting
	expiredProjectBatchSize    = 100
	expiredReleaseBatchSize    = 100
	maintenanceCleanupInterval = time.Hour
)

// RemoveRetiredReleases removes only database-confirmed deleting releases. A
// failure leaves the row in deleting so the next maintenance run can retry.
func RemoveRetiredReleases(conf *config.WebProjectsConfig, releases []*model.WebProjectRelease) error {
	if conf == nil || conf.StorageRoot == "" {
		return ErrInvalid
	}
	store := repository.New()
	var result error
	for _, candidate := range releases {
		if candidate == nil || candidate.ProjectID <= 0 || candidate.ID <= 0 {
			result = errors.Join(result, fmt.Errorf("%w: invalid retired release identity", ErrInvalid))
			continue
		}
		release, err := store.FindRemovableDeletingRelease(candidate.ProjectID, candidate.ID)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%w: query deleting release: %v", ErrDependency, err))
			continue
		}
		if release == nil {
			continue
		}
		releaseDir, missing, err := ReleaseDirectory(conf, release)
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
		deleted, err := store.DeleteDeletingRelease(release.ProjectID, release.ID)
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
	store := repository.New()
	var result error
	processedReleaseIDs := make(map[int64]struct{})
	var afterDeletedAt *time.Time
	var afterID int64
	preparedProjects := 0
	for preparedProjects < expiredProjectBatchSize {
		projects, err := store.ListExpiredProjects(cutoff, afterDeletedAt, afterID, expiredProjectBatchSize)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%w: list expired projects: %v", ErrDependency, err))
			break
		}
		if len(projects) == 0 {
			break
		}
		for _, project := range projects {
			afterDeletedAt, afterID = project.DeletedAt, project.ID
			releases, prepareErr := store.PrepareExpiredProjectCleanup(project.ID, cutoff, expiredReleaseBatchSize)
			if prepareErr != nil {
				result = errors.Join(result, fmt.Errorf("%w: prepare expired project %d: %v", ErrDependency, project.ID, prepareErr))
				continue
			}
			if len(releases) == 0 {
				continue
			}
			preparedProjects++
			for _, release := range releases {
				processedReleaseIDs[release.ID] = struct{}{}
			}
			if err := RemoveRetiredReleases(conf, releases); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup expired project %d: %w", project.ID, err))
			}
			if preparedProjects >= expiredProjectBatchSize {
				break
			}
		}
		if len(projects) < expiredProjectBatchSize {
			break
		}
	}

	retryReleases, err := store.ListDeletingReleases(expiredReleaseBatchSize)
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

// SelectReleasesForPruning computes release count and bytes in Go from a
// bounded, oldest-first DAL result. The current release contributes to quota
// but is never selected, preserving the content served during concurrent upload.
func SelectReleasesForPruning(releases []*model.WebProjectRelease, currentReleaseID *int64, maxBytes int64, maxReleases int, incomingBytes int64) ([]*model.WebProjectRelease, error) {
	if maxBytes <= 0 || maxReleases <= 0 || incomingBytes < 0 {
		return nil, ErrInvalid
	}
	var totalBytes int64
	for _, release := range releases {
		if release == nil || release.TotalBytes < 0 {
			return nil, ErrInvalid
		}
		totalBytes += release.TotalBytes
	}
	remainingCount := len(releases)
	retired := make([]*model.WebProjectRelease, 0)
	for _, release := range releases {
		if totalBytes+incomingBytes <= maxBytes && remainingCount+1 <= maxReleases {
			break
		}
		if currentReleaseID != nil && release.ID == *currentReleaseID {
			continue
		}
		retired = append(retired, release)
		totalBytes -= release.TotalBytes
		remainingCount--
	}
	if totalBytes+incomingBytes > maxBytes {
		return nil, ErrTooLarge
	}
	if remainingCount+1 > maxReleases {
		return nil, ErrRateLimited
	}
	return retired, nil
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
