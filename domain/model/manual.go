package model

import "time"

type ManualAccess uint8

const (
	ManualAccessOwner         ManualAccess = 1
	ManualAccessAuthenticated ManualAccess = 2
	ManualAccessPublic        ManualAccess = 3
)

func (value ManualAccess) String() string {
	switch value {
	case ManualAccessOwner:
		return "owner"
	case ManualAccessAuthenticated:
		return "authenticated"
	case ManualAccessPublic:
		return "public"
	default:
		return ""
	}
}

func ParseManualAccess(value string) (ManualAccess, bool) {
	switch value {
	case "owner":
		return ManualAccessOwner, true
	case "authenticated":
		return ManualAccessAuthenticated, true
	case "public":
		return ManualAccessPublic, true
	default:
		return 0, false
	}
}

type ManualStatus uint8

const (
	ManualStatusDraft   ManualStatus = 1
	ManualStatusActive  ManualStatus = 2
	ManualStatusDeleted ManualStatus = 3
)

func (value ManualStatus) String() string {
	switch value {
	case ManualStatusDraft:
		return "draft"
	case ManualStatusActive:
		return "active"
	case ManualStatusDeleted:
		return "deleted"
	default:
		return ""
	}
}

func ParseManualStatus(value string) (ManualStatus, bool) {
	switch value {
	case "draft":
		return ManualStatusDraft, true
	case "active":
		return ManualStatusActive, true
	case "deleted":
		return ManualStatusDeleted, true
	default:
		return 0, false
	}
}

type ManualItemKind uint8

const (
	ManualItemImage ManualItemKind = 1
	ManualItemPDF   ManualItemKind = 2
	ManualItemText  ManualItemKind = 3
	ManualItemURL   ManualItemKind = 4
)

func (value ManualItemKind) String() string {
	switch value {
	case ManualItemImage:
		return "image"
	case ManualItemPDF:
		return "pdf"
	case ManualItemText:
		return "text"
	case ManualItemURL:
		return "url"
	default:
		return ""
	}
}

func ParseManualItemKind(value string) (ManualItemKind, bool) {
	switch value {
	case "image":
		return ManualItemImage, true
	case "pdf":
		return ManualItemPDF, true
	case "text":
		return ManualItemText, true
	case "url":
		return ManualItemURL, true
	default:
		return 0, false
	}
}

type ManualPreviewStatus uint8

const (
	ManualPreviewNone        ManualPreviewStatus = 1
	ManualPreviewReady       ManualPreviewStatus = 2
	ManualPreviewUnavailable ManualPreviewStatus = 3
)

func (value ManualPreviewStatus) String() string {
	switch value {
	case ManualPreviewNone:
		return "none"
	case ManualPreviewReady:
		return "ready"
	case ManualPreviewUnavailable:
		return "unavailable"
	default:
		return ""
	}
}

type Manual struct {
	ID              int64        `gorm:"column:id"`
	OwnerUserID     int64        `gorm:"column:owner_user_id"`
	Name            string       `gorm:"column:name"`
	Description     string       `gorm:"column:description"`
	AccessMode      ManualAccess `gorm:"column:access_mode"`
	Status          ManualStatus `gorm:"column:status"`
	CoverItemID     *int64       `gorm:"column:cover_item_id"`
	Revision        int64        `gorm:"column:revision"`
	ClientRequestID string       `gorm:"column:client_request_id"`
	RequestHash     string       `gorm:"column:request_hash"`
	CreateTime      time.Time    `gorm:"column:create_time"`
	UpdateTime      time.Time    `gorm:"column:update_time"`
}

type ManualCategory struct {
	ManualID   int64     `gorm:"column:manual_id"`
	Category   string    `gorm:"column:category"`
	CreateTime time.Time `gorm:"column:create_time"`
}

type ManualItem struct {
	ID              int64               `gorm:"column:id"`
	ManualID        int64               `gorm:"column:manual_id"`
	Kind            ManualItemKind      `gorm:"column:kind"`
	Title           string              `gorm:"column:title"`
	Text            string              `gorm:"column:text"`
	URL             string              `gorm:"column:url"`
	StorageKey      *string             `gorm:"column:storage_key"`
	ThumbnailKey    *string             `gorm:"column:thumbnail_key"`
	ContentType     string              `gorm:"column:content_type"`
	OriginalName    string              `gorm:"column:original_name"`
	SizeBytes       int64               `gorm:"column:size_bytes"`
	SHA256          string              `gorm:"column:sha256"`
	Position        int                 `gorm:"column:position"`
	PreviewStatus   ManualPreviewStatus `gorm:"column:preview_status"`
	ClientRequestID string              `gorm:"column:client_request_id"`
	RequestHash     string              `gorm:"column:request_hash"`
	DeletedAt       *time.Time          `gorm:"column:deleted_at"`
	CreateTime      time.Time           `gorm:"column:create_time"`
}
