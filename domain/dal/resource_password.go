package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const ResourcePasswordTable = "resource_passwords"

var resourcePasswordColumns = []string{"resource_type", "resource_id", "password_hash", "version", "update_time"}

func FindResourcePassword(database *gorm.DB, resourceType string, resourceID int64, lock bool) (*model.ResourcePassword, error) {
	query := database.Table(ResourcePasswordTable).Select(resourcePasswordColumns).Where("resource_type = ? AND resource_id = ?", resourceType, resourceID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var record model.ResourcePassword
	err := query.Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &record, err
}

func ListResourcePasswords(database *gorm.DB, resourceType string, resourceIDs []int64) ([]*model.ResourcePassword, error) {
	if len(resourceIDs) == 0 {
		return []*model.ResourcePassword{}, nil
	}
	var records []*model.ResourcePassword
	err := database.Table(ResourcePasswordTable).Select(resourcePasswordColumns).
		Where("resource_type = ? AND resource_id IN ?", resourceType, resourceIDs).
		Order("resource_id ASC").Find(&records).Error
	return records, err
}

func InsertResourcePassword(tx *gorm.DB, record *model.ResourcePassword) error {
	return tx.Session(&gorm.Session{Logger: logger.Discard}).Table(ResourcePasswordTable).Select(resourcePasswordColumns).Create(record).Error
}

func UpdateResourcePassword(tx *gorm.DB, resourceType string, resourceID, expectedVersion int64, passwordHash string, updatedAt time.Time) (bool, error) {
	result := tx.Session(&gorm.Session{Logger: logger.Discard}).Table(ResourcePasswordTable).
		Where("resource_type = ? AND resource_id = ? AND version = ?", resourceType, resourceID, expectedVersion).
		Updates(map[string]interface{}{"password_hash": passwordHash, "version": gorm.Expr("version + 1"), "update_time": updatedAt})
	return result.RowsAffected == 1, result.Error
}
