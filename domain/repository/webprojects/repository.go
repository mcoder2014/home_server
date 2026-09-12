package webprojects

import (
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
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
func (repository *Repository) Create(project *model.WebProject, memberIDs []int64) error {
	return db.MasterDB().Transaction(func(tx *gorm.DB) error {
		if err := dal.CreateWebProject(tx, project); err != nil {
			return err
		}
		return dal.ReplaceWebProjectMembers(tx, project.ID, project.OwnerUserID, memberIDs)
	})
}

func (repository *Repository) Update(ownerUserID, projectID, revision int64, fields map[string]interface{}, memberIDs []int64) (bool, error) {
	updated := false
	err := db.MasterDB().Transaction(func(tx *gorm.DB) error {
		var err error
		updated, err = dal.UpdateProjectFields(tx, ownerUserID, projectID, revision, fields)
		if err != nil || !updated {
			return err
		}
		return dal.ReplaceWebProjectMembers(tx, projectID, ownerUserID, memberIDs)
	})
	return updated, err
}

func (repository *Repository) UpdateFields(ownerUserID, projectID, revision int64, fields map[string]interface{}) (bool, error) {
	return dal.UpdateProjectFields(db.MasterDB(), ownerUserID, projectID, revision, fields)
}

func (repository *Repository) ListReleases(projectID, cursor int64, limit int) ([]*model.WebProjectRelease, error) {
	return dal.ListWebProjectReleases(projectID, cursor, limit)
}

func (repository *Repository) FindRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	return dal.QueryWebProjectRelease(projectID, releaseID)
}

// FindPublished resolves project and current release with two primary-key reads.
// A concurrent publish can only expose an old or new complete release row.
func (repository *Repository) FindPublished(slug string) (*model.WebProject, *model.WebProjectRelease, error) {
	project, err := dal.QueryWebProjectBySlug(slug)
	if err != nil || project == nil || project.CurrentReleaseID == nil {
		return project, nil, err
	}
	release, err := dal.QueryWebProjectRelease(project.ID, *project.CurrentReleaseID)
	return project, release, err
}

func (repository *Repository) IsMember(projectID, userID int64) (bool, error) {
	return dal.IsWebProjectMember(projectID, userID)
}

func (repository *Repository) FindReleaseReferences(releaseIDs []int64) ([]*model.WebProjectRelease, error) {
	return dal.QueryWebProjectReleaseReferences(releaseIDs)
}

// Transaction exposes only web-project persistence operations. App services can
// keep locks around rule evaluation without importing GORM or calling DAL.
func (repository *Repository) Transaction(run func(*Transaction) error) error {
	return db.MasterDB().Transaction(func(tx *gorm.DB) error {
		return run(&Transaction{database: tx})
	})
}

type Transaction struct {
	database *gorm.DB
}

func (tx *Transaction) LockOwnedProject(ownerUserID, projectID int64) (*model.WebProject, error) {
	return dal.LockOwnedWebProject(tx.database, ownerUserID, projectID)
}

func (tx *Transaction) FindReleaseByIdempotencyKey(projectID int64, key string) (*model.WebProjectRelease, error) {
	return dal.QueryWebProjectReleaseByIdempotencyKey(projectID, key, tx.database)
}

func (tx *Transaction) ListReadyReleases(projectID int64, limit int) ([]*model.WebProjectRelease, error) {
	return dal.ListReadyWebProjectReleases(tx.database, projectID, limit)
}

func (tx *Transaction) MarkReleasesDeleting(projectID int64, releaseIDs []int64) (int64, error) {
	return dal.MarkWebProjectReleasesDeleting(tx.database, projectID, releaseIDs, true)
}

func (tx *Transaction) CreateRelease(release *model.WebProjectRelease) error {
	if err := release.EncodeExtra(); err != nil {
		return err
	}
	return dal.CreateWebProjectRelease(tx.database, release)
}

func (tx *Transaction) LockRelease(projectID, releaseID int64) (*model.WebProjectRelease, error) {
	return dal.LockWebProjectRelease(tx.database, projectID, releaseID)
}

func (tx *Transaction) UpdateProject(ownerUserID, projectID, revision int64, fields map[string]interface{}) (bool, error) {
	return dal.UpdateProjectFields(tx.database, ownerUserID, projectID, revision, fields)
}
