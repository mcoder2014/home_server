package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type InvitationPage struct {
	Items        []*model.UserInvitation `json:"items"`
	Remaining    int                     `json:"remaining"`
	MonthlyLimit int                     `json:"monthly_limit"`
	QuotaMonth   string                  `json:"quota_month"`
	NextResetAt  time.Time               `json:"next_reset_at"`
	Enabled      bool                    `json:"enabled"`
}

type CreatedInvitation struct {
	Invitation *model.UserInvitation `json:"invitation"`
	Code       string                `json:"code,omitempty"`
}

func ListInvitations(ctx context.Context, id int64) (*InvitationPage, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	user, err := dal.QueryAccount(database, id, false)
	if err != nil {
		return nil, normalizeError(err)
	}
	if user == nil {
		return nil, apperrors.ErrNotFound
	}
	state, err := dal.ReadSiteRuntimeState(database, false)
	if err != nil {
		return nil, normalizeError(err)
	}
	enabled, err := EnabledTx(database, "registration", "enabled", false)
	if err != nil {
		return nil, err
	}
	month, next := InvitationMonth(time.Now())
	page := &InvitationPage{Items: []*model.UserInvitation{}, MonthlyLimit: 3, QuotaMonth: month, NextResetAt: next, Enabled: enabled}
	if err = database.Table(dal.InvitationTable).Select(dal.InvitationColumns).Where("inviter_user_id = ?", id).Order("id DESC").Limit(100).Find(&page.Items).Error; err != nil {
		return nil, normalizeError(err)
	}
	var used int64
	if err = database.Table(dal.InvitationTable).Where("inviter_user_id = ? AND quota_month = ?", id, month).Count(&used).Error; err != nil {
		return nil, normalizeError(err)
	}
	if !time.Now().Before(user.InviteEligibleAt) && user.Status == model.AccountActive && !user.MustChangePassword {
		page.Remaining = 3 - int(used)
		if page.Remaining < 0 {
			page.Remaining = 0
		}
	}
	usedIDs := []int64{}
	for _, inv := range page.Items {
		if inv.UsedByUserID != nil {
			usedIDs = append(usedIDs, *inv.UsedByUserID)
		}
	}
	names := map[int64]string{}
	if len(usedIDs) > 0 {
		var usedUsers []model.UserAccount
		if e := database.Table(dal.AccountTable).Select("id", "username").Where("id IN ?", usedIDs).Find(&usedUsers).Error; e != nil {
			return nil, normalizeError(e)
		}
		for _, usedUser := range usedUsers {
			names[usedUser.ID] = usedUser.Username
		}
	}
	for _, inv := range page.Items {
		if inv.Status == "unused" {
			if inv.RegistrationEpoch != state.RegistrationEpoch || user.Status != model.AccountActive {
				inv.Status = "revoked"
				inv.RevokeReason = "邀请已失效"
			} else if !inv.ExpiresAt.After(time.Now()) {
				inv.Status = "expired"
			}
		}
		if inv.UsedByUserID != nil {
			inv.UsedByUserName = names[*inv.UsedByUserID]
		}
	}
	return page, nil
}

func CreateInvitation(ctx context.Context, id, version int64, note, requestID string) (*CreatedInvitation, error) {
	if len(note) > 128 || !requestIDPattern.MatchString(requestID) {
		return nil, apperrors.ErrInvalid
	}
	code, err := randomSecret("iv_cq_", 32)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(code))
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	result := &CreatedInvitation{}
	err = database.Transaction(func(tx *gorm.DB) error {
		state, e := dal.ReadSiteRuntimeState(tx, true)
		if e != nil {
			return e
		}
		enabled, e := EnabledTx(tx, "registration", "enabled", true)
		if e != nil {
			return e
		}
		user, e := RequireUserTx(tx, id, version, false)
		if e != nil {
			return e
		}
		var existing model.UserInvitation
		e = tx.Table(dal.InvitationTable).Select(dal.InvitationColumns).Where("inviter_user_id = ? AND request_id = ?", id, requestID).Take(&existing).Error
		if e == nil {
			if existing.Note != strings.TrimSpace(note) {
				return apperrors.ErrConflict
			}
			result.Invitation = &existing
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if !enabled {
			return apperrors.WithMessage(apperrors.ErrForbidden, "网站暂未开放注册")
		}
		now := time.Now()
		month, _ := InvitationMonth(now)
		if now.Before(user.InviteEligibleAt) {
			return apperrors.WithMessage(apperrors.ErrForbidden, "下个月起可以生成邀请码")
		}
		var slots []int
		if e = tx.Table(dal.InvitationTable).Where("inviter_user_id = ? AND quota_month = ?", id, month).Pluck("slot", &slots).Error; e != nil {
			return e
		}
		occupied := map[int]bool{}
		for _, slot := range slots {
			occupied[slot] = true
		}
		slot := 0
		for candidate := 1; candidate <= 3; candidate++ {
			if !occupied[candidate] {
				slot = candidate
				break
			}
		}
		if slot == 0 {
			return apperrors.WithMessage(apperrors.ErrRateLimited, "本月三个邀请码已用完")
		}
		inv := &model.UserInvitation{InviterUserID: id, QuotaMonth: month, Slot: slot, TokenDigest: digest[:], TokenHint: code[len(code)-6:], RegistrationEpoch: state.RegistrationEpoch, Status: "unused", ExpiresAt: now.Add(7 * 24 * time.Hour), Note: strings.TrimSpace(note), RequestID: requestID, CreateTime: now}
		if e = tx.Table(dal.InvitationTable).Create(inv).Error; e != nil {
			return e
		}
		result.Invitation = inv
		result.Code = code
		return nil
	})
	return result, normalizeError(err)
}

func ValidateInvitation(ctx context.Context, code string) (*model.UserInvitation, error) {
	if len(code) < 16 || len(code) > 128 {
		return nil, apperrors.ErrNotFound
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	enabled, err := EnabledTx(database, "registration", "enabled", false)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, apperrors.WithMessage(apperrors.ErrForbidden, "网站暂未开放注册")
	}
	state, err := dal.ReadSiteRuntimeState(database, false)
	if err != nil {
		return nil, normalizeError(err)
	}
	digest := sha256.Sum256([]byte(code))
	inv, err := dal.QueryInvitation(database, digest[:], 0, false)
	if err != nil {
		return nil, normalizeError(err)
	}
	if inv == nil || inv.Status != "unused" || inv.RegistrationEpoch != state.RegistrationEpoch || !inv.ExpiresAt.After(time.Now()) {
		return nil, apperrors.ErrNotFound
	}
	owner, err := dal.QueryAccount(database, inv.InviterUserID, false)
	if err != nil {
		return nil, normalizeError(err)
	}
	if owner == nil || owner.Status != model.AccountActive {
		return nil, apperrors.ErrNotFound
	}
	return inv, nil
}

type RegistrationInput struct {
	UserName        string `json:"user_name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	InvitationCode  string `json:"invitation_code"`
}

// Register commits the account, its login alias and one-time invite consumption
// together. Global epoch and inviter/account locks serialize closing registration
// or banning an inviter against an already-open registration form.
func Register(ctx context.Context, input RegistrationInput) (*model.UserAccount, error) {
	username, err := NormalizeUsername(input.UserName)
	if err != nil {
		return nil, err
	}
	policy := config.Runtime().AccountPolicy
	if err = ValidatePassword(input.Password, input.ConfirmPassword, policy.MinPasswordLength); err != nil {
		return nil, err
	}
	preview, err := ValidateInvitation(ctx, input.InvitationCode)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), policy.BcryptCost)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var user *model.UserAccount
	err = database.Transaction(func(tx *gorm.DB) error {
		state, e := dal.ReadSiteRuntimeState(tx, true)
		if e != nil {
			return e
		}
		enabled, e := EnabledTx(tx, "registration", "enabled", true)
		if e != nil {
			return e
		}
		if !enabled {
			return apperrors.WithMessage(apperrors.ErrForbidden, "网站暂未开放注册")
		}
		inviter, e := dal.QueryAccount(tx, preview.InviterUserID, true)
		if e != nil {
			return e
		}
		if inviter == nil || inviter.Status != model.AccountActive {
			return apperrors.ErrNotFound
		}
		inv, e := dal.QueryInvitation(tx, nil, preview.ID, true)
		if e != nil {
			return e
		}
		now := time.Now()
		if inv == nil || inv.Status != "unused" || inv.InviterUserID != inviter.ID || inv.RegistrationEpoch != state.RegistrationEpoch || !inv.ExpiresAt.After(now) {
			return apperrors.ErrNotFound
		}
		_, eligible := InvitationMonth(now)
		user = &model.UserAccount{Username: username, UsernameKey: username, DisplayName: username, PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleUser, WebDAVPermission: model.WebDAVNone, AuthVersion: 1, Revision: 1, InviteEligibleAt: eligible, Source: "invitation", InvitedByUserID: &inviter.ID, CreateTime: now, UpdateTime: now}
		if e = dal.InsertAccount(tx, user); e != nil {
			return e
		}
		result := tx.Table(dal.InvitationTable).Where("id = ? AND status = ?", inv.ID, "unused").Updates(map[string]interface{}{"status": "used", "used_by_user_id": user.ID, "used_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
	return user, normalizeError(err)
}

func RevokeInvitation(ctx context.Context, id, version, invitationID int64) error {
	database, err := database(ctx)
	if err != nil {
		return err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		if _, e := RequireUserTx(tx, id, version, false); e != nil {
			return e
		}
		inv, e := dal.QueryInvitation(tx, nil, invitationID, true)
		if e != nil {
			return e
		}
		if inv == nil || inv.InviterUserID != id {
			return apperrors.ErrNotFound
		}
		if inv.Status != "unused" {
			return apperrors.ErrConflict
		}
		return tx.Table(dal.InvitationTable).Where("id = ?", invitationID).Updates(map[string]interface{}{"status": "revoked", "revoked_at": time.Now(), "revoke_reason": "邀请人已撤销"}).Error
	})
	return normalizeError(err)
}
