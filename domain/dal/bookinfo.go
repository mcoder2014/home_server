package dal

import (
	"context"
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	BookInfoTable = "bookinfo"
)

func InsertBookInfo(info *model.BookInfo) error {
	info.CreateTime = time.Now()
	info.UpdateTime = time.Now()
	err := db.MasterDB().Table(BookInfoTable).
		Create(info).
		Debug().Error
	if err == nil {
		InvalidateBookInfoCache(context.Background(), info)
	}
	return err
}

func QueryBookInfoById(id int64) (*model.BookInfo, error) {
	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).Select(bookInfoColumns).
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
	if ReadCache("book") != nil {
		books, err := queryCachedBookInfo([]string{isbn}, false)
		if err == nil && len(books) == 1 {
			return books[0], nil
		}
		if err == nil && len(books) == 0 {
			return nil, nil
		}
		// Duplicate ISBN rows keep the original Take selection, not cache ordering.
	}

	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).Select(bookInfoColumns).
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
	if ReadCache("book") != nil {
		books, err := queryCachedBookInfo([]string{isbn}, true)
		if err == nil && len(books) == 1 {
			return books[0], nil
		}
		if err == nil && len(books) == 0 {
			return nil, nil
		}
	}
	var info model.BookInfo
	e := db.MasterDB().Table(BookInfoTable).Select(bookInfoColumns).
		Where("isbn10=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func DeleteBookInfoById(id int64) error {
	var book *model.BookInfo
	if ReadCache("book") != nil {
		var err error
		book, err = QueryBookInfoById(id)
		if err != nil {
			logrus.WithField("namespace", "book").Warn("read cache invalidation metadata unavailable")
		}
	}
	err := db.MasterDB().Table(BookInfoTable).Where("id=?", id).Delete(&model.BookInfo{}).Error
	if err == nil {
		InvalidateBookInfoCache(context.Background(), book)
	}
	return err
}

func BatchQueryBookInfoByIsbn(isbnList []string) ([]*model.BookInfo, error) {
	if len(isbnList) == 0 {
		return nil, nil
	}
	if ReadCache("book") != nil {
		result, err := queryCachedBookInfo(isbnList, false)
		if err == nil {
			return result, nil
		}
	}
	var result []*model.BookInfo
	err := db.MasterDB().Table(BookInfoTable).Select(bookInfoColumns).Where("isbn13 in (?) or isbn10 in (?)", isbnList, isbnList).Find(&result).Error
	return result, err
}
