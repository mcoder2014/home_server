package webprojects

import (
	"sort"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// requireWritePolicy holds the module row and users before any project row.
// Browser requests carry their starting auth version, so a long upload cannot
// commit after password change/logout-all even when the account stays active.
// Member validation and user locking share one bounded query in ID order.
func requireWritePolicy(tx *gorm.DB, ownerID int64, principals []*utils.Principal, memberIDs []int64) error {
	if !accounts.DatabaseMode() {
		return nil
	}
	var principal *utils.Principal
	if len(principals) > 0 {
		if len(principals) != 1 || principals[0] == nil {
			return apperrors.ErrUnauthorized
		}
		principal = principals[0]
		if principal.UserID != ownerID || principal.AuthVersion <= 0 || (principal.Kind != "user" && principal.Kind != "application") {
			return apperrors.ErrUnauthorized
		}
		if principal.Kind == "application" {
			enabled, err := accounts.EnabledTx(tx, "auth", "applications_enabled", true)
			if err != nil {
				return err
			}
			if !enabled {
				return apperrors.ErrForbidden
			}
		}
	}
	enabled, err := accounts.EnabledTx(tx, "web_projects", "enabled", true)
	if err != nil {
		return err
	}
	if !enabled {
		return apperrors.ErrForbidden
	}
	ids := append([]int64{ownerID}, memberIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	expected := map[int64]bool{}
	for _, id := range ids {
		expected[id] = true
	}
	var users []model.UserAccount
	err = tx.Table(dal.AccountTable).Select("id", "status", "must_change_password", "auth_version").Where("id IN ?", ids).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&users).Error
	if err != nil {
		return apperrors.ErrDependency
	}
	ownerFound := false
	for _, user := range users {
		if user.ID == ownerID {
			ownerFound = true
			if user.Status != model.AccountActive || user.MustChangePassword {
				return apperrors.ErrForbidden
			}
			if principal != nil && principal.AuthVersion != user.AuthVersion {
				return apperrors.ErrUnauthorized
			}
		} else if user.Status != model.AccountActive || user.MustChangePassword {
			return apperrors.ErrInvalid
		}
	}
	if !ownerFound {
		return apperrors.ErrUnauthorized
	}
	if len(users) != len(expected) {
		return apperrors.ErrInvalid
	}
	if err := checkWriteExpiry(principals); err != nil {
		return err
	}
	if principal != nil && principal.Kind == "application" {
		return accounts.RequireApplicationSnapshotTx(tx, principal, "web-projects:write")
	}
	return nil
}

// Recheck wall-clock expiry after resource locks/quota reads as well as after
// owner authorization. A request-local transaction carries its own principal.
func checkWriteExpiry(principals []*utils.Principal) error {
	if !accounts.DatabaseMode() || len(principals) == 0 {
		return nil
	}
	if len(principals) != 1 || principals[0] == nil || principals[0].TokenExpiresAt.IsZero() || !principals[0].TokenExpiresAt.After(time.Now()) {
		return apperrors.ErrUnauthorized
	}
	return nil
}
