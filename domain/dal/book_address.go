package dal

import (
	"errors"

	myErrors "github.com/mcoder2014/home_server/errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

const (
	BookAddressTable = "book_address"
)

func InsertBookAddress(m *model.BookAddress) (int64, error) {
	e := db.MasterDB().Table(BookAddressTable).Create(m).Error
	if e != nil {
		return 0, myErrors.Wrap(e, myErrors.ErrorCodeDbError)
	}
	return m.Id, nil
}

func QueryBookAddressById(id int64) (*model.BookAddress, error) {
	var info model.BookAddress
	e := db.MasterDB().Table(BookAddressTable).
		Where("id=?", id).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func DeleteBookAddress(id int64) error {
	return db.MasterDB().Table(BookAddressTable).Where("id=?", id).Delete(&model.BookAddress{}).Error
}
