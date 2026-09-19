package manuals

import (
	"context"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

// requireWritePolicy revalidates the authenticated snapshot after any upload
// work and before taking resource locks or committing side effects.
func requireWritePolicy(ctx context.Context, tx *gorm.DB, ownerID int64, principal *utils.Principal) error {
	if !config.Global().Manuals.Enabled {
		return apperrors.WithMessage(apperrors.ErrForbidden, "功能暂未开放")
	}
	if principal == nil || principal.UserID != ownerID || (principal.Kind != "user" && principal.Kind != "application") {
		return apperrors.ErrUnauthorized
	}
	if err := checkWriteExpiry(principal); err != nil {
		return err
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
	if !accounts.DatabaseMode() {
		if principal.Kind == "user" {
			return passport.RequireConfigSessionTx(ctx, tx, utils.GetTokenFromCtx(ctx), ownerID, principal.AuthVersion)
		}
		identity, err := passport.GetByID(ctx, ownerID)
		if err != nil {
			return apperrors.ErrDependency
		}
		if identity == nil {
			return apperrors.ErrUnauthorized
		}
		return accounts.RequireApplicationSnapshotTx(tx, principal, "manuals:write")
	}
	if principal.AuthVersion <= 0 {
		return apperrors.ErrUnauthorized
	}
	if principal.Kind == "user" {
		user, err := accounts.RequireUserSessionTx(tx, utils.GetTokenFromCtx(ctx), ownerID)
		if err != nil {
			return err
		}
		if user.AuthVersion != principal.AuthVersion {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if _, err := accounts.RequireUserTx(tx, ownerID, principal.AuthVersion, false); err != nil {
		return err
	}
	return accounts.RequireApplicationSnapshotTx(tx, principal, "manuals:write")
}

func checkWriteExpiry(principal *utils.Principal) error {
	if principal == nil || principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now()) {
		return apperrors.ErrUnauthorized
	}
	return nil
}
