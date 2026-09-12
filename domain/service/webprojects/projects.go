package webprojects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/passport"
	myErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

const (
	ProjectStatusDraft    = model.WebProjectStatusDraft
	ProjectStatusEnabled  = model.WebProjectStatusEnabled
	ProjectStatusDisabled = model.WebProjectStatusDisabled
	ProjectStatusDeleted  = model.WebProjectStatusDeleted
	ReleaseStatusReady    = model.WebProjectReleaseReady
)

var (
	ErrInvalid       = myErrors.ErrInvalid
	ErrUnauthorized  = myErrors.ErrUnauthorized
	ErrForbidden     = myErrors.ErrForbidden
	ErrNotFound      = myErrors.ErrNotFound
	ErrConflict      = myErrors.ErrConflict
	ErrTooLarge      = myErrors.ErrTooLarge
	ErrUnsupported   = myErrors.ErrUnsupported
	ErrUnprocessable = myErrors.ErrUnprocessable
	ErrDependency    = myErrors.ErrDependency
	ErrRateLimited   = myErrors.ErrRateLimited
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

// ValidateProjectInput checks the complete post-update value set before a
// transaction or filesystem side effect starts.
func ValidateProjectInput(name, description, slug, mode string, memberStrings []string, creating bool) ([]int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 256 || len(description) > 4000 || !ValidProjectSlug(slug) {
		return nil, ErrInvalid
	}
	if mode == "" && creating {
		mode = AccessModeOwner.String()
	}
	if _, ok := model.ParseWebProjectAccess(mode); !ok {
		return nil, ErrInvalid
	}
	memberIDs, err := parseMemberIDs(memberStrings)
	if err != nil {
		return nil, err
	}
	if mode != AccessModeMembers.String() && len(memberIDs) > 0 {
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

// ProjectStatusFields validates a status transition and returns the complete
// optimistic-lock update. Restore uses the supplied clock for deterministic
// retention checks and never revives an expired tombstone.
func ProjectStatusFields(project *model.WebProject, action string, retentionDays int, now time.Time) (map[string]interface{}, error) {
	if project == nil || retentionDays <= 0 {
		return nil, ErrInvalid
	}
	fields := map[string]interface{}{"revision": gorm.Expr("revision + 1"), "update_time": now}
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
		fields["deleted_at"] = now
	case "restore":
		if project.Status != ProjectStatusDeleted || project.DeletedAt == nil || now.Sub(*project.DeletedAt) > time.Duration(retentionDays)*24*time.Hour {
			return nil, ErrNotFound
		}
		fields["status"] = ProjectStatusDisabled
		fields["deleted_at"] = nil
	default:
		return nil, ErrInvalid
	}
	return fields, nil
}

func ValidProjectSlug(slug string) bool {
	if len(slug) < 3 || len(slug) > 256 || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func ValidProjectStatusFilter(status string) bool {
	if status == "" {
		return true
	}
	_, ok := model.ParseWebProjectStatus(status)
	return ok
}

func SameMemberIDs(left, right []string) bool {
	l, leftErr := parseMemberIDs(left)
	r, rightErr := parseMemberIDs(right)
	if leftErr != nil || rightErr != nil || len(l) != len(r) {
		return false
	}
	for index := range l {
		if l[index] != r[index] {
			return false
		}
	}
	return true
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

func ClassifyStorageError(err error) error {
	if errors.Is(err, ErrDependency) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrUnprocessable) {
		return err
	}
	return fmt.Errorf("%w: unclassified storage failure: %w", ErrDependency, err)
}

func ReleaseContentRoot(conf *config.WebProjectsConfig, release *model.WebProjectRelease) (string, error) {
	if conf == nil || release == nil {
		return "", ErrDependency
	}
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

// ResolveContentPath validates the requested relative path before and after
// symlink resolution. Missing child resources remain distinguishable from a
// missing or corrupt content root for the HTTP layer's 404/503 behavior.
func ResolveContentPath(contentRoot, requested string) (string, error) {
	clean, err := validateRelativePath(requested, 64)
	if err != nil {
		return "", fmt.Errorf("%w: invalid content path: %w", ErrInvalid, err)
	}
	root := filepath.Clean(contentRoot)
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(clean)))
	if target == root || !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", ErrInvalid
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve content root: %w", ErrDependency, err)
	}
	rootInfo, err := os.Stat(resolvedRoot)
	if err != nil || !rootInfo.IsDir() {
		return "", fmt.Errorf("%w: content root is not a directory", ErrDependency)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			if currentRoot, rootErr := os.Stat(resolvedRoot); rootErr == nil && currentRoot.IsDir() {
				return "", err
			}
			return "", fmt.Errorf("%w: content root changed", ErrDependency)
		}
		return "", fmt.Errorf("%w: resolve content file: %v", ErrDependency, err)
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
