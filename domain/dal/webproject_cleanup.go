package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WebProjectReleaseUsage struct {
	ReleaseCount int64 `gorm:"column:release_count"`
	TotalBytes   int64 `gorm:"column:total_bytes"`
}

const qualifiedCleanupReleaseColumns = "release_row.id, release_row.project_id, release_row.uploaded_by, release_row.storage_key, release_row.status, release_row.entry_file, release_row.sha256, release_row.file_count, release_row.total_bytes, release_row.idempotency_key, release_row.error_message, release_row.create_time, release_row.update_time"

func QueryReadyWebProjectReleaseUsage(tx *gorm.DB, projectID int64) (WebProjectReleaseUsage, error) {
	var usage WebProjectReleaseUsage
	err := tx.Table(WebProjectReleaseTable).
		Select("COUNT(id) AS release_count, COALESCE(SUM(total_bytes), 0) AS total_bytes").
		Where("project_id = ? AND status = ?", projectID, "ready").
		Scan(&usage).Error
	return usage, err
}

func ListOldestPrunableWebProjectReleases(tx *gorm.DB, projectID int64, currentReleaseID *int64, limit int) ([]*model.WebProjectRelease, error) {
	query := tx.Table(WebProjectReleaseTable).Select(releaseColumns).
		Where("project_id = ? AND status = ?", projectID, "ready")
	if currentReleaseID != nil {
		query = query.Where("id <> ?", *currentReleaseID)
	}
	var releases []*model.WebProjectRelease
	err := query.Order("create_time ASC, id ASC").Limit(limit).Find(&releases).Error
	return releases, err
}

func MarkWebProjectReleasesDeleting(tx *gorm.DB, projectID int64, releaseIDs []int64, readyOnly bool) (int64, error) {
	if len(releaseIDs) == 0 {
		return 0, nil
	}
	query := tx.Table(WebProjectReleaseTable).Where("project_id = ? AND id IN ?", projectID, releaseIDs)
	if readyOnly {
		query = query.Where("status = ?", "ready")
	}
	result := query.Updates(map[string]interface{}{"status": "deleting", "update_time": time.Now()})
	return result.RowsAffected, result.Error
}

func QueryDeletingWebProjectRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	var release model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable+" AS release_row").
		Select(qualifiedCleanupReleaseColumns).
		Joins("INNER JOIN "+WebProjectTable+" AS project ON project.id = release_row.project_id").
		Where("release_row.project_id = ? AND release_row.id = ? AND release_row.status = ?", projectID, releaseID, "deleting").
		Where("project.current_release_id IS NULL OR project.current_release_id <> release_row.id").
		Take(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &release, err
}

func DeleteDeletingWebProjectRelease(projectID, releaseID int64) (bool, error) {
	currentRelease := db.MasterDB().Table(WebProjectTable).
		Select("1").
		Where("id = ? AND current_release_id = ?", projectID, releaseID)
	result := db.MasterDB().Table(WebProjectReleaseTable).
		Where("project_id = ? AND id = ? AND status = ?", projectID, releaseID, "deleting").
		Where("NOT EXISTS (?)", currentRelease).
		Delete(&model.WebProjectRelease{})
	return result.RowsAffected == 1, result.Error
}

func ListExpiredWebProjectIDs(before time.Time, limit int) ([]int64, error) {
	hasRelease := db.MasterDB().Table(WebProjectReleaseTable + " AS release_row").
		Select("1").Where("release_row.project_id = project.id")
	var projectIDs []int64
	err := db.MasterDB().Table(WebProjectTable+" AS project").
		Select("project.id").
		Where("project.status = ? AND project.deleted_at IS NOT NULL AND project.deleted_at < ?", "deleted", before).
		Where("EXISTS (?)", hasRelease).
		Order("project.deleted_at ASC, project.id ASC").
		Limit(limit).
		Scan(&projectIDs).Error
	return projectIDs, err
}

func ListDeletingWebProjectReleases(limit int) ([]*model.WebProjectRelease, error) {
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable+" AS release_row").
		Select(qualifiedCleanupReleaseColumns).
		Joins("INNER JOIN "+WebProjectTable+" AS project ON project.id = release_row.project_id").
		Where("release_row.status = ?", "deleting").
		Where("project.current_release_id IS NULL OR project.current_release_id <> release_row.id").
		Order("release_row.update_time ASC, release_row.id ASC").
		Limit(limit).
		Find(&releases).Error
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
		Where("id = ? AND status = ? AND deleted_at IS NOT NULL AND deleted_at < ?", projectID, "deleted", before).
		Updates(map[string]interface{}{"current_release_id": nil, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
	return result.RowsAffected == 1, result.Error
}
