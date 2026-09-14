package dal

import (
	"errors"
	"strings"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	AccountTable    = "user_account"
	LoginAliasTable = "user_login_alias"
	InvitationTable = "user_invitation"
	AdminAuditTable = "admin_audit_log"
)

var AccountColumns = []string{"id", "username", "username_key", "display_name", "contact_email", "contact_mobile", "password_hash", "status", "role", "library_enabled", "webdav_permission", "auth_version", "revision", "must_change_password", "password_expires_at", "invite_eligible_at", "source", "invited_by_user_id", "created_by_user_id", "imported_at", "last_login_at", "deleted_at", "create_time", "update_time"}

var InvitationColumns = []string{"id", "inviter_user_id", "quota_month", "slot", "token_digest", "token_hint", "registration_epoch", "status", "expires_at", "used_by_user_id", "used_at", "revoked_at", "revoke_reason", "note", "request_id", "create_time"}
var AccountSessionColumns = []string{"id", "user_id", "token_digest", "auth_version", "purpose", "is_expired", "authenticated_at", "expire_time", "create_time", "update_time"}
var AdminAuditColumns = []string{"id", "actor_user_id", "action", "target_type", "target_id", "before_summary", "after_summary", "reason", "result", "request_id", "create_time"}

// Queries involving identity/session material suppress SQL logging, including
// failure traces. Callers normalize database errors before producing responses.
func QueryAccount(database *gorm.DB, id int64, lock bool) (*model.UserAccount, error) {
	query := database.Session(&gorm.Session{Logger: logger.Discard}).Table(AccountTable).Select(AccountColumns).Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var user model.UserAccount
	err := query.Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

func QueryAccountByLogin(database *gorm.DB, key string) (*model.UserAccount, error) {
	var alias model.LoginAlias
	err := database.Session(&gorm.Session{Logger: logger.Discard}).Table(LoginAliasTable).Select("login_key", "user_id", "kind").Where("login_key = ?", strings.TrimSpace(key)).Take(&alias).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return QueryAccount(database, alias.UserID, false)
}

func InsertAccount(tx *gorm.DB, account *model.UserAccount) error {
	quiet := tx.Session(&gorm.Session{Logger: logger.Discard})
	if err := quiet.Table(AccountTable).Create(account).Error; err != nil {
		return err
	}
	alias := model.LoginAlias{LoginKey: account.UsernameKey, UserID: account.ID, Kind: "username"}
	return quiet.Table(LoginAliasTable).Create(&alias).Error
}

func UpdateAccount(tx *gorm.DB, id, revision int64, fields map[string]interface{}) (bool, error) {
	result := tx.Session(&gorm.Session{Logger: logger.Discard}).Table(AccountTable).Where("id = ? AND revision = ?", id, revision).Updates(fields)
	return result.RowsAffected == 1, result.Error
}

func QueryAccountSession(database *gorm.DB, digest []byte) (*model.AccountSession, error) {
	var session model.AccountSession
	err := database.Session(&gorm.Session{Logger: logger.Discard}).Table(TableUserToken).Select(AccountSessionColumns).Where("token_digest = ?", digest).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

func InsertAccountSession(tx *gorm.DB, session *model.AccountSession) error {
	return tx.Session(&gorm.Session{Logger: logger.Discard}).Table(TableUserToken).Create(session).Error
}

type AccountFilter struct {
	Cursor                                int64
	Limit                                 int
	Query, Status, Role, WebDAVPermission string
	LibraryEnabled                        *bool
}

// ListAccounts 按 ID 倒序游标列出匹配用户名和权限筛选的账号，默认排除已删除账号。
func ListAccounts(database *gorm.DB, filter AccountFilter) ([]*model.UserAccount, error) {
	query := database.Session(&gorm.Session{Logger: logger.Discard}).Table(AccountTable).Select(AccountColumns)
	if filter.Cursor > 0 {
		query = query.Where("id < ?", filter.Cursor)
	}
	if filter.Query != "" {
		query = query.Where("username_key LIKE ?", strings.ToLower(strings.TrimSpace(filter.Query))+"%")
	}
	if filter.Status != "" && filter.Status != "all" {
		query = query.Where("status = ?", filter.Status)
	} else if filter.Status != "all" {
		query = query.Where("status <> ?", model.AccountDeleted)
	}
	if filter.Role != "" {
		query = query.Where("role = ?", filter.Role)
	}
	if filter.LibraryEnabled != nil {
		query = query.Where("library_enabled = ?", *filter.LibraryEnabled)
	}
	if filter.WebDAVPermission != "" {
		query = query.Where("webdav_permission = ?", filter.WebDAVPermission)
	}
	var accounts []*model.UserAccount
	err := query.Order("id DESC").Limit(filter.Limit).Find(&accounts).Error
	return accounts, err
}

func QueryInvitation(database *gorm.DB, digest []byte, id int64, lock bool) (*model.UserInvitation, error) {
	query := database.Session(&gorm.Session{Logger: logger.Discard}).Table(InvitationTable).Select(InvitationColumns)
	if digest != nil {
		query = query.Where("token_digest = ?", digest)
	} else {
		query = query.Where("id = ?", id)
	}
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var invitation model.UserInvitation
	err := query.Take(&invitation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &invitation, err
}
