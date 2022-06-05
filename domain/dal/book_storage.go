package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
)

const (
	BookStorageTable = "book_storage"
)

func InsertBookStorage(info *model.DBBookStorage) error {
	info.CreateTime = time.Now()
	info.UpdateTime = time.Now()
	return db.MasterDB().Table(BookStorageTable).Create(info).Debug().Error
}

func UpdateBookStorage(dto *model.UpdateBookStorageDto) error {
	fields := dto.ToFields()
	if len(fields) == 0 {
		return nil
	}
	return db.MasterDB().Table(BookStorageTable).Where("id=?", dto.Id).Updates(fields).Error
}

func QueryBookStorageByIsbn(isbn string) (*model.DBBookStorage, error) {
	if len(isbn) <= 10 {
		return QueryBookStorageByIsbn10(isbn)
	} else if len(isbn) > 13 {
		return nil, nil
	}

	var info model.DBBookStorage
	e := db.MasterDB().Table(BookStorageTable).
		Where("isbn13=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func QueryBookStorageByIsbn10(isbn string) (*model.DBBookStorage, error) {
	if len(isbn) < 10 {
		return nil, nil
	}
	var info model.DBBookStorage
	e := db.MasterDB().Table(BookStorageTable).
		Where("isbn10=?", isbn).Take(&info).Debug().Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &info, e
}

func DeleteBookStorageById(id int64) error {
	return db.MasterDB().Table(BookStorageTable).Where("id=?", id).Delete(&model.DBBookStorage{}).Error
}

func GetAllBookStorageOrderByUpdateTime(offset int, limit int) ([]*model.DBBookStorage, error) {
	var result []*model.DBBookStorage
	err := db.MasterDB().Table(BookStorageTable).Offset(offset).Limit(limit).Find(&result).Order("update_time desc").Error
	return result, err
}

// GetBookStorageCount 查询总藏书数
func GetBookStorageCount() (int, error) {
	var count int
	err := db.MasterDB().Table(BookStorageTable).Select("count(1)").Find(&count).Error
	return count, err
}
