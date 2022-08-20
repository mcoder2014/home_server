package model

type UserIdentity struct {
	// 主键
	ID int64 `json:"id" gorm:"column:id"`
	// 用户名
	UserName string `json:"user_name" gorm:"column:user_name"`
	// 密码
	BcryptPassword string `json:"password" gorm:"column:password"`
	// 邮箱
	Email string `json:"email" gorm:"column:email"`
	// 手机
	Mobile string `json:"mobile" gorm:"column:mobile"`

	DalModel
}
