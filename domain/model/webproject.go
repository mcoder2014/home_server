package model

import "time"

type WebProject struct {
	ID               int64      `json:"id,string" gorm:"column:id"`
	OwnerUserID      int64      `json:"-" gorm:"column:owner_user_id"`
	Name             string     `json:"name" gorm:"column:name"`
	Description      string     `json:"description" gorm:"column:description"`
	Slug             string     `json:"slug" gorm:"column:slug"`
	AccessMode       string     `json:"access_mode" gorm:"column:access_mode"`
	Status           string     `json:"status" gorm:"column:status"`
	CurrentReleaseID *int64     `json:"-" gorm:"column:current_release_id"`
	Revision         int64      `json:"revision" gorm:"column:revision"`
	ClientRequestID  *string    `json:"-" gorm:"column:client_request_id"`
	DeletedAt        *time.Time `json:"-" gorm:"column:deleted_at"`
	CreateTime       time.Time  `json:"create_time" gorm:"column:create_time"`
	UpdateTime       time.Time  `json:"update_time" gorm:"column:update_time"`
}

type WebProjectMember struct {
	ProjectID int64     `gorm:"column:project_id"`
	UserID    int64     `gorm:"column:user_id"`
	CreatedBy int64     `gorm:"column:created_by"`
	CreateTime time.Time `gorm:"column:create_time"`
}

type WebProjectRelease struct {
	ID             int64     `json:"id,string" gorm:"column:id"`
	ProjectID      int64     `json:"project_id,string" gorm:"column:project_id"`
	UploadedBy     int64     `json:"-" gorm:"column:uploaded_by"`
	StorageKey     string    `json:"-" gorm:"column:storage_key"`
	Status         string    `json:"status" gorm:"column:status"`
	EntryFile      string    `json:"entry_file" gorm:"column:entry_file"`
	SHA256         string    `json:"sha256" gorm:"column:sha256"`
	FileCount      int       `json:"file_count" gorm:"column:file_count"`
	TotalBytes     int64     `json:"total_bytes" gorm:"column:total_bytes"`
	IdempotencyKey *string   `json:"-" gorm:"column:idempotency_key"`
	ErrorMessage   string    `json:"error_message,omitempty" gorm:"column:error_message"`
	CreateTime     time.Time `json:"create_time" gorm:"column:create_time"`
	UpdateTime     time.Time `json:"-" gorm:"column:update_time"`
}
