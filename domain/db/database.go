package db

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var masterDB *gorm.DB

func InitDatabase(dsn string) error {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	masterDB = db
	return nil
}

func MasterDB() *gorm.DB {
	return masterDB
}
