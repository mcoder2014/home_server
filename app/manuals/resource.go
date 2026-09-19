package manuals

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	apperrors "github.com/mcoder2014/home_server/errors"
)

func (application *Application) ResourceItem(ctx context.Context, manualID, itemID, viewerID int64, thumbnail bool, grantTokens ...string) (*model.ManualItem, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	manual, err := findReadableManual(ctx, database, manualID, viewerID)
	if err != nil {
		return nil, err
	}
	grantToken := ""
	if len(grantTokens) > 0 {
		grantToken = grantTokens[0]
	}
	if _, err := resourcepasswords.Default.Authorize(ctx, resourcepasswords.ResourceManual, manual.ID, viewerID == manual.OwnerUserID, grantToken); err != nil {
		return nil, err
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
