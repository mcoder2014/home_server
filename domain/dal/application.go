package dal

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	ApplicationTable      = "application"
	ApplicationTokenTable = "application_access_token"
)

var applicationColumns = []string{
	"id", "owner_user_id", "name", "description", "access_key", "scopes", "status",
	"active_slot", "revision", "secret_version", "expires_at", "last_issued_at", "create_time", "update_time",
}

var applicationCredentialColumns = append(append([]string(nil), applicationColumns...), "secret_digest")

var applicationAuthColumns = []string{
	"id", "owner_user_id", "scopes", "status", "revision", "secret_version", "expires_at",
}

var applicationTokenColumns = []string{
	"id", "application_id", "token_digest", "secret_version", "application_revision", "scope_snapshot", "expired_at", "create_time",
}

func CreateApplication(tx *gorm.DB, application *model.Application) error {
	result := sensitive(tx).Table(ApplicationTable).Create(application)
	return result.Error
}

func LockApplicationSlots(tx *gorm.DB, ownerUserID int64, limit int) ([]int, error) {
	var slots []int
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table(ApplicationTable).
		Where("owner_user_id = ? AND active_slot IS NOT NULL", ownerUserID).
		Order("active_slot ASC").Limit(limit).Pluck("active_slot", &slots).Error
	return slots, err
}

func ListOwnedApplications(database *gorm.DB, ownerUserID, cursor int64, limit int) ([]*model.Application, error) {
	query := database.Table(ApplicationTable).Select(applicationColumns).Where("owner_user_id = ?", ownerUserID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	var applications []*model.Application
	err := query.Order("id DESC").Limit(limit).Find(&applications).Error
	return applications, err
}

func QueryOwnedApplication(database *gorm.DB, ownerUserID, applicationID int64) (*model.Application, error) {
	var application model.Application
	err := database.Table(ApplicationTable).Select(applicationColumns).
		Where("owner_user_id = ? AND id = ?", ownerUserID, applicationID).Take(&application).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &application, err
}

func QueryApplicationByAccessKey(database *gorm.DB, accessKey string) (*model.Application, error) {
	var application model.Application
	err := sensitive(database).Table(ApplicationTable).Select(applicationCredentialColumns).Where("access_key = ?", accessKey).Take(&application).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &application, err
}

func QueryApplicationByID(database *gorm.DB, applicationID int64) (*model.Application, error) {
	var application model.Application
	err := database.Table(ApplicationTable).Select(applicationAuthColumns).Where("id = ?", applicationID).Take(&application).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &application, err
}

func LockApplicationByID(tx *gorm.DB, applicationID int64) (*model.Application, error) {
	var application model.Application
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table(ApplicationTable).Select(applicationAuthColumns).
		Where("id = ?", applicationID).Take(&application).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &application, err
}

func UpdateOwnedApplication(database *gorm.DB, application *model.Application, expectedRevision int64) (bool, error) {
	scopes, err := json.Marshal(application.Scopes)
	if err != nil {
		return false, err
	}
	fields := map[string]interface{}{
		"name": application.Name, "description": application.Description, "scopes": string(scopes),
		"status": application.Status, "active_slot": application.ActiveSlot, "revision": application.Revision,
		"secret_version": application.SecretVersion, "expires_at": application.ExpiresAt, "update_time": application.UpdateTime,
	}
	if len(application.SecretDigest) > 0 {
		fields["secret_digest"] = application.SecretDigest
	}
	result := sensitive(database).Table(ApplicationTable).
		Where("owner_user_id = ? AND id = ? AND revision = ?", application.OwnerUserID, application.ID, expectedRevision).
		Updates(fields)
	return result.RowsAffected == 1, result.Error
}

func CreateApplicationToken(tx *gorm.DB, token *model.ApplicationAccessToken) error {
	result := sensitive(tx).Table(ApplicationTokenTable).Create(token)
	return result.Error
}

func UpdateLastIssuedAt(tx *gorm.DB, applicationID int64, issuedAt time.Time) error {
	return tx.Table(ApplicationTable).Where("id = ?", applicationID).
		Updates(map[string]interface{}{"last_issued_at": issuedAt, "update_time": issuedAt}).Error
}

func QueryApplicationTokenByDigest(database *gorm.DB, digest []byte) (*model.ApplicationAccessToken, error) {
	var token model.ApplicationAccessToken
	err := sensitive(database).Table(ApplicationTokenTable).Select(applicationTokenColumns).Where("token_digest = ?", digest).Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &token, err
}

func ListExpiredApplicationTokenIDs(tx *gorm.DB, before time.Time, limit int) ([]int64, error) {
	var ids []int64
	err := tx.Table(ApplicationTokenTable).Where("expired_at <= ?", before).
		Order("expired_at ASC, id ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func DeleteApplicationTokens(tx *gorm.DB, tokenIDs []int64) error {
	if len(tokenIDs) == 0 {
		return nil
	}
	return tx.Table(ApplicationTokenTable).Where("id IN ?", tokenIDs).Delete(&model.ApplicationAccessToken{}).Error
}

func sensitive(database *gorm.DB) *gorm.DB {
	session := database.Session(&gorm.Session{Logger: logger.Discard})
	return session
}
