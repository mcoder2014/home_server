package dal

import (
	"errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	myErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const TableUserToken = "login_token"

func CreateToken(m *model.UserToken) (int64, error) {
	e := db.MasterDB().Session(&gorm.Session{Logger: logger.Discard}).Table(TableUserToken).Create(m).Error
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
	e := db.MasterDB().Session(&gorm.Session{Logger: logger.Discard}).Table(TableUserToken).
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

func QueryByTokenTx(tx *gorm.DB, token string, lock bool) (*model.UserToken, error) {
	query := tx.Session(&gorm.Session{Logger: logger.Discard}).Table(TableUserToken).
		Select("id, user_id, token, is_expired, create_time, update_time, expire_time").
		Where("token = ?", token)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var result model.UserToken
	err := query.Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, myErrors.Wrap(err, myErrors.ErrorCodeDbError)
	}
	return &result, nil
}
