package db

import (
	"time"

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

	sqlDB, err := db.DB()
	// SetMaxIdleConns 设置空闲连接池中连接的最大数量
	sqlDB.SetMaxIdleConns(10)
	// SetMaxOpenConns 设置打开数据库连接的最大数量。
	sqlDB.SetMaxOpenConns(100)
	// SetConnMaxLifetime 设置了连接可复用的最大时间。
	sqlDB.SetConnMaxLifetime(time.Hour)
	return nil
}

func MasterDB() *gorm.DB {
	return masterDB
}
