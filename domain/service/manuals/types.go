package manuals

import (
	"bytes"
	"encoding/json"
	"time"
)

type OptionalID struct {
	Set   bool
	Value *string
}

func (value *OptionalID) UnmarshalJSON(data []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type CreateManualInput struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Categories      []string `json:"categories"`
	AccessMode      string   `json:"access_mode"`
	ClientRequestID string   `json:"client_request_id"`
}

type UpdateManualInput struct {
	Revision    int64      `json:"revision"`
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Categories  *[]string  `json:"categories"`
	AccessMode  *string    `json:"access_mode"`
	Status      *string    `json:"status"`
	CoverItemID OptionalID `json:"cover_item_id"`
	ItemIDs     *[]string  `json:"item_ids"`
}

type CoverView struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	ThumbnailURL  string `json:"thumbnail_url"`
	TextExcerpt   string `json:"text_excerpt"`
	URLHost       string `json:"url_host"`
	PreviewStatus string `json:"preview_status"`
}

type ItemView struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	Text          string `json:"text"`
	URL           string `json:"url"`
	OriginalName  string `json:"original_name"`
	ContentType   string `json:"content_type"`
	SizeBytes     int64  `json:"size_bytes"`
	Position      int    `json:"position"`
	ContentURL    string `json:"content_url"`
	ThumbnailURL  string `json:"thumbnail_url"`
	PreviewStatus string `json:"preview_status"`
}

type ManualView struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Description       string      `json:"description"`
	Categories        []string    `json:"categories"`
	AccessMode        string      `json:"access_mode"`
	Status            string      `json:"status"`
	Revision          int64       `json:"revision"`
	CoverItemID       *string     `json:"cover_item_id"`
	CoverURL          string      `json:"cover_url"`
	Cover             *CoverView  `json:"cover"`
	ItemCount         int         `json:"item_count"`
	CanEdit           bool        `json:"can_edit"`
	PasswordProtected bool        `json:"password_protected"`
	CreateTime        time.Time   `json:"create_time"`
	UpdateTime        time.Time   `json:"update_time"`
	Items             []*ItemView `json:"items,omitempty"`
}

type ManualPage struct {
	Items      []*ManualView `json:"items"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor"`
}

type CategoryPage struct {
	Items      []string `json:"items"`
	HasMore    bool     `json:"has_more"`
	NextCursor string   `json:"next_cursor"`
}

type ItemMutationResult struct {
	Item     *ItemView `json:"item"`
	Revision int64     `json:"revision"`
}

type DeleteItemResult struct {
	Revision    int64   `json:"revision"`
	CoverItemID *string `json:"cover_item_id"`
}
