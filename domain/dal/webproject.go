package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WebProjectTable        = "web_project"
	WebProjectMemberTable  = "web_project_member"
	WebProjectReleaseTable = "web_project_release"
)

var projectColumns = []string{"id", "owner_user_id", "name", "description", "slug", "access_mode", "status", "current_release_id", "revision", "client_request_id", "deleted_at", "create_time", "update_time"}
var releaseColumns = []string{"id", "project_id", "uploaded_by", "storage_key", "status", "entry_file", "sha256", "file_count", "total_bytes", "idempotency_key", "extra", "create_time", "update_time"}

func CreateWebProject(tx *gorm.DB, project *model.WebProject) error {
	return tx.Table(WebProjectTable).Create(project).Error
}

func LockOwnedWebProject(tx *gorm.DB, ownerUserID, projectID int64) (*model.WebProject, error) {
	var project model.WebProject
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table(WebProjectTable).Select(projectColumns).Where("owner_user_id = ? AND id = ?", ownerUserID, projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func QueryOwnedWebProject(ownerUserID, projectID int64, tx ...*gorm.DB) (*model.WebProject, error) {
	database := selectDB(tx)
	var project model.WebProject
	err := database.Table(WebProjectTable).Select(projectColumns).Where("owner_user_id = ? AND id = ?", ownerUserID, projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func QueryWebProjectBySlug(slug string) (*model.WebProject, error) {
	var project model.WebProject
	err := db.MasterDB().Table(WebProjectTable).Select(projectColumns).Where("slug = ?", slug).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func QueryWebProjectByID(projectID int64) (*model.WebProject, error) {
	var project model.WebProject
	err := db.MasterDB().Table(WebProjectTable).Select(projectColumns).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func QueryWebProjectByRequestID(ownerUserID int64, requestID string) (*model.WebProject, error) {
	var project model.WebProject
	err := db.MasterDB().Table(WebProjectTable).Select(projectColumns).Where("owner_user_id = ? AND client_request_id = ?", ownerUserID, requestID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &project, err
}

func ListOwnedWebProjects(ownerUserID, cursor int64, limit int, status model.WebProjectStatus) ([]*model.WebProject, error) {
	query := db.MasterDB().Table(WebProjectTable).Select(projectColumns).Where("owner_user_id = ?", ownerUserID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	if status == 0 {
		query = query.Where("status <> ?", model.WebProjectStatusDeleted)
	} else {
		query = query.Where("status = ?", status)
	}
	var projects []*model.WebProject
	err := query.Order("id DESC").Limit(limit).Find(&projects).Error
	return projects, err
}

func UpdateProjectFields(tx *gorm.DB, ownerUserID, projectID, revision int64, fields map[string]interface{}) (bool, error) {
	result := tx.Table(WebProjectTable).Where("owner_user_id = ? AND id = ? AND revision = ?", ownerUserID, projectID, revision).Updates(fields)
	return result.RowsAffected == 1, result.Error
}

func ListWebProjectMemberIDs(projectID int64, tx ...*gorm.DB) ([]int64, error) {
	database := selectDB(tx)
	var ids []int64
	err := database.Table(WebProjectMemberTable).Where("project_id = ?", projectID).Order("user_id ASC").Pluck("user_id", &ids).Error
	return ids, err
}

func ListWebProjectMembers(projectIDs []int64) ([]*model.WebProjectMember, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	var members []*model.WebProjectMember
	err := db.MasterDB().Table(WebProjectMemberTable).Select("project_id", "user_id").Where("project_id IN ?", projectIDs).Order("project_id ASC, user_id ASC").Find(&members).Error
	return members, err
}

func ReplaceWebProjectMembers(tx *gorm.DB, projectID, createdBy int64, userIDs []int64) error {
	if err := tx.Table(WebProjectMemberTable).Where("project_id = ?", projectID).Delete(&model.WebProjectMember{}).Error; err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return nil
	}
	members := make([]model.WebProjectMember, 0, len(userIDs))
	createTime := time.Now()
	for _, userID := range userIDs {
		members = append(members, model.WebProjectMember{ProjectID: projectID, UserID: userID, CreatedBy: createdBy, CreateTime: createTime})
	}
	return tx.Table(WebProjectMemberTable).Create(&members).Error
}

func IsWebProjectMember(projectID, userID int64) (bool, error) {
	var member model.WebProjectMember
	err := db.MasterDB().Table(WebProjectMemberTable).Select("project_id", "user_id").Where("project_id = ? AND user_id = ?", projectID, userID).Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func CreateWebProjectRelease(tx *gorm.DB, release *model.WebProjectRelease) error {
	return tx.Table(WebProjectReleaseTable).Create(release).Error
}

func QueryWebProjectRelease(projectID, releaseID int64, tx ...*gorm.DB) (*model.WebProjectRelease, error) {
	database := selectDB(tx)
	var release model.WebProjectRelease
	err := database.Table(WebProjectReleaseTable).Select(releaseColumns).Where("project_id = ? AND id = ?", projectID, releaseID).Take(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil {
		err = release.DecodeExtra()
	}
	return &release, err
}

func LockWebProjectRelease(tx *gorm.DB, projectID, releaseID int64) (*model.WebProjectRelease, error) {
	var release model.WebProjectRelease
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table(WebProjectReleaseTable).Select(releaseColumns).Where("project_id = ? AND id = ?", projectID, releaseID).Take(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil {
		err = release.DecodeExtra()
	}
	return &release, err
}

func QueryWebProjectReleaseByIdempotencyKey(projectID int64, key string, tx ...*gorm.DB) (*model.WebProjectRelease, error) {
	database := selectDB(tx)
	var release model.WebProjectRelease
	err := database.Table(WebProjectReleaseTable).Select(releaseColumns).Where("project_id = ? AND idempotency_key = ?", projectID, key).Take(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil {
		err = release.DecodeExtra()
	}
	return &release, err
}

func ListWebProjectReleases(projectID, cursor int64, limit int) ([]*model.WebProjectRelease, error) {
	query := db.MasterDB().Table(WebProjectReleaseTable).Select(releaseColumns).Where("project_id = ?", projectID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	var releases []*model.WebProjectRelease
	err := query.Order("id DESC").Limit(limit).Find(&releases).Error
	if err == nil {
		for _, release := range releases {
			if decodeErr := release.DecodeExtra(); decodeErr != nil {
				return nil, decodeErr
			}
		}
	}
	return releases, err
}

func selectDB(tx []*gorm.DB) *gorm.DB {
	if len(tx) > 0 && tx[0] != nil {
		return tx[0]
	}
	return db.MasterDB()
}
