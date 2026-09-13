package accounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrCredentials = apperrors.WithMessage(apperrors.ErrUnauthorized, "账号或密码错误")

func DatabaseMode() bool {
	return config.Global().IdentitySource == "database"
}

func database(ctx context.Context) (*gorm.DB, error) {
	if !DatabaseMode() || db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	return db.MasterDB().WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}), nil
}

func normalizeError(err error) error {
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

func GetByID(ctx context.Context, id int64) (*model.UserAccount, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	user, err := dal.QueryAccount(database, id, false)
	return user, normalizeError(err)
}

func GetByLogin(ctx context.Context, key string) (*model.UserAccount, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	user, err := dal.QueryAccountByLogin(database, strings.TrimSpace(key))
	if err == nil && user == nil {
		user, err = dal.QueryAccountByLogin(database, strings.ToLower(strings.TrimSpace(key)))
	}
	return user, normalizeError(err)
}

func RequireUserTx(tx *gorm.DB, id, version int64, admin bool) (*model.UserAccount, error) {
	user, err := dal.QueryAccount(tx, id, true)
	if err != nil {
		return nil, normalizeError(err)
	}
	if user == nil || user.Status != model.AccountActive || user.MustChangePassword || user.AuthVersion != version {
		return nil, apperrors.ErrUnauthorized
	}
	if admin && user.Role != model.RoleAdmin {
		return nil, apperrors.ErrForbidden
	}
	return user, nil
}

func Authenticate(ctx context.Context, key, password string) (*model.UserAccount, error) {
	user, err := GetByLogin(ctx, key)
	if err != nil {
		return nil, err
	}
	var id int64
	hash := ""
	if user != nil {
		id, hash = user.ID, user.PasswordHash
	}
	if err = VerifyPassword(ctx, id, key, hash, password); err != nil {
		return nil, err
	}
	if user.Status != model.AccountActive {
		return nil, apperrors.WithMessage(apperrors.ErrForbidden, "账号不可用，请联系管理员")
	}
	if user.MustChangePassword && (user.PasswordExpiresAt == nil || !user.PasswordExpiresAt.After(time.Now())) {
		return nil, apperrors.WithMessage(apperrors.ErrForbidden, "初始密码已过期，请联系管理员")
	}
	return user, nil
}

// Login validates the password before a short row-locked transaction. The lock
// rechecks the exact password and auth version so a reset/ban cannot race token
// issuance. A temporary password only receives a password-change session.
func Login(ctx context.Context, key, password string) (*model.UserAccount, string, *model.AccountSession, error) {
	verified, err := Authenticate(ctx, key, password)
	if err != nil {
		return nil, "", nil, err
	}
	policy := config.Runtime().AccountPolicy
	var upgraded string
	if cost, e := bcrypt.Cost([]byte(verified.PasswordHash)); e == nil && cost < policy.BcryptCost {
		hash, e := bcrypt.GenerateFromPassword([]byte(password), policy.BcryptCost)
		if e != nil {
			return nil, "", nil, apperrors.ErrDependency
		}
		upgraded = string(hash)
	}
	database, err := database(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	var user *model.UserAccount
	var token string
	var session *model.AccountSession
	err = database.Transaction(func(tx *gorm.DB) error {
		var e error
		user, e = dal.QueryAccount(tx, verified.ID, true)
		if e != nil {
			return e
		}
		if user == nil || user.Status != model.AccountActive || user.AuthVersion != verified.AuthVersion || user.PasswordHash != verified.PasswordHash {
			return ErrCredentials
		}
		if user.MustChangePassword && (user.PasswordExpiresAt == nil || !user.PasswordExpiresAt.After(time.Now())) {
			return ErrCredentials
		}
		if upgraded != "" {
			if e = tx.Table(dal.AccountTable).Where("id = ?", user.ID).Updates(map[string]interface{}{"password_hash": upgraded, "revision": gorm.Expr("revision + 1")}).Error; e != nil {
				return e
			}
			user.PasswordHash = upgraded
			user.Revision++
		}
		token, session, e = issueSessionTx(tx, user)
		return e
	})
	return user, token, session, normalizeError(err)
}

func issueSessionTx(tx *gorm.DB, user *model.UserAccount) (string, *model.AccountSession, error) {
	token, err := randomSecret("us_cq_", 32)
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	ttl := time.Duration(config.Runtime().AccountPolicy.SessionTTLSeconds) * time.Second
	purpose := model.SessionUser
	if user.MustChangePassword {
		purpose = model.SessionPasswordChange
		ttl = 10 * time.Minute
	}
	if ttl <= 0 {
		return "", nil, apperrors.ErrDependency
	}
	digest := sha256.Sum256([]byte(token))
	session := &model.AccountSession{ID: utils.GenInt64ID(), UserID: user.ID, TokenDigest: digest[:], AuthVersion: user.AuthVersion, Purpose: purpose, AuthenticatedAt: now, ExpireTime: now.Add(ttl), CreateTime: now, UpdateTime: now}
	if err = dal.InsertAccountSession(tx, session); err != nil {
		return "", nil, err
	}
	if err = tx.Table(dal.AccountTable).Where("id = ?", user.ID).Updates(map[string]interface{}{"last_login_at": now}).Error; err != nil {
		return "", nil, err
	}
	user.LastLoginAt = &now
	return token, session, nil
}

func IssueVerifiedSession(ctx context.Context, id, version int64) (string, error) {
	database, err := database(ctx)
	if err != nil {
		return "", err
	}
	var token string
	err = database.Transaction(func(tx *gorm.DB) error {
		user, e := dal.QueryAccount(tx, id, true)
		if e != nil {
			return e
		}
		if user == nil || user.Status != model.AccountActive || user.AuthVersion != version {
			return ErrCredentials
		}
		if user.MustChangePassword && (user.PasswordExpiresAt == nil || !user.PasswordExpiresAt.After(time.Now())) {
			return ErrCredentials
		}
		token, _, e = issueSessionTx(tx, user)
		return e
	})
	return token, normalizeError(err)
}

func CheckSession(ctx context.Context, token string, allowPasswordChange bool) (*model.UserAccount, *model.AccountSession, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(token) < 16 || len(token) > 256 {
		return nil, nil, apperrors.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	session, err := dal.QueryAccountSession(database, digest[:])
	if err != nil {
		return nil, nil, normalizeError(err)
	}
	if session == nil || session.IsExpired != 0 || !session.ExpireTime.After(time.Now()) {
		return nil, nil, apperrors.ErrUnauthorized
	}
	user, err := dal.QueryAccount(database, session.UserID, false)
	if err != nil {
		return nil, nil, normalizeError(err)
	}
	if user == nil || user.Status != model.AccountActive || user.AuthVersion != session.AuthVersion {
		return nil, nil, apperrors.ErrUnauthorized
	}
	if user.MustChangePassword || session.Purpose == model.SessionPasswordChange {
		if !allowPasswordChange || session.Purpose != model.SessionPasswordChange || user.PasswordExpiresAt == nil || !user.PasswordExpiresAt.After(time.Now()) {
			return nil, nil, apperrors.ErrForbidden
		}
	} else if session.Purpose != model.SessionUser {
		return nil, nil, apperrors.ErrUnauthorized
	}
	return user, session, nil
}

func Logout(ctx context.Context, token string) error {
	database, err := database(ctx)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	return normalizeError(database.Table(dal.TableUserToken).Where("token_digest = ?", digest[:]).Update("is_expired", 1).Error)
}

func UserIdentity(user *model.UserAccount) *model.UserIdentity {
	if user == nil {
		return nil
	}
	return &model.UserIdentity{ID: user.ID, UserName: user.Username, BcryptPassword: user.PasswordHash, Email: user.ContactEmail, Mobile: user.ContactMobile, AuthVersion: user.AuthVersion, Role: user.Role, LibraryEnabled: user.LibraryEnabled, WebDAVPermission: user.WebDAVPermission, MustChangePassword: user.MustChangePassword, DalModel: model.DalModel{CreateTime: user.CreateTime, UpdateTime: user.UpdateTime}}
}

func EnabledTx(tx *gorm.DB, namespace, key string, lock bool) (bool, error) {
	conf := config.Global()
	if conf.ConfigSource != "database" {
		value, ok := config.DefaultRuntimeValues(conf)[namespace][key].(bool)
		if !ok {
			return false, apperrors.ErrDependency
		}
		return value, nil
	}
	rows, err := dal.ReadSiteConfigs(tx, []string{namespace}, lock)
	if err != nil || len(rows) != 1 {
		return false, apperrors.ErrDependency
	}
	digest := sha256.Sum256([]byte(rows[0].ValuesJSON))
	if rows[0].SchemaVersion != 1 || rows[0].Revision <= 0 || !bytes.Equal(rows[0].ValuesSHA256, digest[:]) {
		return false, apperrors.ErrDependency
	}
	var values map[string]interface{}
	if err = json.Unmarshal([]byte(rows[0].ValuesJSON), &values); err != nil {
		return false, apperrors.ErrDependency
	}
	validated, err := config.ValidateValues(conf, namespace, values)
	if err != nil {
		return false, apperrors.ErrDependency
	}
	enabled, ok := validated[key].(bool)
	if !ok {
		return false, apperrors.ErrDependency
	}
	return enabled, nil
}

func ModuleEnabled(ctx context.Context, namespace string) (bool, error) {
	key := "enabled"
	if namespace == "auth" {
		key = "applications_enabled"
	}
	if db.MasterDB() == nil && config.Global().ConfigSource == "database" {
		return false, apperrors.ErrDependency
	}
	return EnabledTx(db.MasterDB(), namespace, key, false)
}
