package webcomments

import (
	"errors"
	"sort"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Authorize holds configuration, sorted user rows, application then project for
// writes. This matches project/account writers and rechecks revocation at commit.
// Owner history is readable after disable/delete; readers never bypass live ACLs.
func Authorize(tx *gorm.DB, id int64, principal *utils.Principal, write, history bool) (*model.WebProject, error) {
	var project model.WebProject
	err := tx.Table(dal.WebProjectTable).Select(dal.WebProjectColumns()).Where("id = ?", id).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if write && principal == nil {
		return nil, apperrors.ErrUnauthorized
	}
	if principal != nil && principal.Kind == "application" {
		enabled, e := accounts.EnabledTx(tx, "auth", "applications_enabled", write)
		if e != nil {
			return nil, e
		}
		if !enabled {
			return nil, apperrors.ErrForbidden
		}
	}
	enabled, err := accounts.EnabledTx(tx, "web_projects", "enabled", write)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, apperrors.ErrNotFound
	}
	if accounts.DatabaseMode() {
		ids := []int64{project.OwnerUserID}
		if principal != nil && principal.UserID != project.OwnerUserID {
			ids = append(ids, principal.UserID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var users []model.UserAccount
		q := tx.Table(dal.AccountTable).Select("id", "status", "must_change_password", "auth_version").Where("id IN ?", ids).Order("id ASC")
		if write {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err = q.Find(&users).Error; err != nil {
			return nil, apperrors.ErrDependency
		}
		if len(users) != len(ids) {
			return nil, apperrors.ErrNotFound
		}
		for _, u := range users {
			if u.ID == project.OwnerUserID && u.Status != model.AccountActive {
				return nil, apperrors.ErrNotFound
			}
			if principal != nil && u.ID == principal.UserID && (u.Status != model.AccountActive || u.MustChangePassword || u.AuthVersion != principal.AuthVersion) {
				return nil, apperrors.ErrUnauthorized
			}
		}
	}
	if principal != nil && (principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now())) {
		return nil, apperrors.ErrUnauthorized
	}
	if write && principal.Kind == "application" {
		if err = accounts.RequireApplicationSnapshotTx(tx, principal, "web-comments:write"); err != nil {
			return nil, err
		}
	}
	// The first lookup discovers immutable ownership; this second lookup takes the
	// project lock only after all account locks and sees current ACL/publication.
	q := tx.Table(dal.WebProjectTable).Select(dal.WebProjectColumns()).Where("id = ?", id)
	if write {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = q.Take(&project).Error; err != nil {
		return nil, apperrors.ErrNotFound
	}
	uid := int64(0)
	if principal != nil {
		uid = principal.UserID
	}
	if history && !write && uid == project.OwnerUserID {
		return &project, nil
	}
	if project.Status != model.WebProjectStatusEnabled || project.CurrentReleaseID == nil || (project.ModerationStatus != "" && project.ModerationStatus != "normal") {
		return nil, apperrors.ErrNotFound
	}
	member := false
	if project.AccessMode == model.WebProjectAccessMembers && uid != project.OwnerUserID {
		var row model.WebProjectMember
		query := tx.Table(dal.WebProjectMemberTable).Select("project_id", "user_id").Where("project_id = ? AND user_id = ?", id, uid)
		if write {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		err = query.Take(&row).Error
		if err == nil {
			member = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrDependency
		}
	}
	if !webprojects.CanReadProject(project.AccessMode, project.OwnerUserID, uid, member) {
		return nil, apperrors.ErrNotFound
	}
	return &project, nil
}
