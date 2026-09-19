package fileshare

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

type Application struct {
	ownerLocks [64]sync.Mutex
	uploads    struct {
		sync.Mutex
		byUser        map[int64]int
		global        int
		reservedBytes uint64
	}
	attempts struct {
		sync.Mutex
		entries map[string]attemptEntry
	}
	bcryptGate chan struct{}
	cookieKey  []byte
	now        func() time.Time
	diskFree   func(string) (uint64, error)
}

type attemptEntry struct {
	Count     int
	ExpiresAt time.Time
}

var Default = New()

func New() *Application {
	application := &Application{bcryptGate: make(chan struct{}, 4), now: time.Now, diskFree: service.DiskFreeBytes}
	application.uploads.byUser = make(map[int64]int)
	application.attempts.entries = make(map[string]attemptEntry)
	application.cookieKey = make([]byte, 32)
	if _, err := rand.Read(application.cookieKey); err != nil {
		panic("cannot initialize file share cookie signer")
	}
	return application
}

func database(ctx context.Context) (*gorm.DB, error) {
	if db.MasterDB() == nil {
		return nil, service.ErrDependency
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
		return service.ErrConflict
	}
	return service.ErrDependency
}

func (application *Application) AcquireUpload(conf config.FileSharingConfig, ownerID int64) (func(), error) {
	application.uploads.Lock()
	defer application.uploads.Unlock()
	if application.uploads.byUser[ownerID] >= conf.MaxConcurrentUploadsPerUser || application.uploads.global >= conf.MaxConcurrentUploads {
		return nil, service.ErrRateLimited
	}
	free, err := application.diskFree(conf.StorageRoot)
	reserve := uint64(conf.MaxFileBytes)
	minimum := uint64(conf.MinFreeDiskBytes)
	if err != nil || free < minimum || free-minimum < application.uploads.reservedBytes || free-minimum-application.uploads.reservedBytes < reserve {
		return nil, service.ErrRateLimited
	}
	application.uploads.byUser[ownerID]++
	application.uploads.global++
	application.uploads.reservedBytes += reserve
	return func() {
		application.uploads.Lock()
		defer application.uploads.Unlock()
		application.uploads.byUser[ownerID]--
		if application.uploads.byUser[ownerID] == 0 {
			delete(application.uploads.byUser, ownerID)
		}
		application.uploads.global--
		application.uploads.reservedBytes -= reserve
	}, nil
}
