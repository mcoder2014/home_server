package fileshare

import (
	"context"
	"sort"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func requireWritePolicy(ctx context.Context, tx *gorm.DB, principal *utils.Principal, memberIDs []int64) error {
	if !config.Global().FileSharing.Enabled {
		return apperrors.WithMessage(service.ErrForbidden, "功能暂未开放")
	}
	if principal == nil || principal.UserID <= 0 || (principal.Kind != "user" && principal.Kind != "application") || principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now()) {
		return service.ErrUnauthorized
	}
	if principal.Kind == "application" {
		enabled, err := accounts.EnabledTx(tx, "auth", "applications_enabled", true)
		if err != nil || !enabled {
			if err != nil {
				return err
			}
			return service.ErrForbidden
		}
	}
	if !accounts.DatabaseMode() {
		// Every request for one config-backed owner locks the stable oldest
		// session and application rows before checking its exact credential. The
		// two anchors serialize quota writes across credential kinds and service
		// instances without scanning unbounded expired-session history.
		var sessionIDs, applicationIDs []int64
		if err := tx.Table(dal.TableUserToken).Where("user_id = ?", principal.UserID).Order("id ASC").Limit(1).Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &sessionIDs).Error; err != nil {
			return service.ErrDependency
		}
		if err := tx.Table(dal.ApplicationTable).Where("owner_user_id = ?", principal.UserID).Order("id ASC").Limit(1).Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &applicationIDs).Error; err != nil {
			return service.ErrDependency
		}
		if principal.Kind == "user" {
			if err := passport.RequireConfigSessionTx(ctx, tx, utils.GetTokenFromCtx(ctx), principal.UserID, principal.AuthVersion); err != nil {
				return err
			}
		} else if err := accounts.RequireApplicationSnapshotTx(tx, principal, "files:write"); err != nil {
			return err
		}
		for _, userID := range memberIDs {
			identity, err := passport.GetByID(ctx, userID)
			if err != nil {
				return service.ErrDependency
			}
			if identity == nil {
				return service.ErrInvalid
			}
		}
		return nil
	}
	ids := append([]int64{principal.UserID}, memberIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	deduplicated := ids[:0]
	for _, id := range ids {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != id {
			deduplicated = append(deduplicated, id)
		}
	}
	var users []model.UserAccount
	err := tx.Table(dal.AccountTable).Select("id", "status", "must_change_password", "auth_version").Where("id IN ?", deduplicated).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&users).Error
	if err != nil {
		return service.ErrDependency
	}
	if len(users) != len(deduplicated) {
		return service.ErrInvalid
	}
	for _, user := range users {
		if user.Status != model.AccountActive || user.MustChangePassword {
			if user.ID == principal.UserID {
				return service.ErrUnauthorized
			}
			return service.ErrInvalid
		}
		if user.ID == principal.UserID && user.AuthVersion != principal.AuthVersion {
			return service.ErrUnauthorized
		}
	}
	if principal.Kind == "user" {
		_, err = accounts.RequireUserSessionTx(tx, utils.GetTokenFromCtx(ctx), principal.UserID)
		return err
	}
	return accounts.RequireApplicationSnapshotTx(tx, principal, "files:write")
}

func ownerActive(ctx context.Context, ownerID int64) (bool, error) {
	if accounts.DatabaseMode() {
		owner, err := accounts.GetByID(ctx, ownerID)
		if err != nil {
			return false, err
		}
		return owner != nil && owner.Status == model.AccountActive && !owner.MustChangePassword, nil
	}
	owner, err := passport.GetByID(ctx, ownerID)
	if err != nil {
		return false, service.ErrDependency
	}
	return owner != nil, nil
}
