package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

const (
	BookInfoTable = "bookinfo"
)

func InsertBookInfo(info *model.BookInfo) error {
	info.CreateTime = time.Now()
	info.UpdateTime = time.Now()
	return db.MasterDB().Table(BookInfoTable).
		Create(info).
		Debug().Error
}

func QueryBookInfoById(id int64) (*model.BookInfo, error) {
	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).
		Where("id=?", id).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func QueryBookInfoByIsbn(isbn string) (*model.BookInfo, error) {
	if len(isbn) > 13 || len(isbn) < 10 {
		return nil, nil
	}

	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).
		Where("isbn13=? or isbn10=?", isbn, isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func QueryBookInfoByIsbn10(isbn string) (*model.BookInfo, error) {
	if len(isbn) < 10 {
		return nil, nil
	}
	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).
		Where("isbn10=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func DeleteBookInfoById(id int64) error {
	return db.MasterDB().Table(BookInfoTable).Where("id=?", id).Delete(&model.BookInfo{}).Error
}

func BatchQueryBookInfoByIsbn(isbnList []string) ([]*model.BookInfo, error) {
	if len(isbnList) == 0 {
		return nil, nil
	}
	var result []*model.BookInfo
	err := db.MasterDB().Table(BookInfoTable).Where("isbn13 in (?) or isbn10 in (?)", isbnList, isbnList).Find(&result).Error
	return result, err
}
func BatchQueryBookInfoByID(ids []int64) ([]*model.BookInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []*model.BookInfo
	err := db.MasterDB().Table(BookInfoTable).Where("id in (?)", ids).Find(&result).Error
	return result, err
}
