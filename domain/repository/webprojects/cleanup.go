package webprojects

import (
	"errors"
	"fmt"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

func (repository *Repository) FindRemovableDeletingRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	release, err := dal.QueryDeletingWebProjectRelease(projectID, releaseID)
	if err != nil || release == nil {
		return nil, err
	}
	project, err := dal.QueryWebProjectByID(projectID)
	if err != nil {
		return nil, err
	}
	if project != nil && project.CurrentReleaseID != nil && *project.CurrentReleaseID == releaseID {
		return nil, nil
	}
	return release, nil
}

func (repository *Repository) DeleteDeletingRelease(projectID, releaseID int64) (bool, error) {
	return dal.DeleteDeletingWebProjectRelease(projectID, releaseID)
}

func (repository *Repository) ListExpiredProjects(before time.Time, afterDeletedAt *time.Time, afterID int64, limit int) ([]*model.WebProject, error) {
	return dal.ListExpiredWebProjects(before, afterDeletedAt, afterID, limit)
}

func (repository *Repository) ListDeletingReleases(limit int) ([]*model.WebProjectRelease, error) {
	return dal.ListDeletingWebProjectReleases(limit)
}

// PrepareExpiredProjectCleanup serializes with restore by locking the project
// row, reads one bounded release page, marks those rows deleting, and clears the
// current pointer in the same transaction. A concurrent restore either wins
// before the lock or observes the incremented revision after commit.
func (repository *Repository) PrepareExpiredProjectCleanup(projectID int64, cutoff time.Time, limit int) ([]*model.WebProjectRelease, error) {
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		project, err := dal.LockWebProjectByID(tx, projectID)
		if err != nil {
			return err
		}
		if project == nil || project.Status != model.WebProjectStatusDeleted || project.DeletedAt == nil || !project.DeletedAt.Before(cutoff) {
			return nil
		}
		releases, err = dal.ListWebProjectReleasesForCleanup(tx, projectID, limit)
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
			return errors.New("release state changed while preparing cleanup")
		}
		cleared, err := dal.ClearExpiredWebProjectCurrentRelease(tx, projectID, cutoff)
		if err != nil {
			return err
		}
		if !cleared {
			return fmt.Errorf("project state changed while preparing cleanup")
		}
		for _, release := range releases {
			release.Status = model.WebProjectReleaseDeleting
		}
		return nil
	})
	return releases, err
}
