package webprojects

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/webprojects"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const maxReadyReleaseScan = 1000

type Application struct {
	repository *repository.Repository
	uploads    uploadLimiter
	diskFree   func(string) (uint64, error)
}

type uploadLimiter struct {
	sync.Mutex
	byUser        map[int64]int
	global        int
	reservedBytes uint64
}

var Default = New(repository.New())

func New(repository *repository.Repository) *Application {
	return &Application{repository: repository, diskFree: webDiskFreeBytes, uploads: uploadLimiter{byUser: make(map[int64]int)}}
}

// CreateProject validates the complete aggregate before persistence, then writes
// the project and members in one transaction. A repeated request returns only an
// exactly matching aggregate; a reused key with different input is a conflict.
func (application *Application) CreateProject(ownerUserID int64, input service.CreateProjectInput, principals ...*utils.Principal) (*service.ProjectView, error) {
	input.Name = strings.TrimSpace(input.Name)
	memberIDs, err := service.ValidateProjectInput(input.Name, input.Description, input.Slug, input.AccessMode, input.MemberUserIDs, true)
	if err != nil {
		return nil, err
	}
	if input.AccessMode == "" {
		input.AccessMode = service.AccessModeOwner.String()
	}
	if len(input.ClientRequestID) > 256 {
		return nil, fmt.Errorf("%w: client_request_id is too long", service.ErrInvalid)
	}
	if input.ClientRequestID != "" {
		existing, queryErr := application.repository.FindByRequestID(ownerUserID, input.ClientRequestID)
		if queryErr != nil {
			return nil, fmt.Errorf("%w: query project request key", service.ErrDependency)
		}
		if existing != nil {
			view := projectView(existing)
			if existing.Project.Name != input.Name || existing.Project.Description != input.Description || existing.Project.Slug != input.Slug || existing.Project.AccessMode.String() != input.AccessMode || !service.SameMemberIDs(view.MemberUserIDs, input.MemberUserIDs) {
				return nil, service.ErrConflict
			}
			return view, nil
		}
	}
	projectID, err := newID()
	if err != nil {
		return nil, err
	}
	accessMode, _ := model.ParseWebProjectAccess(input.AccessMode)
	now := time.Now()
	project := &model.WebProject{ID: projectID, OwnerUserID: ownerUserID, Name: input.Name, Description: input.Description, Slug: input.Slug, AccessMode: accessMode, Status: service.ProjectStatusDraft, Revision: 1, CreateTime: now, UpdateTime: now}
	if input.ClientRequestID != "" {
		project.ClientRequestID = &input.ClientRequestID
	}
	if err := application.repository.Create(project, memberIDs, principals...); err != nil {
		if isDuplicateKey(err) {
			return nil, fmt.Errorf("%w: create project", service.ErrConflict)
		}
		return nil, projectPersistenceError(err, "create project")
	}
	return projectView(&repository.ProjectAggregate{Project: project, MemberIDs: memberIDs}), nil
}

func (application *Application) GetOwnedProject(ownerUserID, projectID int64) (*service.ProjectView, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil {
		return nil, service.ErrNotFound
	}
	return projectView(aggregate), nil
}

// ListOwnedProjects keeps both SQL reads bounded by limit+1. Repository
// association avoids a JOIN and this layer owns cursor semantics and HTTP views.
func (application *Application) ListOwnedProjects(ownerUserID, cursor int64, limit int, status string) (*service.ProjectPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 || cursor < 0 || !service.ValidProjectStatusFilter(status) {
		return nil, service.ErrInvalid
	}
	statusValue := model.WebProjectStatus(0)
	if status != "" {
		statusValue, _ = model.ParseWebProjectStatus(status)
	}
	aggregates, err := application.repository.ListOwned(ownerUserID, cursor, limit+1, statusValue)
	if err != nil {
		return nil, fmt.Errorf("%w: list projects", service.ErrDependency)
	}
	page := &service.ProjectPage{Items: make([]*service.ProjectView, 0, minInt(limit, len(aggregates)))}
	if len(aggregates) > limit {
		page.HasMore = true
		aggregates = aggregates[:limit]
	}
	for _, aggregate := range aggregates {
		page.Items = append(page.Items, projectView(aggregate))
	}
	if page.HasMore {
		page.NextCursor = strconv.FormatInt(aggregates[len(aggregates)-1].Project.ID, 10)
	}
	return page, nil
}

// UpdateProject resolves omitted member input before validating the resulting
// aggregate. The project revision and member replacement commit atomically.
func (application *Application) UpdateProject(ownerUserID, projectID, revision int64, input service.UpdateProjectInput, principals ...*utils.Principal) (*service.ProjectView, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil || aggregate.Project.Status == service.ProjectStatusDeleted {
		return nil, service.ErrNotFound
	}
	project := aggregate.Project
	name, description, slug, mode := project.Name, project.Description, project.Slug, project.AccessMode.String()
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		description = *input.Description
	}
	if input.Slug != nil {
		slug = *input.Slug
	}
	if input.AccessMode != nil {
		mode = *input.AccessMode
	}
	members := make([]string, 0)
	if mode == service.AccessModeMembers.String() && input.MemberUserIDs == nil {
		members = int64sToStrings(aggregate.MemberIDs)
	}
	if input.MemberUserIDs != nil {
		members = *input.MemberUserIDs
	}
	if input.AccessMode != nil && project.AccessMode != service.AccessModeMembers && mode == service.AccessModeMembers.String() && input.MemberUserIDs == nil {
		return nil, fmt.Errorf("%w: member_user_ids is required when selecting members", service.ErrInvalid)
	}
	memberIDs, err := service.ValidateProjectInput(name, description, slug, mode, members, false)
	if err != nil {
		return nil, err
	}
	if mode != service.AccessModeMembers.String() {
		memberIDs = nil
	}
	accessMode, _ := model.ParseWebProjectAccess(mode)
	fields := map[string]interface{}{"name": name, "description": description, "slug": slug, "access_mode": accessMode, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()}
	updated, err := application.repository.Update(ownerUserID, projectID, revision, fields, memberIDs, principals...)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, service.ErrConflict
		}
		return nil, projectPersistenceError(err, "update project")
	}
	if !updated {
		return nil, service.ErrConflict
	}
	return application.GetOwnedProject(ownerUserID, projectID)
}

// ChangeProjectStatus 计算所属项目的状态变更字段，按预期修订号持久化；未命中更新时返回并发冲突。
func (application *Application) ChangeProjectStatus(ownerUserID, projectID, revision int64, action string, retentionDays int, principals ...*utils.Principal) (*service.ProjectView, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil {
		return nil, service.ErrNotFound
	}
	fields, err := service.ProjectStatusFields(aggregate.Project, action, retentionDays, time.Now())
	if err != nil {
		return nil, err
	}
	updated, err := application.repository.UpdateFields(ownerUserID, projectID, revision, fields, principals...)
	if err != nil {
		return nil, projectPersistenceError(err, "update project status")
	}
	if !updated {
		return nil, service.ErrConflict
	}
	return application.GetOwnedProject(ownerUserID, projectID)
}

func (application *Application) CheckUploadOwner(ownerUserID, projectID int64) error {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil || aggregate.Project.Status == service.ProjectStatusDeleted {
		return service.ErrNotFound
	}
	if aggregate.Project.ModerationStatus != "" && aggregate.Project.ModerationStatus != "normal" {
		return service.ErrForbidden
	}
	return nil
}

// AcquireUpload 在进程内锁下限制个人与全站上传并发；数据库身份模式且指定存储目录时额外预留磁盘空间，返回可重复调用的释放函数。
func (application *Application) AcquireUpload(ownerUserID int64, conf *config.WebProjectsConfig) (func(), error) {
	application.uploads.Lock()
	defer application.uploads.Unlock()
	if conf == nil || !conf.Enabled || application.uploads.byUser[ownerUserID] >= conf.MaxConcurrentUploadsPerUser || application.uploads.global >= conf.MaxConcurrentExtracts {
		return nil, service.ErrRateLimited
	}
	var reservation uint64
	if accounts.DatabaseMode() && conf.StorageRoot != "" {
		if conf.MaxExpandedBytes <= 0 || conf.MaxUploadBytes < 0 || conf.MinFreeDiskBytes < 0 || application.diskFree == nil {
			return nil, service.ErrDependency
		}
		reservation = uint64(conf.MaxExpandedBytes)
		// HTTP staging and ZIP parsing can temporarily keep two compressed
		// copies alongside the expanded content, so reserve those as well.
		compressed := uint64(conf.MaxUploadBytes)
		if compressed > (math.MaxUint64-reservation)/2 {
			return nil, service.ErrDependency
		}
		reservation += 2 * compressed
		free, err := application.diskFree(conf.StorageRoot)
		if err != nil {
			return nil, service.ErrDependency
		}
		minimum := uint64(conf.MinFreeDiskBytes)
		if minimum > free || application.uploads.reservedBytes > free-minimum || reservation > free-minimum-application.uploads.reservedBytes {
			return nil, apperrors.WithMessage(service.ErrRateLimited, "网页存储磁盘可用空间不足，请等待上传或清理完成")
		}
	}
	application.uploads.byUser[ownerUserID]++
	application.uploads.global++
	application.uploads.reservedBytes += reservation
	released := false
	return func() {
		application.uploads.Lock()
		defer application.uploads.Unlock()
		if released {
			return
		}
		released = true
		application.uploads.byUser[ownerUserID]--
		application.uploads.global--
		application.uploads.reservedBytes -= reservation
	}, nil
}

// UploadRelease writes and validates the artifact before opening a short
// database transaction. Project locking protects quota/current-release state;
// failed persistence removes only the newly generated release directory.
func (application *Application) UploadRelease(conf *config.WebProjectsConfig, ownerUserID, projectID int64, fileName, entryFile, idempotencyKey string, src io.Reader, principals ...*utils.Principal) (*model.WebProjectRelease, error) {
	if len(idempotencyKey) > 256 {
		return nil, service.ErrInvalid
	}
	if err := application.CheckUploadOwner(ownerUserID, projectID); err != nil {
		return nil, err
	}
	releaseID, err := newID()
	if err != nil {
		return nil, err
	}
	artifact, err := service.StoreUpload(conf, strconv.FormatInt(ownerUserID, 10), strconv.FormatInt(projectID, 10), strconv.FormatInt(releaseID, 10), fileName, entryFile, src)
	if err != nil {
		return nil, service.ClassifyStorageError(err)
	}
	now := time.Now()
	release := &model.WebProjectRelease{ID: releaseID, ProjectID: projectID, UploadedBy: ownerUserID, StorageKey: artifact.StorageKey, Status: service.ReleaseStatusReady, EntryFile: artifact.EntryFile, SHA256: artifact.SHA256, FileCount: artifact.FileCount, TotalBytes: artifact.TotalBytes, CreateTime: now, UpdateTime: now}
	if idempotencyKey != "" {
		release.IdempotencyKey = &idempotencyKey
	}
	releaseDir, missing, err := service.ReleaseDirectory(conf, release)
	if err != nil || missing {
		return nil, service.ErrDependency
	}
	var existing *model.WebProjectRelease
	var retired []*model.WebProjectRelease
	// 锁定所属项目并核对请求幂等性和用户配额，将待淘汰版本标记与新版本记录原子提交。
	err = application.repository.Transaction(func(tx *repository.Transaction) error {
		project, lockErr := tx.LockOwnedProject(ownerUserID, projectID, principals...)
		if lockErr != nil {
			return projectPersistenceError(lockErr, "lock project")
		}
		if project == nil || project.Status == service.ProjectStatusDeleted {
			return service.ErrNotFound
		}
		if idempotencyKey != "" {
			existing, lockErr = tx.FindReleaseByIdempotencyKey(projectID, idempotencyKey)
			if lockErr != nil {
				return fmt.Errorf("%w: query release request key", service.ErrDependency)
			}
			if existing != nil {
				if existing.SHA256 != artifact.SHA256 || existing.EntryFile != artifact.EntryFile {
					return service.ErrConflict
				}
				return nil
			}
		}
		if err := tx.CheckUserUploadQuota(ownerUserID, artifact.TotalBytes); err != nil {
			return err
		}
		ready, listErr := tx.ListReadyReleases(projectID, maxReadyReleaseScan+1)
		if listErr != nil {
			return fmt.Errorf("%w: list release usage", service.ErrDependency)
		}
		if len(ready) > maxReadyReleaseScan {
			return fmt.Errorf("%w: too many releases to evaluate safely", service.ErrRateLimited)
		}
		retired, listErr = service.SelectReleasesForPruning(ready, project.CurrentReleaseID, conf.MaxProjectBytes, conf.MaxReleases, artifact.TotalBytes)
		if listErr != nil {
			return listErr
		}
		ids := releaseIDs(retired)
		rows, markErr := tx.MarkReleasesDeleting(projectID, ids)
		if markErr != nil {
			return fmt.Errorf("%w: mark releases deleting", service.ErrDependency)
		}
		if rows != int64(len(ids)) {
			return service.ErrConflict
		}
		for _, item := range retired {
			item.Status = model.WebProjectReleaseDeleting
		}
		return tx.CreateRelease(release)
	})
	if err != nil {
		_ = os.RemoveAll(releaseDir)
		if isDuplicateKey(err) {
			return nil, service.ErrConflict
		}
		return nil, err
	}
	if existing != nil {
		_ = os.RemoveAll(releaseDir)
		return existing, nil
	}
	if err := service.RemoveRetiredReleases(conf, retired); err != nil {
		logrus.WithError(err).Warnf("web project release cleanup deferred, project_id=%d", projectID)
	}
	return release, nil
}

// ListReleases 校验项目归属后按版本 ID 游标分页，使用额外一条记录判断是否还有下一页。
func (application *Application) ListReleases(ownerUserID, projectID, cursor int64, limit int) (*service.ReleasePage, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil {
		return nil, service.ErrNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 || cursor < 0 {
		return nil, service.ErrInvalid
	}
	releases, err := application.repository.ListReleases(projectID, cursor, limit+1)
	if err != nil {
		return nil, fmt.Errorf("%w: list releases", service.ErrDependency)
	}
	page := &service.ReleasePage{Items: releases}
	if page.Items == nil {
		page.Items = make([]*model.WebProjectRelease, 0)
	}
	if len(releases) > limit {
		page.HasMore = true
		page.Items = releases[:limit]
		page.NextCursor = strconv.FormatInt(page.Items[len(page.Items)-1].ID, 10)
	}
	return page, nil
}

// PublishRelease locks project before release, matching upload lock order. The
// filesystem entry check runs while locked so a missing artifact cannot become
// current and optimistic revision still resolves concurrent status changes.
func (application *Application) PublishRelease(conf *config.WebProjectsConfig, ownerUserID, projectID, releaseID, revision int64, principals ...*utils.Principal) (*service.ProjectView, error) {
	// 依次锁定项目和版本，验证修订号、版本归属及入口文件后更新当前发布版本。
	err := application.repository.Transaction(func(tx *repository.Transaction) error {
		project, lockErr := tx.LockOwnedProject(ownerUserID, projectID, principals...)
		if lockErr != nil {
			return projectPersistenceError(lockErr, "lock project")
		}
		if project == nil || project.Status == service.ProjectStatusDeleted {
			return service.ErrNotFound
		}
		if project.Revision != revision {
			return service.ErrConflict
		}
		release, lockErr := tx.LockRelease(projectID, releaseID)
		if lockErr != nil {
			return fmt.Errorf("%w: lock release", service.ErrDependency)
		}
		if release == nil || release.Status != service.ReleaseStatusReady {
			return service.ErrNotFound
		}
		if release.UploadedBy != project.OwnerUserID || release.UploadedBy <= 0 {
			return service.ErrDependency
		}
		contentRoot, pathErr := service.ReleaseContentRoot(conf, release)
		if pathErr != nil {
			return service.ErrDependency
		}
		entryPath, pathErr := service.ResolveContentPath(contentRoot, release.EntryFile)
		if pathErr != nil {
			return service.ErrDependency
		}
		entryInfo, pathErr := os.Stat(entryPath)
		if pathErr != nil || !entryInfo.Mode().IsRegular() {
			return service.ErrDependency
		}
		updated, updateErr := tx.UpdateProject(ownerUserID, projectID, revision, map[string]interface{}{"status": service.ProjectStatusEnabled, "current_release_id": releaseID, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
		if updateErr != nil {
			return fmt.Errorf("%w: publish release", service.ErrDependency)
		}
		if !updated {
			return service.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return application.GetOwnedProject(ownerUserID, projectID)
}

// GetReleaseForDownload 校验项目归属、版本就绪状态及上传者与项目所有者一致后，返回可下载的版本记录。
func (application *Application) GetReleaseForDownload(ownerUserID, projectID, releaseID int64) (*model.WebProjectRelease, error) {
	aggregate, err := application.repository.FindOwned(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: query project", service.ErrDependency)
	}
	if aggregate == nil {
		return nil, service.ErrNotFound
	}
	release, err := application.repository.FindRelease(projectID, releaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: query release", service.ErrDependency)
	}
	if release == nil || release.Status != service.ReleaseStatusReady {
		return nil, service.ErrNotFound
	}
	if release.UploadedBy != aggregate.Project.OwnerUserID || release.UploadedBy <= 0 {
		return nil, service.ErrDependency
	}
	return release, nil
}

// GetPublishedProject 检查模块开关、项目发布及审核状态；数据库身份模式额外校验所有者状态，再返回归属一致的当前版本。
func (application *Application) GetPublishedProject(slug string) (*model.WebProject, *model.WebProjectRelease, error) {
	enabled, gateErr := accounts.ModuleEnabled(context.Background(), "web_projects")
	if gateErr != nil {
		return nil, nil, service.ErrDependency
	}
	if !enabled {
		return nil, nil, service.ErrForbidden
	}
	project, release, err := application.repository.FindPublished(slug)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: query published project", service.ErrDependency)
	}
	if project == nil || project.Status != service.ProjectStatusEnabled || project.CurrentReleaseID == nil {
		return nil, nil, service.ErrNotFound
	}
	if project.ModerationStatus != "" && project.ModerationStatus != "normal" {
		return nil, nil, service.ErrNotFound
	}
	if accounts.DatabaseMode() {
		owner, ownerErr := accounts.GetByID(context.Background(), project.OwnerUserID)
		if ownerErr != nil {
			return nil, nil, service.ErrDependency
		}
		if owner == nil || owner.Status != model.AccountActive {
			return nil, nil, service.ErrNotFound
		}
	}
	if release == nil || release.Status != service.ReleaseStatusReady {
		return nil, nil, service.ErrDependency
	}
	if release.ProjectID != project.ID || release.UploadedBy != project.OwnerUserID || release.UploadedBy <= 0 {
		return nil, nil, service.ErrDependency
	}
	return project, release, nil
}

func (application *Application) IsMember(projectID, userID int64) (bool, error) {
	return application.repository.IsMember(projectID, userID)
}

func (application *Application) EligibleUsers() ([]service.EligibleUser, error) {
	identities, err := passport.ListUsersWithError(context.Background())
	if err != nil {
		return nil, service.ErrDependency
	}
	users := make([]service.EligibleUser, 0, len(identities))
	for _, identity := range identities {
		if identity != nil && identity.ID > 0 {
			users = append(users, service.EligibleUser{ID: strconv.FormatInt(identity.ID, 10), UserName: identity.UserName})
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users, nil
}

func projectView(aggregate *repository.ProjectAggregate) *service.ProjectView {
	project := aggregate.Project
	view := &service.ProjectView{ModerationStatus: project.ModerationStatus, ModerationReason: project.ModerationReason, PurgeAfter: project.PurgeAfter, ID: strconv.FormatInt(project.ID, 10), Name: project.Name, Description: project.Description, Slug: project.Slug, AccessMode: project.AccessMode.String(), Status: project.Status.String(), Revision: project.Revision, MemberUserIDs: int64sToStrings(aggregate.MemberIDs), URL: "/p/" + project.Slug + "/", CreateTime: project.CreateTime, UpdateTime: project.UpdateTime}
	if project.CurrentReleaseID != nil {
		view.CurrentReleaseID = strconv.FormatInt(*project.CurrentReleaseID, 10)
	}
	return view
}

func int64sToStrings(values []int64) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strconv.FormatInt(value, 10))
	}
	return result
}

func releaseIDs(releases []*model.WebProjectRelease) []int64 {
	ids := make([]int64, 0, len(releases))
	for _, release := range releases {
		ids = append(ids, release.ID)
	}
	return ids
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func newID() (int64, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0, fmt.Errorf("generate id: %w", err)
	}
	id := int64(binary.BigEndian.Uint64(data[:]) & uint64(^uint64(0)>>1))
	if id == 0 {
		return newID()
	}
	return id, nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func projectPersistenceError(err error, operation string) error {
	var apiError *apperrors.APIError
	if errors.As(err, &apiError) {
		return err
	}
	return fmt.Errorf("%w: %s", service.ErrDependency, operation)
}
