package applications

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	appErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

type Repository struct{}

// Create serializes the owner's active-slot range and assigns one of the
// bounded slots. The unique owner/slot key remains the final concurrency guard.
func (r *Repository) Create(ctx context.Context, application *model.Application, maxActive int) error {
	for attempt := 0; attempt < maxActive; attempt++ {
		err := db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := accounts.LockApplicationOwnerTx(tx, application.OwnerUserID, application.Scopes, true, application.ActorAuthVersion); err != nil {
				return err
			}
			slots, err := dal.LockApplicationSlots(tx, application.OwnerUserID, maxActive)
			if err != nil {
				return err
			}
			slot, err := allocateApplicationSlot(slots, maxActive)
			if err != nil {
				return err
			}
			application.ActiveSlot = &slot
			return dal.CreateApplication(tx, application)
		})
		if !isRetryableCreateError(err) {
			return err
		}
	}
	return appErrors.ErrConflict
}

func (r *Repository) ListOwned(ctx context.Context, ownerUserID, cursor int64, limit int) ([]*model.Application, error) {
	database := db.MasterDB().WithContext(ctx)
	applications, err := dal.ListOwnedApplications(database, ownerUserID, cursor, limit)
	return applications, err
}

func (r *Repository) GetOwned(ctx context.Context, ownerUserID, applicationID int64) (*model.Application, error) {
	database := db.MasterDB().WithContext(ctx)
	application, err := dal.QueryOwnedApplication(database, ownerUserID, applicationID)
	return application, err
}

func (r *Repository) GetByAccessKey(ctx context.Context, accessKey string) (*model.Application, error) {
	database := db.MasterDB().WithContext(ctx)
	application, err := dal.QueryApplicationByAccessKey(database, accessKey)
	return application, err
}

func (r *Repository) GetByID(ctx context.Context, applicationID int64) (*model.Application, error) {
	database := db.MasterDB().WithContext(ctx)
	application, err := dal.QueryApplicationByID(database, applicationID)
	return application, err
}

func (r *Repository) UpdateOwned(ctx context.Context, application *model.Application, expectedRevision int64) (bool, error) {
	database := db.MasterDB().WithContext(ctx)
	var updated bool
	err := database.Transaction(func(tx *gorm.DB) error {
		scopes := application.Scopes
		if application.Status == model.ApplicationStatusRevoked {
			scopes = nil
		}
		if err := accounts.LockApplicationOwnerTx(tx, application.OwnerUserID, scopes, application.Status == model.ApplicationStatusEnabled, application.ActorAuthVersion); err != nil {
			return err
		}
		var err error
		updated, err = dal.UpdateOwnedApplication(tx, application, expectedRevision)
		return err
	})
	return updated, err
}

// StoreIssuedToken rechecks the authorization snapshot while holding the
// application row lock, so a concurrent rotate or disable cannot issue a token
// against stale credentials.
func (r *Repository) StoreIssuedToken(ctx context.Context, application *model.Application, token *model.ApplicationAccessToken, now time.Time) error {
	return db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := accounts.LockApplicationOwnerTx(tx, application.OwnerUserID, application.Scopes, true); err != nil {
			return err
		}
		current, err := dal.LockApplicationByID(tx, application.ID)
		if err != nil {
			return err
		}
		if current == nil || current.Status != model.ApplicationStatusEnabled || current.Revision != application.Revision || current.SecretVersion != application.SecretVersion || !current.ExpiresAt.After(now) {
			return appErrors.ErrUnauthorized
		}
		if err := dal.CreateApplicationToken(tx, token); err != nil {
			return err
		}
		return dal.UpdateLastIssuedAt(tx, application.ID, now)
	})
}

func (r *Repository) GetTokenByDigest(ctx context.Context, digest []byte) (*model.ApplicationAccessToken, error) {
	database := db.MasterDB().WithContext(ctx)
	token, err := dal.QueryApplicationTokenByDigest(database, digest)
	return token, err
}

func (r *Repository) CleanupExpiredTokens(ctx context.Context, before time.Time, limit int) error {
	return db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := dal.ListExpiredApplicationTokenIDs(tx, before, limit)
		if err != nil || len(ids) == 0 {
			return err
		}
		return dal.DeleteApplicationTokens(tx, ids)
	})
}

func allocateApplicationSlot(slots []int, maxActive int) (int, error) {
	if maxActive <= 0 || len(slots) >= maxActive {
		return 0, appErrors.ErrRateLimited
	}
	used := make([]bool, maxActive+1)
	for _, slot := range slots {
		if slot > 0 && slot <= maxActive {
			used[slot] = true
		}
	}
	for slot := 1; slot <= maxActive; slot++ {
		if !used[slot] {
			return slot, nil
		}
	}
	return 0, appErrors.ErrRateLimited
}

func isRetryableCreateError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == 1062 || mysqlErr.Number == 1205 || mysqlErr.Number == 1213)
}
