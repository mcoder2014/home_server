package dal

import (
	"errors"
	"sort"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SiteConfigCurrentTable = "site_config_current"
	SiteConfigHistoryTable = "site_config_history"
	SiteRuntimeStateTable  = "site_runtime_state"
)

var siteConfigCurrentColumns = []string{"namespace", "revision", "schema_version", "values_json", "values_sha256", "updated_by", "update_time"}
var siteConfigHistoryColumns = []string{"namespace", "revision", "schema_version", "values_json", "values_sha256", "request_id", "request_hash", "actor_user_id", "reason", "rollback_from_revision", "create_time"}

// ReadSiteConfigs locks in namespace order. Callers needing the global runtime
// lock must acquire it before this function and acquire user locks afterwards.
func ReadSiteConfigs(database *gorm.DB, namespaces []string, lock bool) ([]model.SiteConfigCurrent, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}
	ordered := append([]string(nil), namespaces...)
	sort.Strings(ordered)
	query := database.Table(SiteConfigCurrentTable).Select(siteConfigCurrentColumns).Where("namespace IN ?", ordered).Order("namespace ASC")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []model.SiteConfigCurrent
	err := query.Find(&rows).Error
	return rows, err
}

func ReadSiteRuntimeState(database *gorm.DB, lock bool) (*model.SiteRuntimeState, error) {
	query := database.Table(SiteRuntimeStateTable).Select("id", "config_generation", "registration_epoch", "revision", "update_time").Where("id = ?", 1)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var state model.SiteRuntimeState
	if err := query.Take(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func ReadSiteConfigRequest(database *gorm.DB, namespace, requestID string) (*model.SiteConfigHistory, error) {
	var history model.SiteConfigHistory
	err := database.Table(SiteConfigHistoryTable).Select(siteConfigHistoryColumns).Where("namespace = ? AND request_id = ?", namespace, requestID).Take(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &history, err
}

func ReadSiteConfigRevision(database *gorm.DB, namespace string, revision int64) (*model.SiteConfigHistory, error) {
	var history model.SiteConfigHistory
	err := database.Table(SiteConfigHistoryTable).Select(siteConfigHistoryColumns).Where("namespace = ? AND revision = ?", namespace, revision).Take(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &history, err
}

func ListSiteConfigHistory(database *gorm.DB, namespace string, cursor int64, limit int) ([]model.SiteConfigHistory, error) {
	query := database.Table(SiteConfigHistoryTable).Select(siteConfigHistoryColumns).Where("namespace = ?", namespace)
	if cursor > 0 {
		query = query.Where("revision < ?", cursor)
	}
	var history []model.SiteConfigHistory
	err := query.Order("revision DESC").Limit(limit).Find(&history).Error
	return history, err
}

// SaveSiteConfig is called only after locking runtime and current rows. It
// appends immutable history and updates both current and global state in tx.
func SaveSiteConfig(tx *gorm.DB, current *model.SiteConfigCurrent, history *model.SiteConfigHistory, state *model.SiteRuntimeState) error {
	if err := tx.Table(SiteConfigHistoryTable).Create(history).Error; err != nil {
		return err
	}
	result := tx.Table(SiteConfigCurrentTable).Where("namespace = ? AND revision = ?", current.Namespace, current.Revision-1).Updates(map[string]interface{}{
		"revision": current.Revision, "schema_version": current.SchemaVersion, "values_json": current.ValuesJSON, "values_sha256": current.ValuesSHA256, "updated_by": current.UpdatedBy, "update_time": current.UpdateTime,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	result = tx.Table(SiteRuntimeStateTable).Where("id = ? AND revision = ?", 1, state.Revision-1).Updates(map[string]interface{}{
		"config_generation": state.ConfigGeneration, "registration_epoch": state.RegistrationEpoch, "revision": state.Revision, "update_time": state.UpdateTime,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
