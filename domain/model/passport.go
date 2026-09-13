package model

import (
	"encoding/json"
	"time"
)

type UserIdentity struct {
	// 主键
	ID int64 `json:"id" gorm:"column:id"`
	// 用户名
	UserName string `json:"user_name" gorm:"column:user_name"`
	// 密码
	BcryptPassword string `json:"-" gorm:"column:password"`
	// 邮箱
	Email string `json:"email" gorm:"column:email"`
	// 手机
	Mobile             string    `json:"mobile" gorm:"column:mobile"`
	AuthVersion        int64     `json:"-" gorm:"-"`
	SessionExpiry      time.Time `json:"-" gorm:"-"`
	Role               string    `json:"role,omitempty" gorm:"-"`
	LibraryEnabled     bool      `json:"library_enabled" gorm:"-"`
	WebDAVPermission   string    `json:"webdav_permission,omitempty" gorm:"-"`
	MustChangePassword bool      `json:"must_change_password" gorm:"-"`

	DalModel
}

// UnmarshalJSON accepts the historical protected configuration representation.
// Marshaling the same identity never exposes its password hash.
func (u *UserIdentity) UnmarshalJSON(data []byte) error {
	type identity UserIdentity
	decoded := struct {
		*identity
		Password string `json:"password"`
	}{identity: (*identity)(u)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	u.BcryptPassword = decoded.Password
	return nil
}
