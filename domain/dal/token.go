package dal

import (
	"errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	myErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

const TableUserToken = "login_token"

func CreateToken(m *model.UserToken) (int64, error) {
	e := db.MasterDB().Table(TableUserToken).Create(m).Error
	if e != nil {
		return 0, myErrors.Wrap(e, myErrors.ErrorCodeDbError)
	}
	return m.ID, nil
}

func ExpireToken(id int64) error {
	return db.MasterDB().
		Table(TableUserToken).
		Where("id=?", id).
		Update("is_expired", model.UserTokenExpired).
		Error
}

func QueryByToken(token string) (*model.UserToken, error) {
	var res model.UserToken
	e := db.MasterDB().Table(TableUserToken).
		Where("token = ?", token).
		First(&res).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, myErrors.Wrap(e, myErrors.ErrorCodeDbError)
	}
	return &res, nil
}
