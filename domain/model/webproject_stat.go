package model

import "time"

// WebProjectStatTotal is an absolute, sequenced snapshot. The HLL is needed to
// recover future unique-visitor counting and must never be returned by APIs.
type WebProjectStatTotal struct {
	ProjectID         int64     `gorm:"column:project_id;primaryKey"`
	PV                uint64    `gorm:"column:pv"`
	UV                uint64    `gorm:"column:uv"`
	UVHLL             []byte    `gorm:"column:uv_hll" json:"-"`
	LastSeq           uint64    `gorm:"column:last_seq"`
	TrackingStartedAt time.Time `gorm:"column:tracking_started_at"`
	PersistedAt       time.Time `gorm:"column:persisted_at"`
	Quality           string    `gorm:"column:quality"`
	QualityReason     string    `gorm:"column:quality_reason"`
	Timezone          string    `gorm:"column:timezone"`
	FormatVersion     uint16    `gorm:"column:format_version"`
}

type WebProjectStatDaily struct {
	ProjectID     int64     `gorm:"column:project_id;primaryKey"`
	StatDate      string    `gorm:"column:stat_date;primaryKey"`
	PV            uint64    `gorm:"column:pv"`
	UV            uint64    `gorm:"column:uv"`
	UVHLL         []byte    `gorm:"column:uv_hll" json:"-"`
	SnapshotSeq   uint64    `gorm:"column:snapshot_seq"`
	PersistedAt   time.Time `gorm:"column:persisted_at"`
	Quality       string    `gorm:"column:quality"`
	QualityReason string    `gorm:"column:quality_reason"`
}
