package model

import "time"

type SiteConfigCurrent struct {
	Namespace     string    `gorm:"column:namespace;primaryKey"`
	Revision      int64     `gorm:"column:revision"`
	SchemaVersion int       `gorm:"column:schema_version"`
	ValuesJSON    string    `gorm:"column:values_json"`
	ValuesSHA256  []byte    `gorm:"column:values_sha256"`
	UpdatedBy     int64     `gorm:"column:updated_by"`
	UpdateTime    time.Time `gorm:"column:update_time"`
}

type SiteConfigHistory struct {
	Namespace            string    `gorm:"column:namespace;primaryKey"`
	Revision             int64     `gorm:"column:revision;primaryKey"`
	SchemaVersion        int       `gorm:"column:schema_version"`
	ValuesJSON           string    `gorm:"column:values_json"`
	ValuesSHA256         []byte    `gorm:"column:values_sha256"`
	RequestID            string    `gorm:"column:request_id"`
	RequestHash          []byte    `gorm:"column:request_hash"`
	ActorUserID          int64     `gorm:"column:actor_user_id"`
	Reason               string    `gorm:"column:reason"`
	RollbackFromRevision *int64    `gorm:"column:rollback_from_revision"`
	CreateTime           time.Time `gorm:"column:create_time"`
}

type SiteRuntimeState struct {
	ID                int       `gorm:"column:id;primaryKey"`
	ConfigGeneration  int64     `gorm:"column:config_generation"`
	RegistrationEpoch int64     `gorm:"column:registration_epoch"`
	Revision          int64     `gorm:"column:revision"`
	UpdateTime        time.Time `gorm:"column:update_time"`
}
