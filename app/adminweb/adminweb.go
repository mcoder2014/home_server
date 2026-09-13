package adminweb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

type Filter struct {
	Cursor, OwnerUserID                         int64
	Limit                                       int
	Query, Status, AccessMode, ModerationStatus string
}

type MutationRequest struct {
	Reason          string `json:"reason"`
	CurrentPassword string `json:"current_password"`
}

type ProjectView struct {
	*webprojects.ProjectView
	OwnerUserID string `json:"owner_user_id"`
	UserName    string `json:"user_name"`
}

type Page struct {
	Items      []*ProjectView `json:"items"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor"`
}

type Detail struct {
	Project  *ProjectView               `json:"project"`
	Releases []*model.WebProjectRelease `json:"releases"`
}

func requireAdmin(ctx context.Context, actorID, version int64) (*model.UserAccount, error) {
	if !accounts.DatabaseMode() || db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	actor, err := accounts.GetByID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor == nil || actor.Status != model.AccountActive || actor.MustChangePassword || version <= 0 || actor.AuthVersion != version {
		return nil, apperrors.ErrUnauthorized
	}
	if actor.Role != model.RoleAdmin {
		return nil, apperrors.ErrForbidden
	}
	return actor, nil
}

// List performs a bounded cross-owner project read and one batched username
// lookup after verifying the current administrator session. No visibility
// filter applies to moderation, and this endpoint never returns credentials.
func List(ctx context.Context, actorID, version int64, filter Filter) (*Page, error) {
	if _, err := requireAdmin(ctx, actorID, version); err != nil {
		return nil, err
	}
	if filter.Cursor < 0 || filter.OwnerUserID < 0 || utf8.RuneCountInString(filter.Query) > 256 {
		return nil, apperrors.ErrInvalid
	}
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	query := db.MasterDB().WithContext(ctx).Table(dal.WebProjectTable).Select(dal.WebProjectColumns())
	if filter.Cursor > 0 {
		query = query.Where("id < ?", filter.Cursor)
	}
	if filter.OwnerUserID > 0 {
		query = query.Where("owner_user_id = ?", filter.OwnerUserID)
	}
	if filter.Status != "" && filter.Status != "all" {
		status, ok := model.ParseWebProjectStatus(filter.Status)
		if !ok {
			return nil, apperrors.ErrInvalid
		}
		query = query.Where("status = ?", status)
	}
	if filter.AccessMode != "" {
		mode, ok := model.ParseWebProjectAccess(filter.AccessMode)
		if !ok {
			return nil, apperrors.ErrInvalid
		}
		query = query.Where("access_mode = ?", mode)
	}
	if filter.ModerationStatus != "" {
		if filter.ModerationStatus != "normal" && filter.ModerationStatus != "blocked" && filter.ModerationStatus != "deleted" {
			return nil, apperrors.ErrInvalid
		}
		query = query.Where("moderation_status = ?", filter.ModerationStatus)
	}
	if filter.Query != "" {
		prefix := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(filter.Query)) + "%"
		query = query.Where("name LIKE ? OR slug LIKE ?", prefix, prefix)
	}
	var projects []*model.WebProject
	if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&projects).Error; err != nil {
		return nil, apperrors.ErrDependency
	}
	result := &Page{Items: []*ProjectView{}, HasMore: len(projects) > filter.Limit}
	if result.HasMore {
		projects = projects[:filter.Limit]
	}
	owners := make([]int64, 0, len(projects))
	for _, project := range projects {
		owners = append(owners, project.OwnerUserID)
	}
	names := map[int64]string{}
	if len(owners) > 0 {
		var users []struct {
			ID       int64
			Username string
		}
		if err := db.MasterDB().WithContext(ctx).Table(dal.AccountTable).Select("id", "username").Where("id IN ?", owners).Find(&users).Error; err != nil {
			return nil, apperrors.ErrDependency
		}
		for _, user := range users {
			names[user.ID] = user.Username
		}
	}
	for _, project := range projects {
		result.Items = append(result.Items, projectView(project, names[project.OwnerUserID]))
	}
	if result.HasMore {
		result.NextCursor = strconv.FormatInt(projects[len(projects)-1].ID, 10)
	}
	return result, nil
}

func Get(ctx context.Context, actorID, version, projectID int64) (*Detail, error) {
	if _, err := requireAdmin(ctx, actorID, version); err != nil {
		return nil, err
	}
	if projectID <= 0 {
		return nil, apperrors.ErrInvalid
	}
	project, err := dal.QueryWebProjectByID(projectID)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if project == nil {
		return nil, apperrors.ErrNotFound
	}
	owner, err := accounts.GetByID(ctx, project.OwnerUserID)
	if err != nil || owner == nil {
		return nil, apperrors.ErrDependency
	}
	releases, err := dal.ListWebProjectReleases(projectID, 0, 1001)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if len(releases) > 1000 {
		return nil, apperrors.ErrRateLimited
	}
	if releases == nil {
		releases = []*model.WebProjectRelease{}
	}
	return &Detail{Project: projectView(project, owner.Username), Releases: releases}, nil
}

// Change never grants normal browsing rights. It rechecks the administrator's
// exact authenticated password/version under sorted user locks, then updates
// moderation and its audit record atomically. Restoring moderator-deleted
// content remains blocked until a separate unblock action approves it.
func Change(ctx context.Context, actorID, version, projectID, revision int64, action string, input MutationRequest) (*ProjectView, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 512 || projectID <= 0 || revision <= 0 {
		return nil, apperrors.ErrInvalid
	}
	verified, err := accounts.VerifyAdminPassword(ctx, actorID, input.CurrentPassword)
	if err != nil {
		return nil, err
	}
	if verified.AuthVersion != version {
		return nil, apperrors.ErrUnauthorized
	}
	hint, err := dal.QueryWebProjectByID(projectID)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if hint == nil {
		return nil, apperrors.ErrNotFound
	}
	var project *model.WebProject
	var ownerName string
	err = db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := dal.ReadSiteRuntimeState(tx, true); err != nil {
			return err
		}
		ids := []int64{actorID, hint.OwnerUserID}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		users := map[int64]*model.UserAccount{}
		for _, id := range ids {
			if _, ok := users[id]; ok {
				continue
			}
			user, err := dal.QueryAccount(tx, id, true)
			if err != nil {
				return err
			}
			users[id] = user
		}
		actor, owner := users[actorID], users[hint.OwnerUserID]
		if actor == nil || actor.Status != model.AccountActive || actor.MustChangePassword || actor.Role != model.RoleAdmin || actor.AuthVersion != version || actor.PasswordHash != verified.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if owner == nil {
			return apperrors.ErrDependency
		}
		ownerName = owner.Username
		project, err = dal.LockWebProjectByID(tx, projectID)
		if err != nil {
			return err
		}
		if project == nil {
			return apperrors.ErrNotFound
		}
		if project.OwnerUserID != hint.OwnerUserID || project.Revision != revision {
			return apperrors.ErrConflict
		}
		before := moderationSummary(project)
		now := time.Now()
		fields := map[string]interface{}{"revision": revision + 1, "update_time": now, "moderation_reason": input.Reason, "moderated_by": actorID, "moderated_at": now}
		switch action {
		case "block":
			if project.Status == model.WebProjectStatusDeleted || project.ModerationStatus == "blocked" {
				return apperrors.ErrConflict
			}
			fields["moderation_status"] = "blocked"
		case "unblock":
			if project.ModerationStatus != "blocked" || project.Status == model.WebProjectStatusDeleted || owner.Status != model.AccountActive {
				return apperrors.ErrConflict
			}
			fields["moderation_status"] = "normal"
		case "delete":
			if project.Status == model.WebProjectStatusDeleted {
				return apperrors.ErrConflict
			}
			fields["status"] = model.WebProjectStatusDeleted
			fields["moderation_status"] = "deleted"
			fields["deleted_at"] = now
			days := config.Runtime().WebProjects.DeleteRetentionDays
			if days <= 0 {
				return apperrors.ErrDependency
			}
			fields["purge_after"] = now.Add(time.Duration(days) * 24 * time.Hour)
		case "restore":
			if project.Status != model.WebProjectStatusDeleted || owner.Status != model.AccountActive || project.DeletedAt == nil {
				return apperrors.ErrConflict
			}
			deadline := project.PurgeAfter
			if deadline == nil {
				days := config.Global().WebProjects.DeleteRetentionDays
				if days <= 0 {
					days = 7
				}
				value := project.DeletedAt.Add(time.Duration(days) * 24 * time.Hour)
				deadline = &value
			}
			if !deadline.After(now) {
				return apperrors.ErrNotFound
			}
			fields["status"] = model.WebProjectStatusDisabled
			fields["deleted_at"] = nil
			fields["purge_after"] = nil
			if project.ModerationStatus == "deleted" {
				fields["moderation_status"] = "blocked"
			}
		default:
			return apperrors.ErrInvalid
		}
		updated, err := dal.UpdateProjectFields(tx, project.OwnerUserID, project.ID, revision, fields)
		if err != nil {
			return err
		}
		if !updated {
			return apperrors.ErrConflict
		}
		project, err = dal.LockWebProjectByID(tx, projectID)
		if err != nil {
			return err
		}
		audit := model.AdminAuditLog{ActorUserID: actorID, Action: "web_project_" + action, TargetType: "web_project", TargetID: projectID, BeforeSummary: before, AfterSummary: moderationSummary(project), Reason: input.Reason, Result: "success", RequestID: uuid.NewString(), CreateTime: now}
		return tx.Table(dal.AdminAuditTable).Create(&audit).Error
	})
	if err != nil {
		var apiError *apperrors.APIError
		if errors.As(err, &apiError) {
			return nil, err
		}
		return nil, apperrors.ErrDependency
	}
	return projectView(project, ownerName), nil
}

// OpenPreview reauthorizes each requested asset and opens only a validated
// ready release file. The caller must close the file and return no-store
// responses; ordinary visibility and feature availability do not restrict it.
func OpenPreview(ctx context.Context, actorID, version, projectID, releaseID int64, requested string) (*os.File, error) {
	if _, err := requireAdmin(ctx, actorID, version); err != nil {
		return nil, err
	}
	project, err := dal.QueryWebProjectByID(projectID)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if project == nil {
		return nil, apperrors.ErrNotFound
	}
	release, err := dal.QueryWebProjectRelease(projectID, releaseID)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if release == nil || release.Status != model.WebProjectReleaseReady {
		return nil, apperrors.ErrNotFound
	}
	if release.UploadedBy != project.OwnerUserID || release.ProjectID != project.ID {
		return nil, apperrors.ErrDependency
	}
	conf := config.Runtime().WebProjects
	root, err := webprojects.ReleaseContentRoot(&conf, release)
	if err != nil {
		return nil, err
	}
	requested = strings.TrimPrefix(requested, "/")
	if requested == "" {
		requested = release.EntryFile
	}
	resolved, err := webprojects.ResolveContentPath(root, requested)
	if err != nil {
		return nil, apperrors.ErrNotFound
	}
	file, err := os.Open(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.ErrNotFound
		}
		return nil, apperrors.ErrDependency
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, apperrors.ErrNotFound
	}
	// Persist document access before returning an open file to the HTTP layer.
	// Static assets share the document's audit rather than generating one row
	// per CSS/image request. Failure cannot return any of the private content.
	extension := strings.ToLower(filepath.Ext(requested))
	if requested == release.EntryFile || extension == ".html" || extension == ".htm" {
		err = db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, err := dal.ReadSiteRuntimeState(tx, true); err != nil {
				return err
			}
			if _, err := accounts.RequireUserTx(tx, actorID, version, true); err != nil {
				return err
			}
			currentProject, err := dal.LockWebProjectByID(tx, projectID)
			if err != nil {
				return err
			}
			if currentProject == nil {
				return apperrors.ErrNotFound
			}
			currentRelease, err := dal.LockWebProjectRelease(tx, projectID, releaseID)
			if err != nil {
				return err
			}
			if currentRelease == nil || currentRelease.Status != model.WebProjectReleaseReady || currentRelease.UploadedBy != currentProject.OwnerUserID || currentRelease.StorageKey != release.StorageKey {
				return apperrors.ErrNotFound
			}
			summary, err := json.Marshal(map[string]string{"release_id": strconv.FormatInt(releaseID, 10), "path": requested})
			if err != nil {
				return err
			}
			audit := model.AdminAuditLog{ActorUserID: actorID, Action: "web_project_preview", TargetType: "web_project", TargetID: projectID, BeforeSummary: "{}", AfterSummary: string(summary), Reason: "管理员内容合规预览", Result: "success", RequestID: uuid.NewString(), CreateTime: time.Now()}
			return tx.Table(dal.AdminAuditTable).Create(&audit).Error
		})
		if err != nil {
			_ = file.Close()
			var apiError *apperrors.APIError
			if errors.As(err, &apiError) {
				return nil, err
			}
			return nil, apperrors.ErrDependency
		}
	}
	return file, nil
}

func projectView(project *model.WebProject, ownerName string) *ProjectView {
	current := ""
	if project.CurrentReleaseID != nil {
		current = strconv.FormatInt(*project.CurrentReleaseID, 10)
	}
	return &ProjectView{OwnerUserID: strconv.FormatInt(project.OwnerUserID, 10), UserName: ownerName, ProjectView: &webprojects.ProjectView{ID: strconv.FormatInt(project.ID, 10), Name: project.Name, Description: project.Description, Slug: project.Slug, AccessMode: project.AccessMode.String(), Status: project.Status.String(), CurrentReleaseID: current, Revision: project.Revision, MemberUserIDs: []string{}, URL: "/p/" + project.Slug + "/", ModerationStatus: project.ModerationStatus, ModerationReason: project.ModerationReason, PurgeAfter: project.PurgeAfter, CreateTime: project.CreateTime, UpdateTime: project.UpdateTime}}
}

func moderationSummary(project *model.WebProject) string {
	value := map[string]interface{}{"status": project.Status.String(), "moderation_status": project.ModerationStatus, "revision": project.Revision, "purge_after": project.PurgeAfter}
	raw, _ := json.Marshal(value)
	return string(raw)
}
