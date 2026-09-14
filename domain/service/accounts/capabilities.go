package accounts

import (
	"context"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func ValidateApplicationScopes(user *model.UserAccount, scopes []string) error {
	if user == nil || user.Status != model.AccountActive || user.MustChangePassword {
		return apperrors.ErrUnauthorized
	}
	for _, scope := range scopes {
		if strings.HasPrefix(scope, "library:") && !user.LibraryEnabled {
			return apperrors.ErrForbidden
		}
		if strings.HasPrefix(scope, "webdav:") && (user.WebDAVPermission == model.WebDAVNone || (scope == "webdav:write" && user.WebDAVPermission != model.WebDAVWrite)) {
			return apperrors.ErrForbidden
		}
	}
	return nil
}

func CheckApplicationOwner(ctx context.Context, id int64, scopes []string) (*model.UserAccount, error) {
	if !DatabaseMode() {
		return nil, nil
	}
	user, err := GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = ValidateApplicationScopes(user, scopes); err != nil {
		return nil, err
	}
	return user, nil
}

// LockApplicationOwnerTx 按需锁定应用开关，再锁定所有者账号，复核认证版本及请求范围对应的账号权限。
func LockApplicationOwnerTx(tx *gorm.DB, id int64, scopes []string, requireEnabled bool, authVersions ...int64) error {
	if !DatabaseMode() {
		return nil
	}
	if requireEnabled {
		enabled, err := EnabledTx(tx, "auth", "applications_enabled", true)
		if err != nil {
			return err
		}
		if !enabled {
			return apperrors.ErrForbidden
		}
	}
	user, err := dal.QueryAccount(tx, id, true)
	if err != nil {
		return normalizeError(err)
	}
	if user != nil && len(authVersions) > 0 && authVersions[0] > 0 && user.AuthVersion != authVersions[0] {
		return apperrors.ErrUnauthorized
	}
	return ValidateApplicationScopes(user, scopes)
}

// RequireApplicationSnapshotTx must run after configuration and owner locks.
// It locks the application and compares the originally authenticated snapshot,
// so rotate, revoke and disable-enable cannot revive an in-flight write.
func RequireApplicationSnapshotTx(tx *gorm.DB, principal *utils.Principal, scope string) error {
	if !DatabaseMode() {
		return nil
	}
	if principal == nil || principal.Kind != "application" || principal.UserID <= 0 || principal.ApplicationID <= 0 || principal.ApplicationRevision <= 0 || principal.SecretVersion <= 0 {
		return apperrors.ErrUnauthorized
	}
	application, err := dal.LockApplicationByID(tx, principal.ApplicationID)
	if err != nil {
		return normalizeError(err)
	}
	now := time.Now()
	if application == nil || application.OwnerUserID != principal.UserID || application.Status != model.ApplicationStatusEnabled || application.Revision != principal.ApplicationRevision || application.SecretVersion != principal.SecretVersion || !application.ExpiresAt.After(now) || principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(now) {
		return apperrors.ErrUnauthorized
	}
	current := &utils.Principal{Scopes: application.Scopes}
	if !principal.Allows(scope) || !current.Allows(scope) {
		return apperrors.ErrForbidden
	}
	return nil
}

// RequireLibraryWriteTx 在图书写事务内锁定模块开关和账号，复核会话有效期；应用身份还需验证凭据快照。
func RequireLibraryWriteTx(tx *gorm.DB, principal *utils.Principal) error {
	if !DatabaseMode() {
		return nil
	}
	if principal == nil || (principal.Kind != "user" && principal.Kind != "application") || principal.UserID <= 0 {
		return apperrors.ErrUnauthorized
	}
	if principal.Kind == "application" {
		enabled, err := EnabledTx(tx, "auth", "applications_enabled", true)
		if err != nil {
			return err
		}
		if !enabled {
			return apperrors.ErrForbidden
		}
	}
	enabled, err := EnabledTx(tx, "library", "enabled", true)
	if err != nil {
		return err
	}
	if !enabled {
		return apperrors.ErrForbidden
	}
	user, err := dal.QueryAccount(tx, principal.UserID, true)
	if err != nil {
		return normalizeError(err)
	}
	if user == nil || user.Status != model.AccountActive || user.MustChangePassword || principal.AuthVersion <= 0 || user.AuthVersion != principal.AuthVersion || principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now()) {
		return apperrors.ErrUnauthorized
	}
	if !user.LibraryEnabled {
		return apperrors.ErrForbidden
	}
	if principal.Kind == "application" {
		return RequireApplicationSnapshotTx(tx, principal, "library:write")
	}
	return nil
}
