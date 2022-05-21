package model

import "time"

// DalModel 数据库相关模型的基础信息
type DalModel struct {
	CreateTime time.Time `json:"create_time" gorm:"column:create_time"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time"`
}
