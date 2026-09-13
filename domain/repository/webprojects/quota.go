package webprojects

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

// readProjectQuota runs after requireWritePolicy has locked the same namespace
// and owner. This reads the persisted quota, not a possibly stale process copy.
func readProjectQuota(tx *gorm.DB) (int64, int64, error) {
	rows, err := dal.ReadSiteConfigs(tx, []string{"web_projects"}, false)
	if err != nil || len(rows) != 1 {
		return 0, 0, apperrors.ErrDependency
	}
	values, err := config.DecodeValues(rows[0].ValuesJSON)
	if err != nil {
		return 0, 0, apperrors.ErrDependency
	}
	values, err = config.ValidateValues(config.Global(), "web_projects", values)
	if err != nil {
		return 0, 0, apperrors.ErrDependency
	}
	return values["max_projects_per_user"].(int64), values["max_user_bytes"].(int64), nil
}

func requireProjectSlot(tx *gorm.DB, ownerID int64) error {
	if !accounts.DatabaseMode() {
		return nil
	}
	limit, _, err := readProjectQuota(tx)
	if err != nil {
		return err
	}
	count, err := dal.CountActiveUserWebProjects(tx, ownerID)
	if err != nil {
		return apperrors.ErrDependency
	}
	if count >= limit {
		return apperrors.WithMessage(apperrors.ErrRateLimited, "已达到账号的网页项目数量上限")
	}
	return nil
}

// CheckUserUploadQuota must precede release pruning and creation while the
// caller holds the owner's row lock. All projects share the same user budget.
func (tx *Transaction) CheckUserUploadQuota(ownerID, incomingBytes int64) error {
	if !accounts.DatabaseMode() {
		return nil
	}
	if incomingBytes < 0 {
		return apperrors.ErrInvalid
	}
	_, maximum, err := readProjectQuota(tx.database)
	if err != nil {
		return err
	}
	used, err := dal.SumUserWebReleaseBytes(tx.database, ownerID)
	if err != nil {
		return apperrors.ErrDependency
	}
	if used > maximum || incomingBytes > maximum-used {
		return apperrors.WithMessage(apperrors.ErrTooLarge, "网页上传超出账号存储额度，请清理历史版本后重试")
	}
	return nil
}
