package manuals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func (application *Application) AddInline(ctx context.Context, ownerID, manualID int64, input service.InlineItemInput, principal *utils.Principal) (*service.ItemMutationResult, error) {
	validated, err := service.ValidateInlineItem(input)
	if err != nil {
		return nil, err
	}
	itemID, err := newID()
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	body := validated.Text
	if validated.Kind == model.ManualItemURL {
		body = validated.URL
	}
	digest := sha256.Sum256([]byte(body))
	hash := inputHash(struct{ Kind, Title, Text, URL string }{validated.Kind.String(), validated.Title, validated.Text, validated.URL})
	item := &model.ManualItem{ID: itemID, ManualID: manualID, Kind: validated.Kind, Title: validated.Title, Text: validated.Text, URL: validated.URL, SizeBytes: int64(len(validated.Text)), SHA256: hex.EncodeToString(digest[:]), PreviewStatus: model.ManualPreviewNone, ClientRequestID: validated.ClientRequestID, RequestHash: hash, CreateTime: time.Now()}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	stored, revision, err := application.appendItem(ctx, database, ownerID, item, principal)
	if err != nil {
		return nil, err
	}
	return &service.ItemMutationResult{Item: itemView(stored), Revision: revision}, nil
}

func (application *Application) appendItem(ctx context.Context, database *gorm.DB, ownerID int64, item *model.ManualItem, principal *utils.Principal) (*model.ManualItem, int64, error) {
	var stored *model.ManualItem
	var revision int64
	unlock := application.lockOwner(ownerID)
	defer unlock()
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(ctx, tx, ownerID, principal); err != nil {
			return err
		}
		manual, err := dal.FindManual(tx, item.ManualID, true)
		if err != nil {
			return err
		}
		if manual == nil || manual.OwnerUserID != ownerID || manual.Status == model.ManualStatusDeleted {
			return apperrors.ErrNotFound
		}
		existing, err := dal.FindManualItemByRequestID(tx, item.ManualID, item.ClientRequestID, true)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.DeletedAt != nil || existing.RequestHash != item.RequestHash {
				return apperrors.ErrConflict
			}
			stored, revision = existing, manual.Revision
			return nil
		}
		if err := checkItemQuota(tx, ownerID, item.ManualID, item.SizeBytes); err != nil {
			return err
		}
		count, err := dal.CountActiveManualItems(tx, item.ManualID)
		if err != nil {
			return err
		}
		item.Position = int(count) + 1
		if err := checkWriteExpiry(principal); err != nil {
			return err
		}
		if err := dal.InsertManualItem(tx, item); err != nil {
			return err
		}
		updated, err := dal.UpdateManualFieldsCAS(tx, ownerID, item.ManualID, manual.Revision, map[string]interface{}{"revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
		if err != nil {
			return err
		}
		if !updated {
			return apperrors.ErrConflict
		}
		stored, revision = item, manual.Revision+1
		return nil
	})
	if err != nil {
		return nil, 0, persistenceError(err)
	}
	return stored, revision, nil
}

func checkItemQuota(tx *gorm.DB, ownerID, manualID, addition int64) error {
	limits := config.Global().Manuals
	count, err := dal.CountActiveManualItems(tx, manualID)
	if err != nil {
		return err
	}
	if count >= int64(limits.MaxItemsPerManual) {
		return apperrors.ErrRateLimited
	}
	manualBytes, err := dal.SumManualItemBytes(tx, manualID)
	if err != nil {
		return err
	}
	userBytes, err := dal.SumUserManualItemBytes(tx, ownerID)
	if err != nil {
		return err
	}
	if addition < 0 || manualBytes > math.MaxInt64-addition || userBytes > math.MaxInt64-addition {
		return apperrors.ErrDependency
	}
	if manualBytes+addition > limits.MaxManualBytes || userBytes+addition > limits.MaxUserBytes {
		return apperrors.ErrRateLimited
	}
	return nil
}

func (application *Application) CheckUploadOwner(ctx context.Context, ownerID, manualID int64) error {
	database, err := database(ctx)
	if err != nil {
		return err
	}
	manual, err := dal.FindManual(database, manualID, false)
	if err != nil {
		return apperrors.ErrDependency
	}
	if manual == nil || manual.OwnerUserID != ownerID || manual.Status == model.ManualStatusDeleted {
		return apperrors.ErrNotFound
	}
	return nil
}

func (application *Application) AcquireUpload(conf *config.ManualsConfig) (func(), error) {
	if conf == nil || !conf.Enabled || conf.MaxFileBytes <= 0 || conf.PDFPreviewOutputLimitBytes <= 0 {
		return nil, apperrors.ErrDependency
	}
	reservation := uint64(conf.MaxFileBytes)
	preview := uint64(conf.PDFPreviewOutputLimitBytes)
	if reservation > math.MaxUint64-preview {
		return nil, apperrors.ErrDependency
	}
	reservation += preview
	application.uploads.Lock()
	defer application.uploads.Unlock()
	free, err := application.diskFree(conf.StorageRoot)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	minimum := uint64(conf.MinFreeDiskBytes)
	if minimum > free || application.uploads.reservedBytes > free-minimum || reservation > free-minimum-application.uploads.reservedBytes {
		return nil, apperrors.ErrRateLimited
	}
	application.uploads.reservedBytes += reservation
	released := false
	return func() {
		application.uploads.Lock()
		defer application.uploads.Unlock()
		if released {
			return
		}
		released = true
		application.uploads.reservedBytes -= reservation
	}, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func (application *Application) currentRevision(ctx context.Context, manualID int64) (int64, error) {
	database, err := database(ctx)
	if err != nil {
		return 0, err
	}
	manual, err := dal.FindManual(database, manualID, false)
	if err != nil || manual == nil {
		return 0, apperrors.ErrDependency
	}
	return manual.Revision, nil
}
