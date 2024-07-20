package db

import (
	"fmt"

	"gorm.io/gorm"
)

var ErrDBNotInit = fmt.Errorf("db is not init")

type BaseDAO struct {
	db        *gorm.DB
	tableName string
}

func NewDAO(db *gorm.DB, tableName string) (*BaseDAO, error) {
	if db == nil {
		return nil, ErrDBNotInit
	}
	return &BaseDAO{
		db:        db,
		tableName: tableName,
	}, nil
}

func (d *BaseDAO) Table(opts ...Option) *gorm.DB {
	var opt option
	for _, f := range opts {
		f(&opt)
	}
	if opt.dryRun {
		return newDryRunDB().Table(d.tableName)
	}
	if opt.useTX {
		return opt.tx.Table(d.tableName)
	}
	return d.db.Table(d.tableName)
}
