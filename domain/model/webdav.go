package model

type WebDAVLogEntity struct {
	ID       int64  `gorm:"column:id"`
	Method   string `gorm:"column:method"`
	FilePath string `gorm:"column:filepath"`
	Hash     string `gorm:"column:hash"`
	UserID   int64  `gorm:"column:user_id"`
	Agent    string `gorm:"column:agent"`
	LogID    string `gorm:"-"`
	DalModel
}
