package manuals

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
)

func (application *Application) ResourceItem(ctx context.Context, manualID, itemID, viewerID int64, thumbnail bool) (*model.ManualItem, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	manual, err := dal.FindManual(database, manualID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if manual == nil {
		return nil, apperrors.ErrNotFound
	}
	active, err := ownerActive(ctx, manual.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if !active || !service.CanReadManual(manual.AccessMode, manual.Status, manual.OwnerUserID, viewerID) {
		return nil, apperrors.ErrNotFound
	}
	item, err := dal.FindManualItem(database, manualID, itemID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if item == nil || item.StorageKey == nil {
		return nil, apperrors.ErrNotFound
	}
	if thumbnail && (item.ThumbnailKey == nil || item.PreviewStatus != model.ManualPreviewReady) {
		return nil, apperrors.ErrNotFound
	}
	return item, nil
}
