package manuals

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func (application *Application) PasswordState(ctx context.Context, ownerID, manualID int64) (*resourcepasswords.State, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	manual, err := dal.FindManual(database, manualID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if manual == nil || manual.OwnerUserID != ownerID || manual.Status == model.ManualStatusDeleted {
		return nil, apperrors.ErrNotFound
	}
	return resourcepasswords.Default.State(ctx, resourcepasswords.ResourceManual, manualID)
}

func (application *Application) SetPassword(ctx context.Context, ownerID, manualID int64, input resourcepasswords.UpdateInput, principal *utils.Principal) (*resourcepasswords.State, error) {
	if input.Version < 0 {
		return nil, apperrors.ErrInvalid
	}
	passwordHash, err := resourcepasswords.PrepareManagedPassword(input.Password)
	if err != nil {
		return nil, err
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var state *resourcepasswords.State
	unlock := application.lockOwner(ownerID)
	defer unlock()
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(ctx, tx, ownerID, principal); err != nil {
			return err
		}
		manual, err := dal.FindManual(tx, manualID, true)
		if err != nil {
			return err
		}
		if manual == nil || manual.OwnerUserID != ownerID || manual.Status == model.ManualStatusDeleted {
			return apperrors.ErrNotFound
		}
		if err := checkWriteExpiry(principal); err != nil {
			return err
		}
		state, err = resourcepasswords.SetTx(tx, resourcepasswords.ResourceManual, manualID, input.Version, passwordHash)
		return err
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	return state, nil
}
