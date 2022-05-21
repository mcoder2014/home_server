package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/database"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

const BookInfoTable = "bookinfo"

func InsertBookInfo(info *model.BookInfo) error {
	info.CreateTime = time.Now()
	info.UpdateTime = time.Now()
	return database.MasterDB().Table(BookInfoTable).
		Create(info).
		Debug().Error
}

func QueryBookInfoByIsbn(isbn string) (*model.BookInfo, error) {
	var info model.BookInfo
	e := database.MasterDB().Table(BookInfoTable).
		Where("isbn13=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func QueryBookInfoByIsbn10(isbn string) (*model.BookInfo, error) {
	var info model.BookInfo
	e := database.MasterDB().Table(BookInfoTable).
		Where("isbn10=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func DeleteById(id int64) error {
	return database.MasterDB().Table(BookInfoTable).Where("id=?", id).Delete(&model.BookInfo{}).Error
}
