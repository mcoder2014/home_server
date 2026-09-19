package manuals

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

// AddReservedFile consumes a file body after the API layer has validated the
// manual owner and reserved worst-case disk space for this upload.
func (application *Application) AddReservedFile(ctx context.Context, ownerID, manualID int64, title, requestID, originalName string, source io.Reader, principal *utils.Principal) (*service.ItemMutationResult, error) {
	title, err := service.ValidateFileMetadata(title, requestID)
	if err != nil {
		return nil, err
	}
	conf := config.Global().Manuals
	itemID, err := newID()
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	storedFile, err := service.StoreFile(&conf, ownerID, manualID, itemID, originalName, source)
	if err != nil {
		return nil, err
	}
	hash := inputHash(struct{ Kind, Title, OriginalName, SHA256 string }{storedFile.Kind.String(), title, storedFile.OriginalName, storedFile.SHA256})
	item := &model.ManualItem{ID: itemID, ManualID: manualID, Kind: storedFile.Kind, Title: title, Text: storedFile.Text, StorageKey: stringPointer(storedFile.StorageKey), ThumbnailKey: stringPointer(storedFile.ThumbnailKey), ContentType: storedFile.ContentType, OriginalName: storedFile.OriginalName, SizeBytes: storedFile.SizeBytes, SHA256: storedFile.SHA256, PreviewStatus: storedFile.PreviewStatus, ClientRequestID: requestID, RequestHash: hash, CreateTime: time.Now()}
	database, err := database(ctx)
	if err != nil {
		if storedFile.StorageKey != "" {
			_ = service.RemoveStoredFile(&conf, ownerID, manualID, itemID)
		}
		return nil, err
	}
	result, revision, appendErr := application.appendItem(ctx, database, ownerID, item, principal)
	if appendErr == nil {
		if result.ID != itemID && storedFile.StorageKey != "" {
			_ = service.RemoveStoredFile(&conf, ownerID, manualID, itemID)
		}
		return &service.ItemMutationResult{Item: itemView(result), Revision: revision}, nil
	}
	return application.resolveFileCommit(ctx, database, &conf, ownerID, manualID, item, appendErr)
}

func (application *Application) resolveFileCommit(ctx context.Context, database *gorm.DB, conf *config.ManualsConfig, ownerID, manualID int64, item *model.ManualItem, appendErr error) (*service.ItemMutationResult, error) {
	if !errors.Is(appendErr, apperrors.ErrDependency) {
		if item.StorageKey != nil {
			_ = service.RemoveStoredFile(conf, ownerID, manualID, item.ID)
		}
		return nil, appendErr
	}
	existing, queryErr := dal.FindManualItemByRequestID(database.WithContext(ctx), manualID, item.ClientRequestID, false)
	if queryErr != nil {
		// The transaction outcome cannot be proven. Keep the generated directory
		// so a committed row can never be left pointing at deleted bytes.
		return nil, apperrors.ErrDependency
	}
	if existing == nil {
		if item.StorageKey != nil {
			_ = service.RemoveStoredFile(conf, ownerID, manualID, item.ID)
		}
		return nil, appendErr
	}
	if existing.ID != item.ID {
		if item.StorageKey != nil {
			_ = service.RemoveStoredFile(conf, ownerID, manualID, item.ID)
		}
		return nil, appendErr
	}
	if existing.DeletedAt != nil || existing.RequestHash != item.RequestHash {
		return nil, apperrors.ErrConflict
	}
	revision, err := application.currentRevision(ctx, manualID)
	if err != nil {
		return nil, err
	}
	return &service.ItemMutationResult{Item: itemView(existing), Revision: revision}, nil
}
