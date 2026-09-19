package dal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ManualTable         = "manuals"
	ManualCategoryTable = "manual_categories"
	ManualItemTable     = "manual_items"
)

var manualColumns = []string{"id", "owner_user_id", "name", "description", "access_mode", "status", "cover_item_id", "revision", "client_request_id", "request_hash", "create_time", "update_time"}
var manualItemColumns = []string{"id", "manual_id", "kind", "title", "text", "url", "storage_key", "thumbnail_key", "content_type", "original_name", "size_bytes", "sha256", "position", "preview_status", "client_request_id", "request_hash", "deleted_at", "create_time"}

type ManualListFilter struct {
	ViewerUserID       int64
	Mine               bool
	Cursor             int64
	Limit              int
	Query              string
	Category           string
	CategorySet        bool
	RequireActiveOwner bool
	ActiveOwnerIDs     []int64
}

type ManualItemCount struct {
	ManualID int64 `gorm:"column:manual_id"`
	Count    int   `gorm:"column:item_count"`
}

type ManualItemPreview struct {
	ID            int64                     `gorm:"column:id"`
	ManualID      int64                     `gorm:"column:manual_id"`
	Kind          model.ManualItemKind      `gorm:"column:kind"`
	Title         string                    `gorm:"column:title"`
	TextExcerpt   string                    `gorm:"column:text_excerpt"`
	URL           string                    `gorm:"column:url"`
	ThumbnailKey  *string                   `gorm:"column:thumbnail_key"`
	PreviewStatus model.ManualPreviewStatus `gorm:"column:preview_status"`
	Position      int                       `gorm:"column:position"`
}

func InsertManual(tx *gorm.DB, manual *model.Manual) error {
	return tx.Table(ManualTable).Create(manual).Error
}

func InsertManualCategories(tx *gorm.DB, manualID int64, categories []string) error {
	if len(categories) == 0 {
		return nil
	}
	rows := make([]*model.ManualCategory, 0, len(categories))
	for _, category := range categories {
		rows = append(rows, &model.ManualCategory{ManualID: manualID, Category: category})
	}
	return tx.Table(ManualCategoryTable).Select("manual_id", "category").Create(&rows).Error
}

func ReplaceManualCategories(tx *gorm.DB, manualID int64, categories []string) error {
	if err := tx.Table(ManualCategoryTable).Where("manual_id = ?", manualID).Delete(&model.ManualCategory{}).Error; err != nil {
		return err
	}
	return InsertManualCategories(tx, manualID, categories)
}

func FindManual(database *gorm.DB, id int64, lock bool) (*model.Manual, error) {
	query := database.Table(ManualTable).Select(manualColumns).Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var manual model.Manual
	err := query.Take(&manual).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &manual, err
}

func FindManualByRequestID(database *gorm.DB, ownerID int64, requestID string, lock bool) (*model.Manual, error) {
	query := database.Table(ManualTable).Select(manualColumns).Where("owner_user_id = ? AND client_request_id = ?", ownerID, requestID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var manual model.Manual
	err := query.Take(&manual).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &manual, err
}

func ListManuals(database *gorm.DB, filter ManualListFilter) ([]*model.Manual, error) {
	query := visibleManualQuery(database, filter).Select(prefixedColumns("m", manualColumns))
	if filter.Cursor > 0 {
		query = query.Where("m.id < ?", filter.Cursor)
	}
	if filter.CategorySet {
		if filter.Category == "" {
			query = query.Where("NOT EXISTS (SELECT 1 FROM " + ManualCategoryTable + " AS filtered_category WHERE filtered_category.manual_id = m.id)")
		} else {
			query = query.Where("EXISTS (SELECT 1 FROM "+ManualCategoryTable+" AS filtered_category WHERE filtered_category.manual_id = m.id AND filtered_category.category = ?)", filter.Category)
		}
	}
	if filter.Query != "" {
		query = query.Where("m.name LIKE ? ESCAPE '!'", ManualNameContainsPattern(filter.Query))
	}
	var manuals []*model.Manual
	err := query.Order("m.id DESC").Limit(filter.Limit).Find(&manuals).Error
	return manuals, err
}

func ListManualCategories(database *gorm.DB, filter ManualListFilter, limit int) ([]string, error) {
	return ListManualCategoriesAfter(database, filter, "", limit)
}

func ListManualCategoriesAfter(database *gorm.DB, filter ManualListFilter, cursor string, limit int) ([]string, error) {
	var categories []string
	query := visibleManualQuery(database, filter).Joins("JOIN " + ManualCategoryTable + " AS mc ON mc.manual_id = m.id")
	if cursor != "" {
		query = query.Where("mc.category > ?", cursor)
	}
	err := query.Distinct("mc.category").Order("mc.category ASC").Limit(limit).Pluck("mc.category", &categories).Error
	return categories, err
}

func ListManualCategoriesByManualIDs(database *gorm.DB, manualIDs []int64) ([]*model.ManualCategory, error) {
	if len(manualIDs) == 0 {
		return []*model.ManualCategory{}, nil
	}
	var categories []*model.ManualCategory
	err := database.Table(ManualCategoryTable).Select("manual_id", "category").Where("manual_id IN ?", manualIDs).Order("manual_id ASC, category ASC").Find(&categories).Error
	return categories, err
}

func visibleManualQuery(database *gorm.DB, filter ManualListFilter) *gorm.DB {
	query := database.Table(ManualTable + " AS m")
	if filter.RequireActiveOwner {
		query = query.Joins("JOIN "+AccountTable+" AS owner_account ON owner_account.id = m.owner_user_id AND owner_account.status = ? AND owner_account.must_change_password = ?", model.AccountActive, false)
	} else if filter.ActiveOwnerIDs != nil {
		query = query.Where("m.owner_user_id IN ?", filter.ActiveOwnerIDs)
	}
	if filter.Mine {
		return query.Where("m.owner_user_id = ? AND m.status <> ?", filter.ViewerUserID, model.ManualStatusDeleted)
	}
	if filter.ViewerUserID > 0 {
		return query.Where("(m.owner_user_id = ? AND m.status <> ?) OR (m.status = ? AND m.access_mode IN ?)", filter.ViewerUserID, model.ManualStatusDeleted, model.ManualStatusActive, []model.ManualAccess{model.ManualAccessAuthenticated, model.ManualAccessPublic})
	}
	return query.Where("m.status = ? AND m.access_mode = ?", model.ManualStatusActive, model.ManualAccessPublic)
}

func ManualNameContainsPattern(value string) string {
	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + replacer.Replace(value) + "%"
}

func ListManualItemCounts(database *gorm.DB, manualIDs []int64) ([]ManualItemCount, error) {
	if len(manualIDs) == 0 {
		return []ManualItemCount{}, nil
	}
	var counts []ManualItemCount
	err := database.Table(ManualItemTable).Select("manual_id, COUNT(*) AS item_count").Where("manual_id IN ? AND deleted_at IS NULL", manualIDs).Group("manual_id").Find(&counts).Error
	return counts, err
}

func ListManualItemsByIDs(database *gorm.DB, itemIDs []int64) ([]*model.ManualItem, error) {
	if len(itemIDs) == 0 {
		return []*model.ManualItem{}, nil
	}
	var items []*model.ManualItem
	err := database.Table(ManualItemTable).Select(manualItemColumns).Where("id IN ? AND deleted_at IS NULL", itemIDs).Find(&items).Error
	return items, err
}

func ListManualItemPreviewsByIDs(database *gorm.DB, itemIDs []int64) ([]*ManualItemPreview, error) {
	if len(itemIDs) == 0 {
		return []*ManualItemPreview{}, nil
	}
	var items []*ManualItemPreview
	err := database.Table(ManualItemTable).Select("id", "manual_id", "kind", "title", "LEFT(text, 160) AS text_excerpt", "url", "thumbnail_key", "preview_status", "position").Where("id IN ? AND deleted_at IS NULL", itemIDs).Find(&items).Error
	return items, err
}

// ListAutomaticManualCovers returns one bounded preview per manual. The window
// ranks ready image/PDF thumbnails first, then preserves the user's item order.
func ListAutomaticManualCovers(database *gorm.DB, manualIDs []int64) ([]*ManualItemPreview, error) {
	if len(manualIDs) == 0 {
		return []*ManualItemPreview{}, nil
	}
	selectSQL := "id, manual_id, kind, title, LEFT(text, 160) AS text_excerpt, url, thumbnail_key, preview_status, position, " +
		"ROW_NUMBER() OVER (PARTITION BY manual_id ORDER BY CASE WHEN kind IN (?, ?) AND preview_status = ? THEN 0 ELSE 1 END, position ASC, id ASC) AS cover_rank"
	ranked := database.Table(ManualItemTable).Select(selectSQL, model.ManualItemImage, model.ManualItemPDF, model.ManualPreviewReady).Where("manual_id IN ? AND deleted_at IS NULL", manualIDs)
	var items []*ManualItemPreview
	err := database.Table("(?) AS ranked_manual_items", ranked).Select("id", "manual_id", "kind", "title", "text_excerpt", "url", "thumbnail_key", "preview_status", "position").Where("cover_rank = 1").Find(&items).Error
	return items, err
}

func ListManualItems(database *gorm.DB, manualID int64, lock bool) ([]*model.ManualItem, error) {
	query := database.Table(ManualItemTable).Select(manualItemColumns).Where("manual_id = ? AND deleted_at IS NULL", manualID).Order("position ASC, id ASC")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var items []*model.ManualItem
	err := query.Find(&items).Error
	return items, err
}

func FindManualItem(database *gorm.DB, manualID, itemID int64, lock bool) (*model.ManualItem, error) {
	query := database.Table(ManualItemTable).Select(manualItemColumns).Where("manual_id = ? AND id = ? AND deleted_at IS NULL", manualID, itemID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var item model.ManualItem
	err := query.Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func FindManualItemByRequestID(database *gorm.DB, manualID int64, requestID string, lock bool) (*model.ManualItem, error) {
	query := database.Table(ManualItemTable).Select(manualItemColumns).Where("manual_id = ? AND client_request_id = ?", manualID, requestID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var item model.ManualItem
	err := query.Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func InsertManualItem(tx *gorm.DB, item *model.ManualItem) error {
	return tx.Table(ManualItemTable).Create(item).Error
}

func CountUserManuals(tx *gorm.DB, ownerID int64) (int64, error) {
	var count int64
	err := tx.Table(ManualTable).Where("owner_user_id = ? AND status <> ?", ownerID, model.ManualStatusDeleted).Count(&count).Error
	return count, err
}

func CountActiveManualItems(tx *gorm.DB, manualID int64) (int64, error) {
	var count int64
	err := tx.Table(ManualItemTable).Where("manual_id = ? AND deleted_at IS NULL", manualID).Count(&count).Error
	return count, err
}

func SumManualItemBytes(tx *gorm.DB, manualID int64) (int64, error) {
	return sumItemBytes(tx.Table(ManualItemTable).Where("manual_id = ?", manualID), "size_bytes")
}

func SumUserManualItemBytes(tx *gorm.DB, ownerID int64) (int64, error) {
	query := tx.Table(ManualItemTable+" AS i").Joins("JOIN "+ManualTable+" AS m ON m.id = i.manual_id").Where("m.owner_user_id = ?", ownerID)
	return sumItemBytes(query, "i.size_bytes")
}

func sumItemBytes(query *gorm.DB, column string) (int64, error) {
	var result struct {
		Bytes       int64 `gorm:"column:bytes"`
		InvalidRows int64 `gorm:"column:invalid_rows"`
	}
	err := query.Select("COALESCE(SUM(" + column + "),0) AS bytes, COALESCE(SUM(CASE WHEN " + column + " < 0 THEN 1 ELSE 0 END),0) AS invalid_rows").Scan(&result).Error
	if err != nil {
		return 0, err
	}
	if result.Bytes < 0 || result.InvalidRows != 0 {
		return 0, fmt.Errorf("invalid recorded manual item size")
	}
	return result.Bytes, nil
}

func UpdateManualFieldsCAS(tx *gorm.DB, ownerID, manualID, revision int64, fields map[string]interface{}) (bool, error) {
	result := tx.Table(ManualTable).Where("owner_user_id = ? AND id = ? AND revision = ?", ownerID, manualID, revision).Updates(fields)
	return result.RowsAffected == 1, result.Error
}

func UpdateManualFields(tx *gorm.DB, manualID int64, fields map[string]interface{}) error {
	return tx.Table(ManualTable).Where("id = ?", manualID).Updates(fields).Error
}

func UpdateManualItemPositions(tx *gorm.DB, manualID int64, positions map[int64]int) (bool, error) {
	if len(positions) == 0 {
		return true, nil
	}
	ids := make([]int64, 0, len(positions))
	for id := range positions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	query := "UPDATE " + ManualItemTable + " SET position = CASE id"
	arguments := make([]interface{}, 0, len(ids)*2+2)
	for _, id := range ids {
		query += " WHEN ? THEN ?"
		arguments = append(arguments, id, positions[id])
	}
	query += " ELSE position END WHERE manual_id = ? AND deleted_at IS NULL AND id IN ?"
	arguments = append(arguments, manualID, ids)
	result := tx.Exec(query, arguments...)
	return result.RowsAffected == int64(len(ids)), result.Error
}

func DeleteManualItem(tx *gorm.DB, manualID, itemID int64, deletedAt time.Time) error {
	return tx.Table(ManualItemTable).Where("manual_id = ? AND id = ? AND deleted_at IS NULL", manualID, itemID).Updates(map[string]interface{}{"deleted_at": deletedAt}).Error
}

func CompactManualItemPositions(tx *gorm.DB, manualID int64, removedPosition int) error {
	return tx.Table(ManualItemTable).Where("manual_id = ? AND deleted_at IS NULL AND position > ?", manualID, removedPosition).Update("position", gorm.Expr("position - 1")).Error
}

func prefixedColumns(alias string, columns []string) []string {
	result := make([]string, 0, len(columns))
	for _, column := range columns {
		result = append(result, alias+"."+column)
	}
	return result
}
