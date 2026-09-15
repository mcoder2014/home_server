package dal

import (
	"context"
	"errors"

	myErrors "github.com/mcoder2014/home_server/errors"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

const (
	BookAddressTable = "book_address"
)

func InsertBookAddress(m *model.BookAddress, transactions ...*gorm.DB) (int64, error) {
	database := db.MasterDB()
	if len(transactions) > 0 {
		database = transactions[0]
	}
	e := database.Table(BookAddressTable).Create(m).Error
	if e != nil {
		return 0, myErrors.Wrap(e, myErrors.ErrorCodeDbError)
	}
	// Transaction callers publish only after commit. With negative caching off,
	// their new auto-increment IDs cannot expose an uncommitted row via this cache.
	if len(transactions) == 0 {
		InvalidateBookAddressCache(context.Background(), m.Id)
	}
	return m.Id, nil
}

func QueryBookAddressById(id int64) (*model.BookAddress, error) {
	if ReadCache("book_address") != nil {
		addresses, err := queryCachedBookAddresses([]int64{id})
		if err == nil && len(addresses) > 0 {
			return addresses[0], nil
		}
		if err == nil {
			return nil, nil
		}
	}
	var info model.BookAddress
	e := db.MasterDB().Table(BookAddressTable).Select(bookAddressColumns).
		Where("id=?", id).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func BatchQueryBookAddress(ids []int64) ([]*model.BookAddress, error) {
	if ReadCache("book_address") != nil {
		result, err := queryCachedBookAddresses(ids)
		if err == nil {
			return result, nil
		}
	}
	var res []*model.BookAddress
	e := db.MasterDB().Table(BookAddressTable).Select(bookAddressColumns).Where("id in (?)", ids).Find(&res).Error
	return res, e
}

func DeleteBookAddress(id int64) error {
	err := db.MasterDB().Table(BookAddressTable).Where("id=?", id).Delete(&model.BookAddress{}).Error
	if err == nil {
		InvalidateBookAddressCache(context.Background(), id)
	}
	return err
}
