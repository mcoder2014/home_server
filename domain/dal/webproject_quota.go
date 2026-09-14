package dal

import (
	"errors"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

func CountActiveUserWebProjects(tx *gorm.DB, ownerID int64) (int64, error) {
	var count int64
	err := tx.Table(WebProjectTable).Where("owner_user_id = ? AND status <> ?", ownerID, model.WebProjectStatusDeleted).Count(&count).Error
	return count, err
}

// SumUserWebReleaseBytes includes every state. Marking a release deleting must
// not release capacity while its files still exist; only row removal does so.
func SumUserWebReleaseBytes(tx *gorm.DB, ownerID int64) (int64, error) {
	var usage struct {
		Bytes       int64
		InvalidRows int64
	}
	err := tx.Table(WebProjectReleaseTable).Select("COALESCE(SUM(total_bytes),0) AS bytes, COALESCE(SUM(CASE WHEN total_bytes < 0 THEN 1 ELSE 0 END),0) AS invalid_rows").Where("uploaded_by = ?", ownerID).Scan(&usage).Error
	if err != nil {
		return 0, err
	}
	if usage.InvalidRows > 0 || usage.Bytes < 0 {
		return 0, errors.New("invalid recorded web release size")
	}
	return usage.Bytes, nil
}
