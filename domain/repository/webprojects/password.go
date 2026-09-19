package webprojects

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

// SetResourcePassword serializes password version changes behind the owning
// project row and revalidates the exact browser session before committing.
func (repository *Repository) SetResourcePassword(ctx context.Context, ownerUserID, projectID, expectedVersion int64, passwordHash string, principal *utils.Principal) (*resourcepasswords.State, error) {
	if db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	var state *resourcepasswords.State
	err := db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		principals := []*utils.Principal{principal}
		if err := requireWritePolicy(tx, ownerUserID, principals, nil); err != nil {
			return err
		}
		if principal == nil || principal.UserID != ownerUserID {
			return apperrors.ErrUnauthorized
		}
		if principal.Kind == "user" {
			if accounts.DatabaseMode() {
				user, err := accounts.RequireUserSessionTx(tx, utils.GetTokenFromCtx(ctx), ownerUserID)
				if err != nil {
					return err
				}
				if user.AuthVersion != principal.AuthVersion {
					return apperrors.ErrUnauthorized
				}
			} else if err := passport.RequireConfigSessionTx(ctx, tx, utils.GetTokenFromCtx(ctx), ownerUserID, principal.AuthVersion); err != nil {
				return err
			}
		}
		project, err := dal.LockOwnedWebProject(tx, ownerUserID, projectID)
		if err != nil {
			return err
		}
		if project == nil || project.Status == model.WebProjectStatusDeleted {
			return apperrors.ErrNotFound
		}
		if project.ModerationStatus != "" && project.ModerationStatus != "normal" {
			return apperrors.ErrForbidden
		}
		if err := checkWriteExpiry(principals); err != nil {
			return err
		}
		state, err = resourcepasswords.SetTx(tx, resourcepasswords.ResourceWebProject, projectID, expectedVersion, passwordHash)
		return err
	})
	return state, err
}
