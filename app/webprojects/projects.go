package webprojects

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/webprojects"
	"github.com/mcoder2014/home_server/domain/service/passport"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const maxReadyReleaseScan = 1000

type Application struct {
	repository *repository.Repository
	uploads    uploadLimiter
}

type uploadLimiter struct {
	sync.Mutex
	byUser map[int64]int
	global int
}

var Default = New(repository.New())

func New(repository *repository.Repository) *Application {
	return &Application{repository: repository, uploads: uploadLimiter{byUser: make(map[int64]int)}}
}

// CreateProject validates the complete aggregate before persistence, then writes
// the project and members in one transaction. A repeated request returns only an
// exactly matching aggregate; a reused key with different input is a conflict.
func (application *Application) CreateProject(ownerUserID int64, input service.CreateProjectInput) (*service.ProjectView, error) {
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
	if err := application.repository.Create(project, memberIDs); err != nil {
		if isDuplicateKey(err) {
			return nil, fmt.Errorf("%w: create project", service.ErrConflict)
		}
		return nil, fmt.Errorf("%w: create project", service.ErrDependency)
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
func (application *Application) UpdateProject(ownerUserID, projectID, revision int64, input service.UpdateProjectInput) (*service.ProjectView, error) {
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
	updated, err := application.repository.Update(ownerUserID, projectID, revision, fields, memberIDs)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, service.ErrConflict
		}
		return nil, fmt.Errorf("%w: update project", service.ErrDependency)
	}
	if !updated {
		return nil, service.ErrConflict
	}
	return application.GetOwnedProject(ownerUserID, projectID)
}

func (application *Application) ChangeProjectStatus(ownerUserID, projectID, revision int64, action string, retentionDays int) (*service.ProjectView, error) {
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
	updated, err := application.repository.UpdateFields(ownerUserID, projectID, revision, fields)
	if err != nil {
		return nil, fmt.Errorf("%w: update project status", service.ErrDependency)
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
	return nil
}

func (application *Application) AcquireUpload(ownerUserID int64, conf *config.WebProjectsConfig) (func(), error) {
	application.uploads.Lock()
	defer application.uploads.Unlock()
	if conf == nil || !conf.Enabled || application.uploads.byUser[ownerUserID] >= conf.MaxConcurrentUploadsPerUser || application.uploads.global >= conf.MaxConcurrentExtracts {
		return nil, service.ErrRateLimited
	}
	application.uploads.byUser[ownerUserID]++
	application.uploads.global++
	return func() {
		application.uploads.Lock()
		defer application.uploads.Unlock()
		application.uploads.byUser[ownerUserID]--
		application.uploads.global--
	}, nil
}

// UploadRelease writes and validates the artifact before opening a short
// database transaction. Project locking protects quota/current-release state;
// failed persistence removes only the newly generated release directory.
func (application *Application) UploadRelease(conf *config.WebProjectsConfig, ownerUserID, projectID int64, fileName, entryFile, idempotencyKey string, src io.Reader) (*model.WebProjectRelease, error) {
	if len(idempotencyKey) > 256 {
		return nil, service.ErrInvalid
	}
	releaseID, err := newID()
	if err != nil {
		return nil, err
	}
	artifact, err := service.StoreUpload(conf, strconv.FormatInt(projectID, 10), strconv.FormatInt(releaseID, 10), fileName, entryFile, src)
	if err != nil {
		return nil, service.ClassifyStorageError(err)
	}
	releaseDir := filepath.Join(conf.StorageRoot, "projects", strconv.FormatInt(projectID, 10), "releases", strconv.FormatInt(releaseID, 10))
	now := time.Now()
	release := &model.WebProjectRelease{ID: releaseID, ProjectID: projectID, UploadedBy: ownerUserID, StorageKey: artifact.StorageKey, Status: service.ReleaseStatusReady, EntryFile: artifact.EntryFile, SHA256: artifact.SHA256, FileCount: artifact.FileCount, TotalBytes: artifact.TotalBytes, CreateTime: now, UpdateTime: now}
	if idempotencyKey != "" {
		release.IdempotencyKey = &idempotencyKey
	}
	var existing *model.WebProjectRelease
	var retired []*model.WebProjectRelease
	err = application.repository.Transaction(func(tx *repository.Transaction) error {
		project, lockErr := tx.LockOwnedProject(ownerUserID, projectID)
		if lockErr != nil {
			return fmt.Errorf("%w: lock project", service.ErrDependency)
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
func (application *Application) PublishRelease(conf *config.WebProjectsConfig, ownerUserID, projectID, releaseID, revision int64) (*service.ProjectView, error) {
	err := application.repository.Transaction(func(tx *repository.Transaction) error {
		project, lockErr := tx.LockOwnedProject(ownerUserID, projectID)
		if lockErr != nil {
			return fmt.Errorf("%w: lock project", service.ErrDependency)
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
	return release, nil
}

func (application *Application) GetPublishedProject(slug string) (*model.WebProject, *model.WebProjectRelease, error) {
	project, release, err := application.repository.FindPublished(slug)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: query published project", service.ErrDependency)
	}
	if project == nil || project.Status != service.ProjectStatusEnabled || project.CurrentReleaseID == nil {
		return nil, nil, service.ErrNotFound
	}
	if release == nil || release.Status != service.ReleaseStatusReady {
		return nil, nil, service.ErrDependency
	}
	return project, release, nil
}

func (application *Application) IsMember(projectID, userID int64) (bool, error) {
	return application.repository.IsMember(projectID, userID)
}

func (application *Application) EligibleUsers() []service.EligibleUser {
	identities := passport.ListUsers()
	users := make([]service.EligibleUser, 0, len(identities))
	for _, identity := range identities {
		if identity != nil && identity.ID > 0 {
			users = append(users, service.EligibleUser{ID: strconv.FormatInt(identity.ID, 10), UserName: identity.UserName})
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users
}

func projectView(aggregate *repository.ProjectAggregate) *service.ProjectView {
	project := aggregate.Project
	view := &service.ProjectView{ID: strconv.FormatInt(project.ID, 10), Name: project.Name, Description: project.Description, Slug: project.Slug, AccessMode: project.AccessMode.String(), Status: project.Status.String(), Revision: project.Revision, MemberUserIDs: int64sToStrings(aggregate.MemberIDs), URL: "/p/" + project.Slug + "/", CreateTime: project.CreateTime, UpdateTime: project.UpdateTime}
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
