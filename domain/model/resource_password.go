package model

import "time"

// ResourcePassword stores only a bcrypt verifier. A retained empty row records
// the monotonic version after password protection has been cleared.
type ResourcePassword struct {
	ResourceType string    `gorm:"column:resource_type;primaryKey" json:"-"`
	ResourceID   int64     `gorm:"column:resource_id;primaryKey" json:"-"`
	PasswordHash string    `gorm:"column:password_hash" json:"-"`
	Version      int64     `gorm:"column:version" json:"version"`
	UpdateTime   time.Time `gorm:"column:update_time" json:"-"`
}

func (ResourcePassword) TableName() string { return "resource_passwords" }
