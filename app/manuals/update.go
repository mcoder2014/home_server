package manuals

import (
	"context"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func (application *Application) Update(ctx context.Context, ownerID, manualID int64, input service.UpdateManualInput, principal *utils.Principal) (*service.ManualView, error) {
	if input.Revision <= 0 || !hasManualPatch(input) {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
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
		if manual.Revision != input.Revision {
			return apperrors.ErrConflict
		}
		return applyManualPatch(tx, manual, input, principal)
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	return application.Get(ctx, manualID, ownerID, principal)
}

func hasManualPatch(input service.UpdateManualInput) bool {
	return input.Name != nil || input.Description != nil || input.Categories != nil || input.AccessMode != nil || input.Status != nil || input.CoverItemID.Set || input.ItemIDs != nil
}

func applyManualPatch(tx *gorm.DB, manual *model.Manual, input service.UpdateManualInput, principal *utils.Principal) error {
	name, description, accessMode := manual.Name, manual.Description, manual.AccessMode.String()
	if input.Name != nil {
		name = *input.Name
	}
	if input.Description != nil {
		description = *input.Description
	}
	if input.AccessMode != nil {
		accessMode = *input.AccessMode
	}
	name, description, access, err := service.ValidateManualFields(name, description, accessMode)
	if err != nil {
		return err
	}
	var categories []string
	if input.Categories != nil {
		categories, err = service.ValidateCategories(*input.Categories)
		if err != nil {
			return err
		}
	}
	fields := map[string]interface{}{"name": name, "description": description, "access_mode": access, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()}
	items, err := patchItems(tx, manual, input, fields)
	if err != nil {
		return err
	}
	if input.Status != nil {
		status, ok := model.ParseManualStatus(*input.Status)
		if !ok || status == model.ManualStatusDeleted || (status != manual.Status && !(manual.Status == model.ManualStatusDraft && status == model.ManualStatusActive)) {
			return apperrors.ErrInvalid
		}
		if status == model.ManualStatusActive && len(items) == 0 {
			return apperrors.ErrUnprocessable
		}
		fields["status"] = status
	}
	if err := checkWriteExpiry(principal); err != nil {
		return err
	}
	updated, err := dal.UpdateManualFieldsCAS(tx, manual.OwnerUserID, manual.ID, manual.Revision, fields)
	if err != nil {
		return err
	}
	if !updated {
		return apperrors.ErrConflict
	}
	if input.Categories != nil {
		return dal.ReplaceManualCategories(tx, manual.ID, categories)
	}
	return nil
}

func patchItems(tx *gorm.DB, manual *model.Manual, input service.UpdateManualInput, fields map[string]interface{}) ([]*model.ManualItem, error) {
	needItems := input.ItemIDs != nil || input.CoverItemID.Set || (input.Status != nil && *input.Status == "active")
	if !needItems {
		return nil, nil
	}
	items, err := dal.ListManualItems(tx, manual.ID, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*model.ManualItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	if input.ItemIDs != nil {
		ordered, err := parseCompleteItemOrder(*input.ItemIDs, byID)
		if err != nil {
			return nil, err
		}
		positions := make(map[int64]int)
		for index, id := range ordered {
			if byID[id].Position != index+1 {
				positions[id] = index + 1
			}
		}
		updated, err := dal.UpdateManualItemPositions(tx, manual.ID, positions)
		if err != nil {
			return nil, err
		}
		if !updated {
			return nil, apperrors.ErrConflict
		}
	}
	if input.CoverItemID.Set {
		if input.CoverItemID.Value == nil {
			fields["cover_item_id"] = nil
		} else {
			coverID, err := parsePositiveID(*input.CoverItemID.Value)
			if err != nil || byID[coverID] == nil {
				return nil, apperrors.ErrInvalid
			}
			fields["cover_item_id"] = coverID
		}
	}
	return items, nil
}

func parseCompleteItemOrder(values []string, items map[int64]*model.ManualItem) ([]int64, error) {
	if len(values) != len(items) {
		return nil, apperrors.ErrInvalid
	}
	ordered, seen := make([]int64, 0, len(values)), make(map[int64]bool, len(values))
	for _, value := range values {
		id, err := parsePositiveID(value)
		if err != nil || seen[id] || items[id] == nil {
			return nil, apperrors.ErrInvalid
		}
		seen[id] = true
		ordered = append(ordered, id)
	}
	return ordered, nil
}

func (application *Application) Delete(ctx context.Context, ownerID, manualID, revision int64, principal *utils.Principal) (int64, error) {
	if revision <= 0 {
		return 0, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return 0, err
	}
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
		if manual.Revision != revision {
			return apperrors.ErrConflict
		}
		if err := checkWriteExpiry(principal); err != nil {
			return err
		}
		updated, err := dal.UpdateManualFieldsCAS(tx, ownerID, manualID, revision, map[string]interface{}{"status": model.ManualStatusDeleted, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
		if err != nil {
			return err
		}
		if !updated {
			return apperrors.ErrConflict
		}
		return nil
	})
	if err != nil {
		return 0, persistenceError(err)
	}
	return revision + 1, nil
}

func (application *Application) DeleteItem(ctx context.Context, ownerID, manualID, itemID, revision int64, principal *utils.Principal) (*service.DeleteItemResult, error) {
	if revision <= 0 {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var coverID *int64
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
		if manual.Revision != revision {
			return apperrors.ErrConflict
		}
		items, err := dal.ListManualItems(tx, manualID, true)
		if err != nil {
			return err
		}
		item := findItem(items, itemID)
		if item == nil {
			return apperrors.ErrNotFound
		}
		if manual.Status == model.ManualStatusActive && len(items) == 1 {
			return apperrors.ErrUnprocessable
		}
		now := time.Now()
		if err := dal.DeleteManualItem(tx, manualID, itemID, now); err != nil {
			return err
		}
		if err := dal.CompactManualItemPositions(tx, manualID, item.Position); err != nil {
			return err
		}
		coverID = manual.CoverItemID
		fields := map[string]interface{}{"revision": gorm.Expr("revision + 1"), "update_time": now}
		if coverID != nil && *coverID == itemID {
			coverID = nil
			fields["cover_item_id"] = nil
		}
		if err := checkWriteExpiry(principal); err != nil {
			return err
		}
		updated, err := dal.UpdateManualFieldsCAS(tx, ownerID, manualID, revision, fields)
		if err != nil {
			return err
		}
		if !updated {
			return apperrors.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	result := &service.DeleteItemResult{Revision: revision + 1}
	if coverID != nil {
		value := strconv.FormatInt(*coverID, 10)
		result.CoverItemID = &value
	}
	return result, nil
}

func findItem(items []*model.ManualItem, id int64) *model.ManualItem {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

func parsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, apperrors.ErrInvalid
	}
	return id, nil
}
