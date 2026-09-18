package accounts

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProfileInput struct {
	DisplayName   *string `json:"display_name"`
	ContactEmail  *string `json:"contact_email"`
	ContactMobile *string `json:"contact_mobile"`
}

// UpdateProfile 在认证版本和资料修订号匹配时更新显式提供的联系资料，提交后读取最新账号。
func UpdateProfile(ctx context.Context, id, version, revision int64, input ProfileInput) (*model.UserAccount, error) {
	if input.DisplayName == nil && input.ContactEmail == nil && input.ContactMobile == nil {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	// 锁定账号并合并未修改的字段，校验最终资料后按原修订号提交更新。
	err = database.Transaction(func(tx *gorm.DB) error {
		user, e := RequireUserTx(tx, id, version, false)
		if e != nil {
			return e
		}
		if user.Revision != revision {
			return apperrors.ErrConflict
		}
		name, email, mobile := user.DisplayName, user.ContactEmail, user.ContactMobile
		fields := map[string]interface{}{"revision": revision + 1, "update_time": time.Now()}
		if input.DisplayName != nil {
			name = strings.TrimSpace(*input.DisplayName)
			fields["display_name"] = name
		}
		if input.ContactEmail != nil {
			email = strings.TrimSpace(*input.ContactEmail)
			fields["contact_email"] = email
		}
		if input.ContactMobile != nil {
			mobile = strings.TrimSpace(*input.ContactMobile)
			fields["contact_mobile"] = mobile
		}
		if e = validateProfile(name, email, mobile); e != nil {
			return e
		}
		_, e = dal.UpdateAccount(tx, id, revision, fields)
		return e
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return GetByID(ctx, id)
}

// ChangePassword 验证旧密码和新密码策略，在锁内更新密码与认证版本，并停用账号已有的应用凭据。
func ChangePassword(ctx context.Context, id, version int64, current, password, confirmation string) error {
	policy := config.Runtime().AccountPolicy
	if err := ValidatePassword(password, confirmation, policy.MinPasswordLength); err != nil {
		return err
	}
	verified, err := GetByID(ctx, id)
	if err != nil {
		return err
	}
	if verified == nil {
		return ErrCredentials
	}
	if err = VerifyPassword(ctx, id, "", verified.PasswordHash, current); err != nil {
		return err
	}
	if current == password {
		return apperrors.WithMessage(apperrors.ErrInvalid, "新密码不能与当前密码相同")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), policy.BcryptCost)
	if err != nil {
		return apperrors.ErrDependency
	}
	database, err := database(ctx)
	if err != nil {
		return err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		user, e := dal.QueryAccount(tx, id, true)
		if e != nil {
			return e
		}
		if user == nil || user.Status != model.AccountActive || user.AuthVersion != version || user.PasswordHash != verified.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if user.MustChangePassword && (user.PasswordExpiresAt == nil || !user.PasswordExpiresAt.After(time.Now())) {
			return apperrors.ErrUnauthorized
		}
		_, e = dal.UpdateAccount(tx, id, user.Revision, map[string]interface{}{"password_hash": string(hash), "must_change_password": false, "password_expires_at": nil, "auth_version": user.AuthVersion + 1, "revision": user.Revision + 1, "update_time": time.Now()})
		if e != nil {
			return e
		}
		return suspendApplicationsTx(tx, id, false)
	})
	return normalizeError(err)
}

func LogoutAll(ctx context.Context, id, version int64) error {
	database, err := database(ctx)
	if err != nil {
		return err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		user, e := RequireUserTx(tx, id, version, false)
		if e != nil {
			return e
		}
		_, e = dal.UpdateAccount(tx, id, user.Revision, map[string]interface{}{"auth_version": user.AuthVersion + 1, "revision": user.Revision + 1, "update_time": time.Now()})
		if e != nil {
			return e
		}
		return suspendApplicationsTx(tx, id, false)
	})
	return normalizeError(err)
}

type CreateInput struct {
	UserName         string `json:"user_name"`
	Password         string `json:"password"`
	GeneratePassword bool   `json:"generate_password"`
}

type CreatedAccount struct {
	User              *model.UserAccount `json:"user"`
	InitialPassword   string             `json:"initial_password"`
	PasswordExpiresAt *time.Time         `json:"password_expires_at"`
}

// AdminCreate 按当前密码策略创建待改密账号，事务内复核管理员并写审计，成功后一次返回初始密码。
func AdminCreate(ctx context.Context, actorID, version int64, input CreateInput) (*CreatedAccount, error) {
	username, err := NormalizeUsername(input.UserName)
	if err != nil {
		return nil, err
	}
	policy := config.Runtime().AccountPolicy
	password := input.Password
	if input.GeneratePassword || password == "" {
		password, err = randomPassword(policy.MinPasswordLength)
		if err != nil {
			return nil, err
		}
	}
	if err = ValidatePassword(password, password, policy.MinPasswordLength); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), policy.BcryptCost)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	now := time.Now()
	_, eligible := InvitationMonth(now)
	expiry := now.Add(time.Duration(policy.TemporaryPasswordTTLDays) * 24 * time.Hour)
	user := &model.UserAccount{Username: username, UsernameKey: username, DisplayName: username, PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleUser, WebDAVPermission: model.WebDAVNone, AuthVersion: 1, Revision: 1, MustChangePassword: true, PasswordExpiresAt: &expiry, InviteEligibleAt: eligible, Source: "admin", CreatedByUserID: &actorID, CreateTime: now, UpdateTime: now}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		if _, e := dal.ReadSiteRuntimeState(tx, true); e != nil {
			return e
		}
		if _, e := RequireUserTx(tx, actorID, version, true); e != nil {
			return e
		}
		if e := dal.InsertAccount(tx, user); e != nil {
			return e
		}
		return writeAudit(tx, actorID, "create", "user", user.ID, "", "创建账号", user)
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &CreatedAccount{User: user, InitialPassword: password, PasswordExpiresAt: &expiry}, nil
}

type AdminInput struct {
	ActingToken      string `json:"-"`
	Reason           string `json:"reason"`
	CurrentPassword  string `json:"current_password"`
	Password         string `json:"password"`
	GeneratePassword bool   `json:"generate_password"`
	Role             string `json:"role"`
	Enabled          bool   `json:"enabled"`
	Permission       string `json:"permission"`
	ResetDisplayName bool   `json:"reset_display_name"`
	ResetAvatar      bool   `json:"reset_avatar"`
}

func VerifyAdminPassword(ctx context.Context, id int64, password string) (*model.UserAccount, error) {
	user, err := GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Role != model.RoleAdmin || user.Status != model.AccountActive || user.MustChangePassword {
		return nil, apperrors.ErrForbidden
	}
	if err = VerifyPassword(ctx, id, "", user.PasswordHash, password); err != nil {
		return nil, err
	}
	return user, nil
}

// AdminChange serializes dangerous account transitions with runtime state, then
// locks actor/target in ID order. User state, session revocation, capability
// withdrawal and audit commit together. Restore never revives old credentials.
func AdminChange(ctx context.Context, actorID, version, targetID, revision int64, action string, input AdminInput) (*CreatedAccount, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len(input.Reason) > 512 || targetID <= 0 || revision <= 0 {
		return nil, apperrors.ErrInvalid
	}
	verified, err := VerifyAdminPassword(ctx, actorID, input.CurrentPassword)
	if err != nil {
		return nil, err
	}
	password, hash := "", ""
	if action == "reset-password" {
		policy := config.Runtime().AccountPolicy
		password = input.Password
		if input.GeneratePassword || password == "" {
			password, err = randomPassword(policy.MinPasswordLength)
			if err != nil {
				return nil, err
			}
		}
		if err = ValidatePassword(password, password, policy.MinPasswordLength); err != nil {
			return nil, err
		}
		encoded, e := bcrypt.GenerateFromPassword([]byte(password), policy.BcryptCost)
		if e != nil {
			return nil, apperrors.ErrDependency
		}
		hash = string(encoded)
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var target *model.UserAccount
	// 顺序锁定管理员和目标账号，校验凭据与修订号，再将账号变更、关联权限回收及审计一起提交。
	err = database.Transaction(func(tx *gorm.DB) error {
		if _, e := dal.ReadSiteRuntimeState(tx, true); e != nil {
			return e
		}
		ids := []int64{actorID, targetID}
		if targetID < actorID {
			ids[0], ids[1] = targetID, actorID
		}
		users := map[int64]*model.UserAccount{}
		for _, id := range ids {
			user, e := dal.QueryAccount(tx, id, true)
			if e != nil {
				return e
			}
			users[id] = user
		}
		actor := users[actorID]
		target = users[targetID]
		if actor == nil || actor.Status != model.AccountActive || actor.Role != model.RoleAdmin || actor.MustChangePassword || actor.AuthVersion != version || actor.PasswordHash != verified.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if action == "reset-profile" {
			// Both account locks are already held; take the acting session lock next.
			acting, _, _, e := actingSessionTx(tx, input.ActingToken, true, true)
			if e != nil {
				return e
			}
			if acting.ID != actorID || acting.AuthVersion != version {
				return apperrors.ErrUnauthorized
			}
		}
		if target == nil {
			return apperrors.ErrNotFound
		}
		if target.Revision != revision {
			return apperrors.ErrConflict
		}
		before := accountSummary(target)
		if action == "reset-profile" {
			before = profileSummary(target)
		}
		fields, e := adminTransition(tx, actorID, target, action, input, hash)
		if e != nil {
			return e
		}
		fields["revision"] = target.Revision + 1
		fields["update_time"] = time.Now()
		updated, e := dal.UpdateAccount(tx, targetID, revision, fields)
		if e != nil {
			return e
		}
		if !updated {
			return apperrors.ErrConflict
		}
		target, e = dal.QueryAccount(tx, targetID, false)
		if e != nil {
			return e
		}
		return writeAudit(tx, actorID, action, "user", targetID, before, input.Reason, target)
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &CreatedAccount{User: target, InitialPassword: password, PasswordExpiresAt: target.PasswordExpiresAt}, nil
}

// adminTransition 计算管理员操作对应的账号字段，并执行应用权限、邀请码和项目状态的关联变更。
// 调用方须持有全局账号管理锁与双方账号行锁；这里阻止危险的自操作及最后一名可用管理员被移除。
func adminTransition(tx *gorm.DB, actorID int64, target *model.UserAccount, action string, input AdminInput, passwordHash string) (map[string]interface{}, error) {
	if target.Status == model.AccountDeleted {
		return nil, apperrors.ErrConflict
	}
	if actorID == target.ID && (action == "ban" || action == "delete" || action == "role" || action == "reset-password") {
		return nil, apperrors.WithMessage(apperrors.ErrForbidden, "此操作需由另一名管理员执行")
	}
	losesAdmin := target.Role == model.RoleAdmin && target.Status == model.AccountActive && (action == "ban" || action == "delete" || (action == "role" && input.Role != model.RoleAdmin))
	if losesAdmin {
		var count int64
		if err := tx.Table(dal.AccountTable).Where("role = ? AND status = ? AND must_change_password = ?", model.RoleAdmin, model.AccountActive, false).Count(&count).Error; err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, apperrors.WithMessage(apperrors.ErrForbidden, "必须保留至少一名可用管理员")
		}
	}
	now := time.Now()
	fields := map[string]interface{}{}
	switch action {
	case "ban", "delete":
		fields["status"] = model.AccountBanned
		fields["auth_version"] = target.AuthVersion + 1
		fields["library_enabled"] = false
		fields["webdav_permission"] = model.WebDAVNone
		if action == "delete" {
			fields["status"] = model.AccountDeleted
			fields["deleted_at"] = now
			fields["avatar_version"] = int64(0)
			if err := tx.Table(dal.AccountAvatarTable).Where("user_id = ?", target.ID).Delete(&model.UserAvatar{}).Error; err != nil {
				return nil, err
			}
		}
		if err := suspendApplicationsTx(tx, target.ID, action == "delete"); err != nil {
			return nil, err
		}
		if err := removeScopesTx(tx, target.ID, []string{"library:", "webdav:"}); err != nil {
			return nil, err
		}
		if err := tx.Table(dal.InvitationTable).Where("inviter_user_id = ? AND status = ?", target.ID, "unused").Updates(map[string]interface{}{"status": "revoked", "revoked_at": now, "revoke_reason": "邀请人账号不可用"}).Error; err != nil {
			return nil, err
		}
		if action == "delete" {
			purge := now.Add(time.Duration(config.Runtime().WebProjects.DeleteRetentionDays) * 24 * time.Hour)
			if err := tx.Table(dal.WebProjectTable).Where("owner_user_id = ? AND status <> ?", target.ID, model.WebProjectStatusDeleted).Updates(map[string]interface{}{"status": model.WebProjectStatusDeleted, "deleted_at": now, "purge_after": purge, "revision": gorm.Expr("revision + 1"), "update_time": now}).Error; err != nil {
				return nil, err
			}
		}
	case "unban":
		if target.Status != model.AccountBanned {
			return nil, apperrors.ErrConflict
		}
		fields["status"] = model.AccountActive
	case "reset-profile":
		if !input.ResetDisplayName && !input.ResetAvatar {
			return nil, apperrors.ErrInvalid
		}
		if input.ResetDisplayName {
			fields["display_name"] = ""
		}
		if input.ResetAvatar {
			fields["avatar_version"] = int64(0)
			if err := tx.Table(dal.AccountAvatarTable).Where("user_id = ?", target.ID).Delete(&model.UserAvatar{}).Error; err != nil {
				return nil, err
			}
		}
	case "role":
		if input.Role != model.RoleAdmin && input.Role != model.RoleUser {
			return nil, apperrors.ErrInvalid
		}
		fields["role"] = input.Role
		fields["auth_version"] = target.AuthVersion + 1
	case "library-permission":
		fields["library_enabled"] = input.Enabled
		if !input.Enabled {
			if err := removeScopesTx(tx, target.ID, []string{"library:"}); err != nil {
				return nil, err
			}
		}
	case "webdav-permission":
		if input.Permission != model.WebDAVNone && input.Permission != model.WebDAVRead && input.Permission != model.WebDAVWrite {
			return nil, apperrors.ErrInvalid
		}
		fields["webdav_permission"] = input.Permission
		prefixes := []string{}
		if input.Permission == model.WebDAVNone {
			prefixes = []string{"webdav:"}
		} else if input.Permission == model.WebDAVRead {
			prefixes = []string{"webdav:write"}
		}
		if len(prefixes) > 0 {
			if err := removeScopesTx(tx, target.ID, prefixes); err != nil {
				return nil, err
			}
		}
	case "logout-all", "reset-password":
		fields["auth_version"] = target.AuthVersion + 1
		if err := suspendApplicationsTx(tx, target.ID, false); err != nil {
			return nil, err
		}
		if action == "reset-password" {
			expiry := now.Add(time.Duration(config.Runtime().AccountPolicy.TemporaryPasswordTTLDays) * 24 * time.Hour)
			fields["password_hash"] = passwordHash
			fields["must_change_password"] = true
			fields["password_expires_at"] = expiry
			fields["library_enabled"] = false
			fields["webdav_permission"] = model.WebDAVNone
			if err := removeScopesTx(tx, target.ID, []string{"library:", "webdav:"}); err != nil {
				return nil, err
			}
		}
	default:
		return nil, apperrors.ErrInvalid
	}
	return fields, nil
}

func suspendApplicationsTx(tx *gorm.DB, id int64, revoke bool) error {
	fields := map[string]interface{}{"status": model.ApplicationStatusDisabled, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()}
	if revoke {
		fields["status"] = model.ApplicationStatusRevoked
		fields["active_slot"] = nil
	}
	return tx.Table(dal.ApplicationTable).Where("owner_user_id = ? AND status <> ?", id, model.ApplicationStatusRevoked).Updates(fields).Error
}

// removeScopesTx 锁定账号下全部应用，移除匹配前缀的授权范围，仅对范围发生变化的应用增加修订号。
func removeScopesTx(tx *gorm.DB, id int64, prefixes []string) error {
	var applications []*model.Application
	if err := tx.Table(dal.ApplicationTable).Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "scopes").Where("owner_user_id = ?", id).Order("id ASC").Find(&applications).Error; err != nil {
		return err
	}
	for _, app := range applications {
		kept := []string{}
		for _, scope := range app.Scopes {
			remove := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(scope, prefix) {
					remove = true
				}
			}
			if !remove {
				kept = append(kept, scope)
			}
		}
		if len(kept) == len(app.Scopes) {
			continue
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		if err = tx.Table(dal.ApplicationTable).Where("id = ?", app.ID).Updates(map[string]interface{}{"scopes": string(encoded), "revision": gorm.Expr("revision + 1"), "update_time": time.Now()}).Error; err != nil {
			return err
		}
	}
	return nil
}

func accountSummary(user *model.UserAccount) string {
	if user == nil {
		return "{}"
	}
	data, _ := json.Marshal(map[string]interface{}{"status": user.Status, "role": user.Role, "library_enabled": user.LibraryEnabled, "webdav_permission": user.WebDAVPermission, "auth_version": user.AuthVersion})
	return string(data)
}

func profileSummary(user *model.UserAccount) string {
	if user == nil {
		return "{}"
	}
	data, _ := json.Marshal(map[string]interface{}{"display_name": user.DisplayName, "avatar_version": user.AvatarVersion, "revision": user.Revision})
	return string(data)
}

func writeAudit(tx *gorm.DB, actorID int64, action, targetType string, targetID int64, before, reason string, after *model.UserAccount) error {
	if before == "" {
		before = "{}"
	}
	audit := model.AdminAuditLog{ActorUserID: actorID, Action: action, TargetType: targetType, TargetID: targetID, BeforeSummary: before, AfterSummary: accountSummary(after), Reason: reason, Result: "success", CreateTime: time.Now()}
	if action == "reset-profile" {
		audit.AfterSummary = profileSummary(after)
	}
	return tx.Table(dal.AdminAuditTable).Create(&audit).Error
}
