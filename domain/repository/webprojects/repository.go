package webprojects

import (
	"context"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
)

// ProjectAggregate is the repository boundary for a project and its bounded
// member set. The repository assembles rows in Go instead of issuing JOINs.
type ProjectAggregate struct {
	Project   *model.WebProject
	MemberIDs []int64
}

type Repository struct{}

func New() *Repository {
	return &Repository{}
}

func (repository *Repository) FindOwned(ownerUserID, projectID int64) (*ProjectAggregate, error) {
	project, err := dal.QueryOwnedWebProject(ownerUserID, projectID)
	if err != nil || project == nil {
		return nil, err
	}
	members, err := dal.ListWebProjectMemberIDs(projectID)
	if err != nil {
		return nil, err
	}
	return &ProjectAggregate{Project: project, MemberIDs: members}, nil
}

func (repository *Repository) FindByRequestID(ownerUserID int64, requestID string) (*ProjectAggregate, error) {
	project, err := dal.QueryWebProjectByRequestID(ownerUserID, requestID)
	if err != nil || project == nil {
		return nil, err
	}
	members, err := dal.ListWebProjectMemberIDs(project.ID)
	if err != nil {
		return nil, err
	}
	return &ProjectAggregate{Project: project, MemberIDs: members}, nil
}

// ListOwned performs two bounded single-table reads, then associates members in
// Go. The caller controls the page limit, preventing an unbounded IN clause.
func (repository *Repository) ListOwned(ownerUserID, cursor int64, limit int, status model.WebProjectStatus) ([]*ProjectAggregate, error) {
	projects, err := dal.ListOwnedWebProjects(ownerUserID, cursor, limit, status)
	if err != nil || len(projects) == 0 {
		return nil, err
	}
	projectIDs := make([]int64, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID)
	}
	members, err := dal.ListWebProjectMembers(projectIDs)
	if err != nil {
		return nil, err
	}
	membersByProject := make(map[int64][]int64, len(projects))
	for _, member := range members {
		membersByProject[member.ProjectID] = append(membersByProject[member.ProjectID], member.UserID)
	}
	aggregates := make([]*ProjectAggregate, 0, len(projects))
	for _, project := range projects {
		aggregates = append(aggregates, &ProjectAggregate{Project: project, MemberIDs: membersByProject[project.ID]})
	}
	return aggregates, nil
}

// Create persists a project and its member rows atomically. Member replacement
// remains visible here because it is part of the aggregate, not a DAL concern.
func (repository *Repository) Create(project *model.WebProject, memberIDs []int64, principals ...*utils.Principal) error {
	return db.MasterDB().Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(tx, project.OwnerUserID, principals, memberIDs); err != nil {
			return err
		}
		if err := requireProjectSlot(tx, project.OwnerUserID); err != nil {
			return err
		}
		if err := checkWriteExpiry(principals); err != nil {
			return err
		}
		if err := dal.CreateWebProject(tx, project); err != nil {
			return err
		}
		return dal.ReplaceWebProjectMembers(tx, project.ID, project.OwnerUserID, memberIDs)
	})
}

// Update 在同一事务中校验写权限、锁定项目，并按修订号更新项目字段和完整成员集合。
func (repository *Repository) Update(ownerUserID, projectID, revision int64, fields map[string]interface{}, memberIDs []int64, principals ...*utils.Principal) (bool, error) {
	updated := false
	// 在权限和审核状态仍允许写入时提交修订更新，只有项目更新成功才替换成员。
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(tx, ownerUserID, principals, memberIDs); err != nil {
			return err
		}
		project, err := dal.LockOwnedWebProject(tx, ownerUserID, projectID)
		if err != nil {
			return err
		}
		if project == nil {
			return apperrors.ErrNotFound
		}
		if project.ModerationStatus != "" && project.ModerationStatus != "normal" {
			return apperrors.ErrForbidden
		}
		if err := checkWriteExpiry(principals); err != nil {
			return err
		}
		updated, err = dal.UpdateProjectFields(tx, ownerUserID, projectID, revision, fields)
		if err != nil || !updated {
			return err
		}
		return dal.ReplaceWebProjectMembers(tx, projectID, ownerUserID, memberIDs)
	})
	return updated, err
}

// UpdateFields 在锁定项目后按修订号更新字段；恢复已删除项目时重新检查所有者的项目数量配额。
func (repository *Repository) UpdateFields(ownerUserID, projectID, revision int64, fields map[string]interface{}, principals ...*utils.Principal) (bool, error) {
	updated := false
	// 串行复核权限、审核状态和恢复配额，并在写入前再次检查凭据有效期。
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(tx, ownerUserID, principals, nil); err != nil {
			return err
		}
		project, err := dal.LockOwnedWebProject(tx, ownerUserID, projectID)
		if err != nil {
			return err
		}
		if project == nil {
			return apperrors.ErrNotFound
		}
		if project.ModerationStatus != "" && project.ModerationStatus != "normal" {
			return apperrors.ErrForbidden
		}
		if status, ok := fields["status"].(model.WebProjectStatus); ok && project.Status == model.WebProjectStatusDeleted && status != model.WebProjectStatusDeleted {
			if err := requireProjectSlot(tx, ownerUserID); err != nil {
				return err
			}
		}
		if err := checkWriteExpiry(principals); err != nil {
			return err
		}
		updated, err = dal.UpdateProjectFields(tx, ownerUserID, projectID, revision, fields)
		return err
	})
	return updated, err
}

func (repository *Repository) ListReleases(projectID, cursor int64, limit int) ([]*model.WebProjectRelease, error) {
	return dal.ListWebProjectReleases(projectID, cursor, limit)
}

func (repository *Repository) FindRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	return dal.QueryWebProjectRelease(projectID, releaseID)
}

// FindPublished resolves project and current release with two primary-key reads.
// A concurrent publish can only expose an old or new complete release row.
func (repository *Repository) FindPublished(slug string, contexts ...context.Context) (*model.WebProject, *model.WebProjectRelease, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	project, err := dal.QueryWebProjectBySlug(slug)
	if err != nil || project == nil || project.CurrentReleaseID == nil {
		return project, nil, err
	}
	release, err := dal.QueryPublishedWebProjectRelease(ctx, project.ID, *project.CurrentReleaseID, project.OwnerUserID)
	return project, release, err
}

func (repository *Repository) IsMember(projectID, userID int64) (bool, error) {
	return dal.IsWebProjectMember(projectID, userID)
}

func (repository *Repository) FindReleaseReferences(releaseIDs []int64) ([]*model.WebProjectRelease, error) {
	return dal.QueryWebProjectReleaseReferences(releaseIDs)
}

func (repository *Repository) FindProjectOwnerReferences(projectIDs []int64) ([]*model.WebProject, error) {
	return dal.QueryWebProjectOwnerReferences(projectIDs)
}

func (repository *Repository) CompareAndSwapReleaseStorageKey(releaseID, projectID, uploadedBy int64, oldStorageKey, newStorageKey string) (bool, error) {
	updated, err := dal.CompareAndSwapWebProjectReleaseStorageKey(releaseID, projectID, uploadedBy, oldStorageKey, newStorageKey)
	if err == nil && updated {
		dal.InvalidateWebReleaseCache(context.Background(), projectID, releaseID)
	}
	return updated, err
}

// Transaction exposes only web-project persistence operations. App services can
// keep locks around rule evaluation without importing GORM or calling DAL.
func (repository *Repository) Transaction(run func(*Transaction) error) error {
	transaction := &Transaction{}
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		transaction.database = tx
		return run(transaction)
	})
	if err == nil {
		for _, release := range transaction.invalidatedReleases {
			dal.InvalidateWebReleaseCache(context.Background(), release[0], release[1])
		}
	}
	return err
}

type Transaction struct {
	database            *gorm.DB
	principals          []*utils.Principal
	invalidatedReleases [][2]int64
}

func (tx *Transaction) LockOwnedProject(ownerUserID, projectID int64, principals ...*utils.Principal) (*model.WebProject, error) {
	if err := requireWritePolicy(tx.database, ownerUserID, principals, nil); err != nil {
		return nil, err
	}
	tx.principals = append([]*utils.Principal(nil), principals...)
	project, err := dal.LockOwnedWebProject(tx.database, ownerUserID, projectID)
	if err == nil && project != nil && project.ModerationStatus != "" && project.ModerationStatus != "normal" {
		return nil, apperrors.ErrForbidden
	}
	return project, err
}

func (tx *Transaction) FindReleaseByIdempotencyKey(projectID int64, key string) (*model.WebProjectRelease, error) {
	return dal.QueryWebProjectReleaseByIdempotencyKey(projectID, key, tx.database)
}

func (tx *Transaction) ListReadyReleases(projectID int64, limit int) ([]*model.WebProjectRelease, error) {
	return dal.ListReadyWebProjectReleases(tx.database, projectID, limit)
}

func (tx *Transaction) MarkReleasesDeleting(projectID int64, releaseIDs []int64) (int64, error) {
	if err := checkWriteExpiry(tx.principals); err != nil {
		return 0, err
	}
	count, err := dal.MarkWebProjectReleasesDeleting(tx.database, projectID, releaseIDs, true)
	if err == nil && count > 0 {
		for _, releaseID := range releaseIDs {
			tx.invalidatedReleases = append(tx.invalidatedReleases, [2]int64{projectID, releaseID})
		}
	}
	return count, err
}

func (tx *Transaction) CreateRelease(release *model.WebProjectRelease) error {
	if err := checkWriteExpiry(tx.principals); err != nil {
		return err
	}
	if err := release.EncodeExtra(); err != nil {
		return err
	}
	return dal.CreateWebProjectRelease(tx.database, release)
}

func (tx *Transaction) LockRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	return dal.LockWebProjectRelease(tx.database, projectID, releaseID)
}

func (tx *Transaction) UpdateProject(ownerUserID, projectID, revision int64, fields map[string]interface{}) (bool, error) {
	if err := checkWriteExpiry(tx.principals); err != nil {
		return false, err
	}
	return dal.UpdateProjectFields(tx.database, ownerUserID, projectID, revision, fields)
}
