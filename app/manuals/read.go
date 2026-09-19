package manuals

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

func (application *Application) List(ctx context.Context, viewerID int64, mine bool, cursor int64, limit int, queryText, category string, categorySet bool, principal *utils.Principal) (*service.ManualPage, error) {
	if limit == 0 {
		limit = 20
	}
	queryText, category = strings.TrimSpace(queryText), strings.TrimSpace(category)
	if limit < 1 || limit > 100 || cursor < 0 || (mine && viewerID <= 0) || !utf8.ValidString(queryText) || utf8.RuneCountInString(queryText) > service.MaxNameRunes || !utf8.ValidString(category) || utf8.RuneCountInString(category) > service.MaxCategoryRunes {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	filter := dal.ManualListFilter{ViewerUserID: viewerID, Mine: mine, Cursor: cursor, Limit: limit + 1, Query: queryText, Category: category, CategorySet: categorySet}
	if accounts.DatabaseMode() {
		filter.RequireActiveOwner = true
	} else {
		filter.ActiveOwnerIDs, err = activeOwnerIDs(ctx)
		if err != nil {
			return nil, err
		}
	}
	manualRows, err := dal.ListManuals(database, filter)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	page := &service.ManualPage{Items: []*service.ManualView{}}
	if len(manualRows) > limit {
		page.HasMore = true
		manualRows = manualRows[:limit]
	}
	counts, covers, categories, err := listDecorations(database, manualRows)
	if err != nil {
		return nil, err
	}
	for _, manual := range manualRows {
		page.Items = append(page.Items, manualView(manual, categories[manual.ID], counts[manual.ID], covers[manual.ID], nil, principal))
	}
	if page.HasMore {
		page.NextCursor = strconv.FormatInt(manualRows[len(manualRows)-1].ID, 10)
	}
	return page, nil
}

func (application *Application) Categories(ctx context.Context, viewerID int64, mine bool, cursor string, limit int) (*service.CategoryPage, error) {
	if limit == 0 {
		limit = 200
	}
	if mine && viewerID <= 0 {
		return nil, apperrors.ErrUnauthorized
	}
	if limit < 1 || limit > 1000 || !utf8.ValidString(cursor) || utf8.RuneCountInString(cursor) > service.MaxCategoryRunes || strings.TrimSpace(cursor) != cursor {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	filter := dal.ManualListFilter{ViewerUserID: viewerID, Mine: mine}
	if accounts.DatabaseMode() {
		filter.RequireActiveOwner = true
	} else if filter.ActiveOwnerIDs, err = activeOwnerIDs(ctx); err != nil {
		return nil, err
	}
	items, err := dal.ListManualCategoriesAfter(database, filter, cursor, limit+1)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if items == nil {
		items = []string{}
	}
	page := &service.CategoryPage{Items: []string{}}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
	}
	page.Items = items
	if page.HasMore {
		page.NextCursor = items[len(items)-1]
	}
	return page, nil
}

func (application *Application) Get(ctx context.Context, manualID, viewerID int64, principal *utils.Principal) (*service.ManualView, error) {
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
	items, err := dal.ListManualItems(database, manual.ID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	categoryRows, err := dal.ListManualCategoriesByManualIDs(database, []int64{manual.ID})
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	cover := chooseDetailCover(manual.CoverItemID, items)
	return manualView(manual, categoriesByManualID(categoryRows)[manual.ID], len(items), cover, items, principal), nil
}

func activeOwnerIDs(ctx context.Context) ([]int64, error) {
	users, err := passport.ListUsersWithError(ctx)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		if user != nil && user.ID > 0 {
			ids = append(ids, user.ID)
		}
	}
	if len(ids) == 0 {
		ids = append(ids, -1)
	}
	return ids, nil
}

func ownerActive(ctx context.Context, ownerID int64) (bool, error) {
	owner, err := passport.GetByID(ctx, ownerID)
	if err != nil {
		return false, apperrors.ErrDependency
	}
	return owner != nil, nil
}

func listDecorations(database *gorm.DB, manuals []*model.Manual) (map[int64]int, map[int64]*dal.ManualItemPreview, map[int64][]string, error) {
	manualIDs, explicitItemIDs, autoManualIDs := make([]int64, 0, len(manuals)), make([]int64, 0, len(manuals)), make([]int64, 0, len(manuals))
	for _, manual := range manuals {
		manualIDs = append(manualIDs, manual.ID)
		if manual.CoverItemID == nil {
			autoManualIDs = append(autoManualIDs, manual.ID)
		} else {
			explicitItemIDs = append(explicitItemIDs, *manual.CoverItemID)
		}
	}
	counts, err := dal.ListManualItemCounts(database, manualIDs)
	if err != nil {
		return nil, nil, nil, apperrors.ErrDependency
	}
	explicit, err := dal.ListManualItemPreviewsByIDs(database, explicitItemIDs)
	if err != nil {
		return nil, nil, nil, apperrors.ErrDependency
	}
	automatic, err := dal.ListAutomaticManualCovers(database, autoManualIDs)
	if err != nil {
		return nil, nil, nil, apperrors.ErrDependency
	}
	categoryRows, err := dal.ListManualCategoriesByManualIDs(database, manualIDs)
	if err != nil {
		return nil, nil, nil, apperrors.ErrDependency
	}
	countMap, coverMap := make(map[int64]int, len(counts)), make(map[int64]*dal.ManualItemPreview, len(manuals))
	for _, count := range counts {
		countMap[count.ManualID] = count.Count
	}
	for _, cover := range append(explicit, automatic...) {
		coverMap[cover.ManualID] = cover
	}
	return countMap, coverMap, categoriesByManualID(categoryRows), nil
}

func categoriesByManualID(rows []*model.ManualCategory) map[int64][]string {
	categories := make(map[int64][]string)
	for _, row := range rows {
		categories[row.ManualID] = append(categories[row.ManualID], row.Category)
	}
	return categories
}

func chooseDetailCover(explicitID *int64, items []*model.ManualItem) *dal.ManualItemPreview {
	if explicitID != nil {
		for _, item := range items {
			if item.ID == *explicitID {
				return previewFromItem(item)
			}
		}
	}
	for _, item := range items {
		if (item.Kind == model.ManualItemImage || item.Kind == model.ManualItemPDF) && item.PreviewStatus == model.ManualPreviewReady {
			return previewFromItem(item)
		}
	}
	if len(items) > 0 {
		return previewFromItem(items[0])
	}
	return nil
}

func previewFromItem(item *model.ManualItem) *dal.ManualItemPreview {
	return &dal.ManualItemPreview{ID: item.ID, ManualID: item.ManualID, Kind: item.Kind, Title: item.Title, TextExcerpt: excerpt(item.Text, 160), URL: item.URL, ThumbnailKey: item.ThumbnailKey, PreviewStatus: item.PreviewStatus, Position: item.Position}
}

func manualView(manual *model.Manual, categories []string, itemCount int, cover *dal.ManualItemPreview, items []*model.ManualItem, principal *utils.Principal) *service.ManualView {
	if categories == nil {
		categories = []string{}
	}
	view := &service.ManualView{ID: strconv.FormatInt(manual.ID, 10), Name: manual.Name, Description: manual.Description, Categories: categories, AccessMode: manual.AccessMode.String(), Status: manual.Status.String(), Revision: manual.Revision, ItemCount: itemCount, CanEdit: canEdit(manual.OwnerUserID, principal), CreateTime: manual.CreateTime, UpdateTime: manual.UpdateTime}
	if manual.CoverItemID != nil {
		value := strconv.FormatInt(*manual.CoverItemID, 10)
		view.CoverItemID = &value
	}
	if cover != nil {
		view.Cover = coverView(manual.ID, cover)
		view.CoverURL = view.Cover.ThumbnailURL
	}
	if items != nil {
		view.Items = make([]*service.ItemView, 0, len(items))
		for _, item := range items {
			view.Items = append(view.Items, itemView(item))
		}
	}
	return view
}

func coverView(manualID int64, item *dal.ManualItemPreview) *service.CoverView {
	view := &service.CoverView{ID: strconv.FormatInt(item.ID, 10), Kind: item.Kind.String(), Title: item.Title, TextExcerpt: excerpt(item.TextExcerpt, 160), PreviewStatus: item.PreviewStatus.String()}
	if item.ThumbnailKey != nil && item.PreviewStatus == model.ManualPreviewReady {
		view.ThumbnailURL = itemResourceURL(manualID, item.ID, "thumbnail")
	}
	if parsed, err := url.Parse(item.URL); err == nil {
		view.URLHost = parsed.Hostname()
	}
	return view
}

func itemView(item *model.ManualItem) *service.ItemView {
	view := &service.ItemView{ID: strconv.FormatInt(item.ID, 10), Kind: item.Kind.String(), Title: item.Title, Text: item.Text, URL: item.URL, OriginalName: item.OriginalName, ContentType: item.ContentType, SizeBytes: item.SizeBytes, Position: item.Position, PreviewStatus: item.PreviewStatus.String()}
	if item.StorageKey != nil {
		view.ContentURL = itemResourceURL(item.ManualID, item.ID, "content")
	}
	if item.ThumbnailKey != nil && item.PreviewStatus == model.ManualPreviewReady {
		view.ThumbnailURL = itemResourceURL(item.ManualID, item.ID, "thumbnail")
	}
	return view
}

func itemResourceURL(manualID, itemID int64, resource string) string {
	return "/api/manuals/" + strconv.FormatInt(manualID, 10) + "/items/" + strconv.FormatInt(itemID, 10) + "/" + resource
}

func canEdit(ownerID int64, principal *utils.Principal) bool {
	return principal != nil && principal.UserID == ownerID && (principal.Kind == "user" || principal.Allows("manuals:write"))
}

func excerpt(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
