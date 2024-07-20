package db

import (
	"fmt"
	"log"
	"os"
	"time"

	driver "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const driverName = "mysql2"

type option struct {
	useTX  bool
	tx     *gorm.DB
	dryRun bool
}

type Option func(opt *option)

func WithTx(tx *gorm.DB) Option {
	return func(opt *option) {
		if opt == nil {
			return
		}
		opt.useTX = true
		opt.tx = tx
	}
}

func DryRun() Option {
	return func(opt *option) {
		if opt == nil {
			return
		}
		opt.dryRun = true
	}
}

func newDryRunDB() *gorm.DB {
	db, err := gorm.Open(driver.New(driver.Config{
		DriverName:                driverName,
		Conn:                      &gorm.PreparedStmtDB{},
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   "",
			SingularTable: true,
		},
		Logger: logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
			SlowThreshold: 100 * time.Millisecond,
			Colorful:      true,
			LogLevel:      logger.Info,
		}),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
		PrepareStmt: true,
		DryRun:      true,
		QueryFields: true,
	})
	if err != nil {
		panic(fmt.Sprintf("newDryRunDB error: %v", err))
	}
	return db
}
