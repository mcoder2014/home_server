package dal

import (
	"errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
)

// QueryWebProjectReleaseReferences returns the release side of the audit
// relationship. uploaded_by is evidence to verify, not a trusted owner source.
func QueryWebProjectReleaseReferences(releaseIDs []int64) ([]*model.WebProjectRelease, error) {
	if len(releaseIDs) == 0 {
		return []*model.WebProjectRelease{}, nil
	}
	if db.MasterDB() == nil {
		return nil, errors.New("database is not initialized")
	}
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable).
		Select("id", "project_id", "uploaded_by", "storage_key").
		Where("id IN ?", releaseIDs).
		Find(&releases).Error
	return releases, err
}

// QueryWebProjectOwnerReferences returns only the project ownership columns.
// Audit callers batch this separately from release reads to avoid a JOIN.
func QueryWebProjectOwnerReferences(projectIDs []int64) ([]*model.WebProject, error) {
	if len(projectIDs) == 0 {
		return []*model.WebProject{}, nil
	}
	if db.MasterDB() == nil {
		return nil, errors.New("database is not initialized")
	}
	var projects []*model.WebProject
	err := db.MasterDB().Table(WebProjectTable).
		Select("id", "owner_user_id").
		Where("id IN ?", projectIDs).
		Find(&projects).Error
	return projects, err
}

// CompareAndSwapWebProjectReleaseStorageKey changes only a verified legacy
// reference. Ownership and the old key are part of the compare condition.
func CompareAndSwapWebProjectReleaseStorageKey(releaseID, projectID, uploadedBy int64, oldStorageKey, newStorageKey string) (bool, error) {
	if db.MasterDB() == nil {
		return false, errors.New("database is not initialized")
	}
	result := db.MasterDB().Table(WebProjectReleaseTable).
		Where("id = ? AND project_id = ? AND uploaded_by = ? AND storage_key = ?", releaseID, projectID, uploadedBy, oldStorageKey).
		Update("storage_key", newStorageKey)
	return result.RowsAffected == 1, result.Error
}
