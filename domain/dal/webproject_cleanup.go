package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ListReadyWebProjectReleases(tx *gorm.DB, projectID int64, limit int) ([]*model.WebProjectRelease, error) {
	query := tx.Table(WebProjectReleaseTable).Select(releaseColumns).
		Where("project_id = ? AND status = ?", projectID, model.WebProjectReleaseReady)
	var releases []*model.WebProjectRelease
	err := query.Order("create_time ASC, id ASC").Limit(limit).Find(&releases).Error
	if err == nil {
		for _, release := range releases {
			if decodeErr := release.DecodeExtra(); decodeErr != nil {
				return nil, decodeErr
			}
		}
	}
	return releases, err
}

func MarkWebProjectReleasesDeleting(tx *gorm.DB, projectID int64, releaseIDs []int64, readyOnly bool) (int64, error) {
	if len(releaseIDs) == 0 {
		return 0, nil
	}
	query := tx.Table(WebProjectReleaseTable).Where("project_id = ? AND id IN ?", projectID, releaseIDs)
	if readyOnly {
		query = query.Where("status = ?", model.WebProjectReleaseReady)
	}
	result := query.Updates(map[string]interface{}{"status": model.WebProjectReleaseDeleting, "update_time": time.Now()})
	return result.RowsAffected, result.Error
}

func QueryDeletingWebProjectRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	var release model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable).
		Select(releaseColumns).
		Where("project_id = ? AND id = ? AND status = ?", projectID, releaseID, model.WebProjectReleaseDeleting).
		Take(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil {
		err = release.DecodeExtra()
	}
	return &release, err
}

func DeleteDeletingWebProjectRelease(projectID, releaseID int64) (bool, error) {
	result := db.MasterDB().Table(WebProjectReleaseTable).
		Where("project_id = ? AND id = ? AND status = ?", projectID, releaseID, model.WebProjectReleaseDeleting).
		Delete(&model.WebProjectRelease{})
	return result.RowsAffected == 1, result.Error
}

func ListExpiredWebProjects(before time.Time, afterDeletedAt *time.Time, afterID int64, limit int) ([]*model.WebProject, error) {
	query := db.MasterDB().Table(WebProjectTable).
		Select(projectColumns).
		Where("status = ? AND deleted_at IS NOT NULL AND deleted_at < ?", model.WebProjectStatusDeleted, before)
	if afterDeletedAt != nil {
		query = query.Where("(deleted_at, id) > (?, ?)", *afterDeletedAt, afterID)
	}
	var projects []*model.WebProject
	err := query.Order("deleted_at ASC, id ASC").
		Limit(limit).
		Find(&projects).Error
	return projects, err
}

func ListDeletingWebProjectReleases(limit int) ([]*model.WebProjectRelease, error) {
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable).
		Select(releaseColumns).
		Where("status = ?", model.WebProjectReleaseDeleting).
		Order("update_time ASC, id ASC").
		Limit(limit).
		Find(&releases).Error
	if err == nil {
		for _, release := range releases {
			if decodeErr := release.DecodeExtra(); decodeErr != nil {
				return nil, decodeErr
			}
		}
	}
	return releases, err
}

func LockWebProjectByID(tx *gorm.DB, projectID int64) (*model.WebProject, error) {
	var project model.WebProject
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table(WebProjectTable).
		Select(projectColumns).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func ListWebProjectReleasesForCleanup(tx *gorm.DB, projectID int64, limit int) ([]*model.WebProjectRelease, error) {
	var releases []*model.WebProjectRelease
	err := tx.Table(WebProjectReleaseTable).Select(releaseColumns).
		Where("project_id = ?", projectID).
		Order("create_time ASC, id ASC").
		Limit(limit).
		Find(&releases).Error
	return releases, err
}

func ClearExpiredWebProjectCurrentRelease(tx *gorm.DB, projectID int64, before time.Time) (bool, error) {
	result := tx.Table(WebProjectTable).
		Where("id = ? AND status = ? AND deleted_at IS NOT NULL AND deleted_at < ?", projectID, model.WebProjectStatusDeleted, before).
		Updates(map[string]interface{}{"current_release_id": nil, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
	return result.RowsAffected == 1, result.Error
}
