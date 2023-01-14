package model

import "time"

type UserTokenExpireStatus int

const (
	UserTokenNotExpired UserTokenExpireStatus = 0
	UserTokenExpired    UserTokenExpireStatus = 1
)

type UserToken struct {
	ID         int64                 `gorm:"column:id"`
	UserID     int64                 `gorm:"column:user_id"`
	Token      string                `gorm:"column:token"`
	IsExpired  UserTokenExpireStatus `gorm:"column:is_expired"`
	CreateTime time.Time             `gorm:"column:create_time;default:NULL"`
	UpdateTime time.Time             `gorm:"column:update_time;default:NULL"`
	ExpireTime time.Time             `gorm:"column:expire_time"`
}
