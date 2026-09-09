package webprojects

import (
	"context"
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
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/passport"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	ProjectStatusDraft    = "draft"
	ProjectStatusEnabled  = "enabled"
	ProjectStatusDisabled = "disabled"
	ProjectStatusDeleted  = "deleted"
	ReleaseStatusReady    = "ready"
)

var (
	ErrInvalid       = errors.New("invalid web project request")
	ErrUnauthorized  = errors.New("authentication required")
	ErrForbidden     = errors.New("web project access forbidden")
	ErrNotFound      = errors.New("web project not found")
	ErrConflict      = errors.New("web project revision or idempotency conflict")
	ErrTooLarge      = errors.New("web project upload is too large")
	ErrUnsupported   = errors.New("unsupported web project upload")
	ErrUnprocessable = errors.New("invalid web project artifact")
	ErrDependency    = errors.New("web project dependency unavailable")
	ErrRateLimited   = errors.New("web project upload limit reached")
)

type ProjectView struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Slug             string    `json:"slug"`
	AccessMode       string    `json:"access_mode"`
	Status           string    `json:"status"`
	CurrentReleaseID string    `json:"current_release_id"`
	Revision         int64     `json:"revision"`
	MemberUserIDs    []string  `json:"member_user_ids"`
	URL              string    `json:"url"`
	CreateTime       time.Time `json:"create_time"`
	UpdateTime       time.Time `json:"update_time"`
}

type CreateProjectInput struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Slug            string   `json:"slug"`
	AccessMode      string   `json:"access_mode"`
	MemberUserIDs   []string `json:"member_user_ids"`
	ClientRequestID string   `json:"client_request_id"`
}

type UpdateProjectInput struct {
	Name          *string   `json:"name"`
	Description   *string   `json:"description"`
	Slug          *string   `json:"slug"`
	AccessMode    *string   `json:"access_mode"`
	MemberUserIDs *[]string `json:"member_user_ids"`
}

type ProjectPage struct {
	Items      []*ProjectView `json:"items"`
	NextCursor string         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

type ReleasePage struct {
	Items      []*model.WebProjectRelease `json:"items"`
	NextCursor string                     `json:"next_cursor"`
	HasMore    bool                       `json:"has_more"`
}

type EligibleUser struct {
	ID       string `json:"id"`
	UserName string `json:"user_name"`
}

var uploadLimiter = struct {
	sync.Mutex
	byUser map[int64]int
	global int
}{byUser: make(map[int64]int)}

func CreateProject(ownerUserID int64, input CreateProjectInput) (*ProjectView, error) {
	input.Name = strings.TrimSpace(input.Name)
	memberIDs, err := validateProjectInput(input.Name, input.Description, input.Slug, input.AccessMode, input.MemberUserIDs, true)
	if err != nil {
		return nil, err
	}
	if input.AccessMode == "" {
		input.AccessMode = AccessModeOwner
	}
	if len(input.ClientRequestID) > 128 {
		return nil, fmt.Errorf("%w: client_request_id is too long", ErrInvalid)
	}
	if input.ClientRequestID != "" {
		existing, queryErr := dal.QueryWebProjectByRequestID(ownerUserID, input.ClientRequestID)
		if queryErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrDependency, queryErr)
		}
		if existing != nil {
			view, viewErr := buildProjectView(existing)
			if viewErr != nil {
				return nil, viewErr
			}
			if existing.Name != input.Name || existing.Description != input.Description || existing.Slug != input.Slug || existing.AccessMode != input.AccessMode || !sameMemberIDs(view.MemberUserIDs, input.MemberUserIDs) {
				return nil, ErrConflict
			}
			return view, nil
		}
	}
	projectID, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	project := &model.WebProject{ID: projectID, OwnerUserID: ownerUserID, Name: input.Name, Description: input.Description, Slug: input.Slug, AccessMode: input.AccessMode, Status: ProjectStatusDraft, Revision: 1, CreateTime: now, UpdateTime: now}
	if input.ClientRequestID != "" {
		project.ClientRequestID = &input.ClientRequestID
	}
	err = db.MasterDB().Transaction(func(tx *gorm.DB) error {
		if createErr := dal.CreateWebProject(tx, project); createErr != nil {
			return createErr
		}
		return dal.ReplaceWebProjectMembers(tx, project.ID, ownerUserID, memberIDs)
	})
	if err != nil {
		if isDuplicateKey(err) {
			return nil, fmt.Errorf("%w: create project", ErrConflict)
		}
		return nil, fmt.Errorf("%w: create project: %v", ErrDependency, err)
	}
	return buildProjectView(project)
}

func GetOwnedProject(ownerUserID, projectID int64) (*ProjectView, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil {
		return nil, ErrNotFound
	}
	return buildProjectView(project)
}

func ListOwnedProjects(ownerUserID, cursor int64, limit int, status string) (*ProjectPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 || cursor < 0 || !validProjectStatusFilter(status) {
		return nil, ErrInvalid
	}
	projects, err := dal.ListOwnedWebProjects(ownerUserID, cursor, limit+1, status)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	page := &ProjectPage{Items: make([]*ProjectView, 0, minInt(limit, len(projects)))}
	if len(projects) > limit {
		page.HasMore = true
		projects = projects[:limit]
	}
	projectIDs := make([]int64, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID)
	}
	members, err := dal.ListWebProjectMembers(projectIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	membersByProject := make(map[int64][]int64, len(projects))
	for _, member := range members {
		membersByProject[member.ProjectID] = append(membersByProject[member.ProjectID], member.UserID)
	}
	for _, project := range projects {
		page.Items = append(page.Items, projectView(project, membersByProject[project.ID]))
	}
	if page.HasMore && len(projects) > 0 {
		page.NextCursor = strconv.FormatInt(projects[len(projects)-1].ID, 10)
	}
	return page, nil
}

func UpdateProject(ownerUserID, projectID, revision int64, input UpdateProjectInput) (*ProjectView, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil || project.Status == ProjectStatusDeleted {
		return nil, ErrNotFound
	}
	name, description, slug, mode := project.Name, project.Description, project.Slug, project.AccessMode
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
	memberStrings := make([]string, 0)
	if mode == AccessModeMembers && input.MemberUserIDs == nil {
		existingMembers, memberErr := dal.ListWebProjectMemberIDs(projectID)
		if memberErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrDependency, memberErr)
		}
		memberStrings = int64sToStrings(existingMembers)
	}
	if input.MemberUserIDs != nil {
		memberStrings = *input.MemberUserIDs
	}
	if input.AccessMode != nil && project.AccessMode != AccessModeMembers && mode == AccessModeMembers && input.MemberUserIDs == nil {
		return nil, fmt.Errorf("%w: member_user_ids is required when selecting members", ErrInvalid)
	}
	memberIDs, err := validateProjectInput(name, description, slug, mode, memberStrings, false)
	if err != nil {
		return nil, err
	}
	if mode != AccessModeMembers {
		memberIDs = nil
	}
	fields := map[string]interface{}{"name": name, "description": description, "slug": slug, "access_mode": mode, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()}
	err = db.MasterDB().Transaction(func(tx *gorm.DB) error {
		updated, updateErr := dal.UpdateProjectFields(tx, ownerUserID, projectID, revision, fields)
		if updateErr != nil {
			return updateErr
		}
		if !updated {
			return ErrConflict
		}
		return dal.ReplaceWebProjectMembers(tx, projectID, ownerUserID, memberIDs)
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return nil, ErrConflict
		}
		if isDuplicateKey(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	return GetOwnedProject(ownerUserID, projectID)
}

func ChangeProjectStatus(ownerUserID, projectID, revision int64, action string, retentionDays int) (*ProjectView, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil {
		return nil, ErrNotFound
	}
	fields := map[string]interface{}{"revision": gorm.Expr("revision + 1"), "update_time": time.Now()}
	switch action {
	case "disable":
		if project.Status == ProjectStatusDeleted {
			return nil, ErrNotFound
		}
		fields["status"] = ProjectStatusDisabled
	case "delete":
		if project.Status == ProjectStatusDeleted {
			return nil, ErrNotFound
		}
		fields["status"] = ProjectStatusDeleted
		fields["deleted_at"] = time.Now()
	case "restore":
		if project.Status != ProjectStatusDeleted || project.DeletedAt == nil || time.Since(*project.DeletedAt) > time.Duration(retentionDays)*24*time.Hour {
			return nil, ErrNotFound
		}
		fields["status"] = ProjectStatusDisabled
		fields["deleted_at"] = nil
	default:
		return nil, ErrInvalid
	}
	updated, err := dal.UpdateProjectFields(db.MasterDB(), ownerUserID, projectID, revision, fields)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if !updated {
		return nil, ErrConflict
	}
	return GetOwnedProject(ownerUserID, projectID)
}

func UploadRelease(conf *config.WebProjectsConfig, ownerUserID, projectID int64, fileName, entryFile, idempotencyKey string, src io.Reader) (*model.WebProjectRelease, error) {
	if len(idempotencyKey) > 128 {
		return nil, ErrInvalid
	}
	releaseID, err := newID()
	if err != nil {
		return nil, err
	}
	artifact, err := StoreUpload(conf, strconv.FormatInt(projectID, 10), strconv.FormatInt(releaseID, 10), fileName, entryFile, src)
	if err != nil {
		return nil, classifyStorageError(err)
	}
	cleanup := func() {
		_ = os.RemoveAll(filepath.Join(conf.StorageRoot, "projects", strconv.FormatInt(projectID, 10), "releases", strconv.FormatInt(releaseID, 10)))
	}
	now := time.Now()
	release := &model.WebProjectRelease{ID: releaseID, ProjectID: projectID, UploadedBy: ownerUserID, StorageKey: artifact.StorageKey, Status: ReleaseStatusReady, EntryFile: artifact.EntryFile, SHA256: artifact.SHA256, FileCount: artifact.FileCount, TotalBytes: artifact.TotalBytes, CreateTime: now, UpdateTime: now}
	if idempotencyKey != "" {
		release.IdempotencyKey = &idempotencyKey
	}
	var existing *model.WebProjectRelease
	var retired []*model.WebProjectRelease
	err = db.MasterDB().Transaction(func(tx *gorm.DB) error {
		project, lockErr := dal.LockOwnedWebProject(tx, ownerUserID, projectID)
		if lockErr != nil {
			return fmt.Errorf("%w: %v", ErrDependency, lockErr)
		}
		if project == nil || project.Status == ProjectStatusDeleted {
			return ErrNotFound
		}
		if idempotencyKey != "" {
			existing, lockErr = dal.QueryWebProjectReleaseByIdempotencyKey(projectID, idempotencyKey, tx)
			if lockErr != nil {
				return fmt.Errorf("%w: %v", ErrDependency, lockErr)
			}
			if existing != nil {
				if existing.SHA256 != artifact.SHA256 || existing.EntryFile != artifact.EntryFile {
					return ErrConflict
				}
				return nil
			}
		}
		retired, lockErr = PruneProjectReleases(tx, project, conf, artifact.TotalBytes)
		if lockErr != nil {
			return lockErr
		}
		return dal.CreateWebProjectRelease(tx, release)
	})
	if err != nil {
		cleanup()
		if isDuplicateKey(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if existing != nil {
		cleanup()
		return existing, nil
	}
	if err := RemoveRetiredReleases(conf, retired); err != nil {
		logrus.WithError(err).Warnf("web project release cleanup deferred, project_id=%d", projectID)
	}
	return release, nil
}

func CheckUploadOwner(ownerUserID, projectID int64) error {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil || project.Status == ProjectStatusDeleted {
		return ErrNotFound
	}
	return nil
}

func AcquireUpload(ownerUserID int64, conf *config.WebProjectsConfig) (func(), error) {
	if conf == nil || !conf.Enabled || !acquireUpload(ownerUserID, conf) {
		return nil, ErrRateLimited
	}
	return func() { releaseUpload(ownerUserID) }, nil
}

func ListReleases(ownerUserID, projectID, cursor int64, limit int) (*ReleasePage, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil {
		return nil, ErrNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 || cursor < 0 {
		return nil, ErrInvalid
	}
	releases, err := dal.ListWebProjectReleases(projectID, cursor, limit+1)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	page := &ReleasePage{Items: releases}
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

func PublishRelease(conf *config.WebProjectsConfig, ownerUserID, projectID, releaseID, revision int64) (*ProjectView, error) {
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		project, lockErr := dal.LockOwnedWebProject(tx, ownerUserID, projectID)
		if lockErr != nil {
			return fmt.Errorf("%w: %v", ErrDependency, lockErr)
		}
		if project == nil || project.Status == ProjectStatusDeleted {
			return ErrNotFound
		}
		if project.Revision != revision {
			return ErrConflict
		}
		release, lockErr := dal.LockWebProjectRelease(tx, projectID, releaseID)
		if lockErr != nil {
			return fmt.Errorf("%w: %v", ErrDependency, lockErr)
		}
		if release == nil || release.Status != ReleaseStatusReady {
			return ErrNotFound
		}
		contentRoot, pathErr := ReleaseContentRoot(conf, release)
		if pathErr != nil {
			return ErrDependency
		}
		entryPath, pathErr := ResolveContentPath(contentRoot, release.EntryFile)
		if pathErr != nil {
			return ErrDependency
		}
		entryInfo, pathErr := os.Stat(entryPath)
		if pathErr != nil || !entryInfo.Mode().IsRegular() {
			return ErrDependency
		}
		updated, updateErr := dal.UpdateProjectFields(tx, ownerUserID, projectID, revision, map[string]interface{}{"status": ProjectStatusEnabled, "current_release_id": releaseID, "revision": gorm.Expr("revision + 1"), "update_time": time.Now()})
		if updateErr != nil {
			return updateErr
		}
		if !updated {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetOwnedProject(ownerUserID, projectID)
}

func GetReleaseForDownload(ownerUserID, projectID, releaseID int64) (*model.WebProjectRelease, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil {
		return nil, ErrNotFound
	}
	release, err := dal.QueryWebProjectRelease(projectID, releaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if release == nil || release.Status != ReleaseStatusReady {
		return nil, ErrNotFound
	}
	return release, nil
}

func EligibleUsers() []EligibleUser {
	identities := passport.ListUsers()
	users := make([]EligibleUser, 0, len(identities))
	for _, identity := range identities {
		if identity != nil && identity.ID > 0 {
			users = append(users, EligibleUser{ID: strconv.FormatInt(identity.ID, 10), UserName: identity.UserName})
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users
}

func GetPublishedProject(slug string) (*model.WebProject, *model.WebProjectRelease, error) {
	project, err := dal.QueryWebProjectBySlug(slug)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if project == nil || project.Status != ProjectStatusEnabled || project.CurrentReleaseID == nil {
		return nil, nil, ErrNotFound
	}
	release, err := dal.QueryWebProjectRelease(project.ID, *project.CurrentReleaseID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if release == nil || release.Status != ReleaseStatusReady {
		return nil, nil, ErrDependency
	}
	return project, release, nil
}

func IsMember(projectID, userID int64) (bool, error) {
	return dal.IsWebProjectMember(projectID, userID)
}

func buildProjectView(project *model.WebProject) (*ProjectView, error) {
	members, err := dal.ListWebProjectMemberIDs(project.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	return projectView(project, members), nil
}

func projectView(project *model.WebProject, members []int64) *ProjectView {
	view := &ProjectView{ID: strconv.FormatInt(project.ID, 10), Name: project.Name, Description: project.Description, Slug: project.Slug, AccessMode: project.AccessMode, Status: project.Status, Revision: project.Revision, MemberUserIDs: int64sToStrings(members), URL: "/p/" + project.Slug + "/", CreateTime: project.CreateTime, UpdateTime: project.UpdateTime}
	if project.CurrentReleaseID != nil {
		view.CurrentReleaseID = strconv.FormatInt(*project.CurrentReleaseID, 10)
	}
	return view
}

func validateProjectInput(name, description, slug, mode string, memberStrings []string, creating bool) ([]int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 || len(description) > 4000 || !validProjectSlug(slug) {
		return nil, ErrInvalid
	}
	if mode == "" && creating {
		mode = AccessModeOwner
	}
	if mode != AccessModeOwner && mode != AccessModeMembers && mode != AccessModeAuthenticated && mode != AccessModePublic {
		return nil, ErrInvalid
	}
	memberIDs, err := parseMemberIDs(memberStrings)
	if err != nil {
		return nil, err
	}
	if mode != AccessModeMembers && len(memberIDs) > 0 {
		return nil, fmt.Errorf("%w: members require members access mode", ErrInvalid)
	}
	for _, userID := range memberIDs {
		identity, lookupErr := passport.GetMockData().GetByID(userID)
		if lookupErr != nil || identity == nil {
			return nil, fmt.Errorf("%w: unknown member", ErrInvalid)
		}
	}
	return memberIDs, nil
}

func parseMemberIDs(values []string) ([]int64, error) {
	seen := make(map[int64]bool, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, ErrInvalid
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func validProjectSlug(slug string) bool {
	if len(slug) < 3 || len(slug) > 48 || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func validProjectStatusFilter(status string) bool {
	return status == "" || status == ProjectStatusDraft || status == ProjectStatusEnabled || status == ProjectStatusDisabled || status == ProjectStatusDeleted
}
func int64sToStrings(values []int64) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strconv.FormatInt(value, 10))
	}
	return result
}
func sameMemberIDs(left, right []string) bool {
	l, e1 := parseMemberIDs(left)
	r, e2 := parseMemberIDs(right)
	if e1 != nil || e2 != nil || len(l) != len(r) {
		return false
	}
	for i := range l {
		if l[i] != r[i] {
			return false
		}
	}
	return true
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func acquireUpload(userID int64, conf *config.WebProjectsConfig) bool {
	uploadLimiter.Lock()
	defer uploadLimiter.Unlock()
	if uploadLimiter.byUser[userID] >= conf.MaxConcurrentUploadsPerUser || uploadLimiter.global >= conf.MaxConcurrentExtracts {
		return false
	}
	uploadLimiter.byUser[userID]++
	uploadLimiter.global++
	return true
}
func releaseUpload(userID int64) {
	uploadLimiter.Lock()
	defer uploadLimiter.Unlock()
	uploadLimiter.byUser[userID]--
	uploadLimiter.global--
}

func classifyStorageError(err error) error {
	if errors.Is(err, ErrDependency) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrUnprocessable) {
		return err
	}
	return fmt.Errorf("%w: unclassified storage failure: %w", ErrDependency, err)
}

func ReleaseContentRoot(conf *config.WebProjectsConfig, release *model.WebProjectRelease) (string, error) {
	expectedKey := filepath.ToSlash(filepath.Join("projects", strconv.FormatInt(release.ProjectID, 10), "releases", strconv.FormatInt(release.ID, 10), "content"))
	if release.StorageKey != expectedKey {
		return "", ErrDependency
	}
	root := filepath.Clean(conf.StorageRoot)
	content := filepath.Clean(filepath.Join(root, filepath.FromSlash(release.StorageKey)))
	if content == root || !strings.HasPrefix(content, root+string(os.PathSeparator)) {
		return "", ErrDependency
	}
	return content, nil
}

func ResolveContentPath(contentRoot, requested string) (string, error) {
	clean, err := validateRelativePath(requested, 64)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(contentRoot)
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(clean)))
	if target == root || !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", ErrInvalid
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if resolvedTarget == resolvedRoot || !strings.HasPrefix(resolvedTarget, resolvedRoot+string(os.PathSeparator)) {
		return "", ErrInvalid
	}
	return resolvedTarget, nil
}

func ParsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}

func CheckContentUser(ctx context.Context, token string) (*model.UserIdentity, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	user, err := passport.CheckToken(ctx, token)
	if err != nil {
		if IsDependencyError(err) {
			return nil, ErrDependency
		}
		return nil, ErrUnauthorized
	}
	if user == nil {
		return nil, ErrUnauthorized
	}
	return user, nil
}

func IsDependencyError(err error) bool {
	var typed *myErrors.Error
	return errors.As(err, &typed) && typed.Code == myErrors.ErrorCodeDbError
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
