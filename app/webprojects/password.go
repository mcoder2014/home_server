package webprojects

import (
	"context"
	"fmt"

	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
)

func (application *Application) PasswordState(ctx context.Context, ownerUserID, projectID int64) (*resourcepasswords.State, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil {
		return nil, service.ErrNotFound
	}
	return resourcepasswords.Default.State(ctx, resourcepasswords.ResourceWebProject, projectID)
}

func (application *Application) SetPassword(ctx context.Context, ownerUserID, projectID int64, input resourcepasswords.UpdateInput, principal *utils.Principal) (*resourcepasswords.State, error) {
	if input.Version < 0 {
		return nil, apperrors.ErrInvalid
	}
	passwordHash, err := resourcepasswords.PrepareManagedPassword(input.Password)
	if err != nil {
		return nil, err
	}
	state, err := application.repository.SetResourcePassword(ctx, ownerUserID, projectID, input.Version, passwordHash, principal)
	if err != nil {
		return nil, projectPersistenceError(err, "set project password")
	}
	return state, nil
}
