package model

type BookAddress struct {
	// 数据库主键
	Id int64 `json:"id" gorm:"column:id"`
	// 详细地址
	Address string `json:"address" gorm:"column:address"`
	// 简称
	ShortName string `json:"short_name" gorm:"column:short_name"`
	DalModel
}
