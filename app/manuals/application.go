package manuals

import (
	"context"
	"errors"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/db"
	manualservice "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

type Application struct {
	ownerLocks [64]sync.Mutex
	uploads    uploadReservations
	diskFree   func(string) (uint64, error)
}

type uploadReservations struct {
	sync.Mutex
	reservedBytes uint64
}

var Default = New()

func New() *Application {
	return &Application{diskFree: manualservice.DiskFreeBytes}
}

func database(ctx context.Context) (*gorm.DB, error) {
	if db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	return db.MasterDB().WithContext(ctx), nil
}

func (application *Application) lockOwner(ownerID int64) func() {
	index := uint64(ownerID) % uint64(len(application.ownerLocks))
	application.ownerLocks[index].Lock()
	return application.ownerLocks[index].Unlock
}

func persistenceError(err error) error {
	if err == nil {
		return nil
	}
	var apiError *apperrors.APIError
	if errors.As(err, &apiError) {
		return err
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		return apperrors.ErrConflict
	}
	return apperrors.ErrDependency
}
