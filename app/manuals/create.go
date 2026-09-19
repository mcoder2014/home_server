package manuals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func (application *Application) Create(ctx context.Context, ownerID int64, input service.CreateManualInput, principal *utils.Principal) (*service.ManualView, error) {
	name, description, access, err := service.ValidateManualFields(input.Name, input.Description, input.AccessMode)
	if err != nil || !service.ValidRequestID(input.ClientRequestID) {
		return nil, apperrors.ErrInvalid
	}
	categories, err := service.ValidateCategories(input.Categories)
	if err != nil {
		return nil, err
	}
	hash := inputHash(struct {
		Name        string
		Description string
		Categories  []string
		AccessMode  string
	}{name, description, categories, access.String()})
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	manualID, err := newID()
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	now := time.Now()
	manual := &model.Manual{ID: manualID, OwnerUserID: ownerID, Name: name, Description: description, AccessMode: access, Status: model.ManualStatusDraft, Revision: 1, ClientRequestID: input.ClientRequestID, RequestHash: hash, CreateTime: now, UpdateTime: now}
	replayed := false
	unlock := application.lockOwner(ownerID)
	defer unlock()
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(ctx, tx, ownerID, principal); err != nil {
			return err
		}
		existing, err := dal.FindManualByRequestID(tx, ownerID, input.ClientRequestID, true)
		if err != nil {
			return err
		}
		if existing != nil {
			manual = existing
			if existing.RequestHash != hash || existing.Status == model.ManualStatusDeleted {
				return apperrors.ErrConflict
			}
			replayed = true
			return nil
		}
		count, err := dal.CountUserManuals(tx, ownerID)
		if err != nil {
			return err
		}
		if count >= int64(config.Global().Manuals.MaxManualsPerUser) {
			return apperrors.ErrRateLimited
		}
		if err := checkWriteExpiry(principal); err != nil {
			return err
		}
		if err := dal.InsertManual(tx, manual); err != nil {
			return err
		}
		return dal.InsertManualCategories(tx, manual.ID, categories)
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	if replayed {
		return application.Get(ctx, manual.ID, ownerID, principal)
	}
	return manualView(manual, categories, 0, nil, []*model.ManualItem{}, principal), nil
}

func inputHash(value interface{}) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func newID() (int64, error) {
	for {
		var data [8]byte
		if _, err := rand.Read(data[:]); err != nil {
			return 0, err
		}
		id := int64(binary.BigEndian.Uint64(data[:]) & uint64(^uint64(0)>>1))
		if id > 0 {
			return id, nil
		}
	}
}
