package accounts

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

func ListActiveUsers(ctx context.Context) ([]*model.UserAccount, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var users []*model.UserAccount
	err = database.Table(dal.AccountTable).Select(dal.AccountColumns).Where("status = ? AND must_change_password = ?", model.AccountActive, false).Order("id ASC").Limit(10000).Find(&users).Error
	return users, normalizeError(err)
}

// UserDetail 组合账号资料及最近的应用、网页项目和邀请记录，应用列表仅选取管理页面需要的非密钥字段。
func UserDetail(ctx context.Context, id int64) (map[string]interface{}, error) {
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
	var apps []struct {
		ID        int64     `json:"id,string"`
		Name      string    `json:"name"`
		Status    int       `json:"status"`
		Scopes    []string  `gorm:"serializer:json" json:"scopes"`
		ExpiresAt time.Time `json:"expires_at"`
		Revision  int64     `json:"revision"`
	}
	if err = database.Table(dal.ApplicationTable).Select("id", "name", "status", "scopes", "expires_at", "revision").Where("owner_user_id = ?", id).Order("id DESC").Limit(100).Find(&apps).Error; err != nil {
		return nil, normalizeError(err)
	}
	var projects []model.WebProject
	if err = database.Table(dal.WebProjectTable).Select("id", "name", "slug", "status", "access_mode", "revision", "moderation_status", "moderation_reason", "create_time", "update_time").Where("owner_user_id = ?", id).Order("id DESC").Limit(100).Find(&projects).Error; err != nil {
		return nil, normalizeError(err)
	}
	invites, err := ListInvitations(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"user": user, "applications": apps, "projects": projects, "invitations": invites.Items}, nil
}

type UserSummary struct {
	*model.UserAccount
	ApplicationCount       int64 `json:"application_count"`
	ActiveApplicationCount int64 `json:"active_application_count"`
	ProjectCount           int64 `json:"project_count"`
	PublishedProjectCount  int64 `json:"published_project_count"`
}

type UserPage struct {
	Items      []*UserSummary `json:"items"`
	NextCursor string         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

// ListUsers 按账号游标分页，并批量汇总本页用户的应用与项目数量，多读一条判断是否有下一页。
func ListUsers(ctx context.Context, filter dal.AccountFilter) (*UserPage, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 || filter.Cursor < 0 {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	filter.Limit++
	users, err := dal.ListAccounts(database, filter)
	if err != nil {
		return nil, normalizeError(err)
	}
	page := &UserPage{Items: []*UserSummary{}, HasMore: len(users) > limit}
	if page.HasMore {
		users = users[:limit]
	}
	ids := make([]int64, 0, len(users))
	byID := map[int64]*UserSummary{}
	for _, user := range users {
		v := &UserSummary{UserAccount: user}
		page.Items = append(page.Items, v)
		byID[user.ID] = v
		ids = append(ids, user.ID)
	}
	if len(ids) == 0 {
		return page, nil
	}
	var counts []struct {
		OwnerUserID int64
		Total       int64
		Active      int64
	}
	err = database.Table(dal.ApplicationTable).Select("owner_user_id, COUNT(*) AS total, SUM(status = 1 AND expires_at > ?) AS active", time.Now()).Where("owner_user_id IN ? AND status <> ?", ids, model.ApplicationStatusRevoked).Group("owner_user_id").Scan(&counts).Error
	if err != nil {
		return nil, normalizeError(err)
	}
	for _, n := range counts {
		byID[n.OwnerUserID].ApplicationCount = n.Total
		byID[n.OwnerUserID].ActiveApplicationCount = n.Active
	}
	counts = nil
	err = database.Table(dal.WebProjectTable).Select("owner_user_id, COUNT(*) AS total, SUM(status = 2 AND current_release_id IS NOT NULL AND moderation_status = 'normal') AS active").Where("owner_user_id IN ? AND status <> ?", ids, model.WebProjectStatusDeleted).Group("owner_user_id").Scan(&counts).Error
	if err != nil {
		return nil, normalizeError(err)
	}
	for _, n := range counts {
		byID[n.OwnerUserID].ProjectCount = n.Total
		byID[n.OwnerUserID].PublishedProjectCount = n.Active
	}
	if page.HasMore {
		page.NextCursor = strconv.FormatInt(users[len(users)-1].ID, 10)
	}
	return page, nil
}

type AuditView struct {
	model.AdminAuditLog
	ActorUserName string                 `json:"actor_user_name"`
	Before        map[string]interface{} `json:"before"`
	After         map[string]interface{} `json:"after"`
}

// ListAudit 按目标筛选并分页读取管理员审计，批量补充操作者名称，将变更前后摘要解码为展示对象。
func ListAudit(ctx context.Context, cursor int64, limit int, targetType string, targetID int64) (map[string]interface{}, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 || cursor < 0 {
		return nil, apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	query := database.Table(dal.AdminAuditTable).Select(dal.AdminAuditColumns)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	if targetType != "" {
		query = query.Where("target_type = ?", targetType)
	}
	if targetID > 0 {
		query = query.Where("target_id = ?", targetID)
	}
	var logs []model.AdminAuditLog
	if err = query.Order("id DESC").Limit(limit + 1).Find(&logs).Error; err != nil {
		return nil, normalizeError(err)
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	ids := []int64{}
	for _, log := range logs {
		ids = append(ids, log.ActorUserID)
	}
	var actors []model.UserAccount
	if len(ids) > 0 {
		if err = database.Table(dal.AccountTable).Select("id", "username").Where("id IN ?", ids).Find(&actors).Error; err != nil {
			return nil, normalizeError(err)
		}
	}
	names := map[int64]string{}
	for _, actor := range actors {
		names[actor.ID] = actor.Username
	}
	items := []AuditView{}
	for _, log := range logs {
		v := AuditView{AdminAuditLog: log, ActorUserName: names[log.ActorUserID]}
		_ = json.Unmarshal([]byte(log.BeforeSummary), &v.Before)
		_ = json.Unmarshal([]byte(log.AfterSummary), &v.After)
		items = append(items, v)
	}
	next := ""
	if hasMore {
		next = strconv.FormatInt(logs[len(logs)-1].ID, 10)
	}
	return map[string]interface{}{"items": items, "next_cursor": next, "has_more": hasMore}, nil
}

// AdminRevokeInvitation 验证管理员密码后撤销指定账号的未使用邀请，并将撤销原因与审计记录一起提交。
func AdminRevokeInvitation(ctx context.Context, actorID, version, targetID, revision, invitationID int64, input AdminInput) error {
	verified, err := VerifyAdminPassword(ctx, actorID, input.CurrentPassword)
	if err != nil {
		return err
	}
	if input.Reason == "" || len(input.Reason) > 512 {
		return apperrors.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return err
	}
	// 顺序锁定双方账号和邀请码，复核管理员凭据、目标修订号及邀请归属后撤销并写审计。
	err = database.Transaction(func(tx *gorm.DB) error {
		if _, e := dal.ReadSiteRuntimeState(tx, true); e != nil {
			return e
		}
		ids := []int64{actorID, targetID}
		if targetID < actorID {
			ids[0], ids[1] = targetID, actorID
		}
		var actor, target *model.UserAccount
		for _, id := range ids {
			u, e := dal.QueryAccount(tx, id, true)
			if e != nil {
				return e
			}
			if id == actorID {
				actor = u
			}
			if id == targetID {
				target = u
			}
		}
		if actor == nil || actor.Role != model.RoleAdmin || actor.Status != model.AccountActive || actor.AuthVersion != version || actor.PasswordHash != verified.PasswordHash || actor.MustChangePassword {
			return apperrors.ErrUnauthorized
		}
		if target == nil {
			return apperrors.ErrNotFound
		}
		if target.Revision != revision {
			return apperrors.ErrConflict
		}
		inv, e := dal.QueryInvitation(tx, nil, invitationID, true)
		if e != nil {
			return e
		}
		if inv == nil || inv.InviterUserID != targetID {
			return apperrors.ErrNotFound
		}
		if inv.Status != "unused" {
			return apperrors.ErrConflict
		}
		if e = tx.Table(dal.InvitationTable).Where("id = ?", invitationID).Updates(map[string]interface{}{"status": "revoked", "revoked_at": time.Now(), "revoke_reason": input.Reason}).Error; e != nil {
			return e
		}
		return writeAudit(tx, actorID, "revoke-invitation", "invitation", invitationID, "{}", input.Reason, nil)
	})
	return normalizeError(err)
}
