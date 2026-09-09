package dal

import (
	"errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
)

// QueryWebProjectReleaseReferences returns only the database fields needed to
// compare generated release directories with their persisted references.
func QueryWebProjectReleaseReferences(releaseIDs []int64) ([]*model.WebProjectRelease, error) {
	if len(releaseIDs) == 0 {
		return []*model.WebProjectRelease{}, nil
	}
	if db.MasterDB() == nil {
		return nil, errors.New("database is not initialized")
	}
	var releases []*model.WebProjectRelease
	err := db.MasterDB().Table(WebProjectReleaseTable).
		Select("id", "project_id", "storage_key").
		Where("id IN ?", releaseIDs).
		Find(&releases).Error
	return releases, err
}
