package dal

import (
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	FileTable            = "files"
	FileShareTable       = "file_shares"
	FileShareMemberTable = "file_share_members"
)

var fileColumns = []string{"id", "owner_user_id", "original_name", "storage_key", "size_bytes", "sha256", "deleted_at", "create_time"}
var fileShareColumns = []string{"id", "file_id", "owner_user_id", "token", "access_mode", "secret_mode", "secret_hash", "expires_at", "max_downloads", "download_count", "revoked_at", "create_time", "update_time"}

type FileShareCount struct {
	FileID int64 `gorm:"column:file_id"`
	Count  int64 `gorm:"column:share_count"`
}

func InsertFile(tx *gorm.DB, file *model.StoredFile) error {
	return tx.Table(FileTable).Select(fileColumns).Create(file).Error
}

func FindFile(database *gorm.DB, id int64, lock bool) (*model.StoredFile, error) {
	query := database.Table(FileTable).Select(fileColumns).Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var file model.StoredFile
	err := query.Take(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &file, err
}

func FindOwnedFile(database *gorm.DB, ownerID, id int64, includeDeleted, lock bool) (*model.StoredFile, error) {
	query := database.Table(FileTable).Select(fileColumns).Where("owner_user_id = ? AND id = ?", ownerID, id)
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var file model.StoredFile
	err := query.Take(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &file, err
}

func ListOwnedFiles(database *gorm.DB, ownerID, cursor int64, limit int) ([]*model.StoredFile, error) {
	query := database.Table(FileTable).Select(fileColumns).Where("owner_user_id = ? AND deleted_at IS NULL", ownerID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	var files []*model.StoredFile
	err := query.Order("id DESC").Limit(limit).Find(&files).Error
	return files, err
}

func CountFileShares(database *gorm.DB, fileIDs []int64) (map[int64]int64, error) {
	result := make(map[int64]int64, len(fileIDs))
	if len(fileIDs) == 0 {
		return result, nil
	}
	var rows []FileShareCount
	err := database.Table(FileShareTable).Select("file_id, COUNT(*) AS share_count").Where("file_id IN ?", fileIDs).Group("file_id").Find(&rows).Error
	for _, row := range rows {
		result[row.FileID] = row.Count
	}
	return result, err
}

func CountOwnedFiles(tx *gorm.DB, ownerID int64) (int64, error) {
	var count int64
	err := tx.Table(FileTable).Where("owner_user_id = ? AND deleted_at IS NULL", ownerID).Count(&count).Error
	return count, err
}

func SumOwnedFileBytes(tx *gorm.DB, ownerID int64) (int64, error) {
	var result struct {
		Bytes       int64 `gorm:"column:bytes"`
		InvalidRows int64 `gorm:"column:invalid_rows"`
	}
	err := tx.Table(FileTable).Select("COALESCE(SUM(size_bytes),0) AS bytes, COALESCE(SUM(CASE WHEN size_bytes < 0 THEN 1 ELSE 0 END),0) AS invalid_rows").Where("owner_user_id = ? AND deleted_at IS NULL", ownerID).Scan(&result).Error
	if err != nil || result.Bytes < 0 || result.InvalidRows != 0 {
		if err == nil {
			err = errors.New("invalid stored file size")
		}
		return 0, err
	}
	return result.Bytes, nil
}

func DeleteOwnedFile(tx *gorm.DB, ownerID, id int64, deletedAt time.Time) (bool, error) {
	result := tx.Table(FileTable).Where("owner_user_id = ? AND id = ? AND deleted_at IS NULL", ownerID, id).Update("deleted_at", deletedAt)
	return result.RowsAffected == 1, result.Error
}

func InsertFileShare(tx *gorm.DB, share *model.FileShare, memberIDs []int64) error {
	if err := tx.Session(&gorm.Session{Logger: logger.Discard}).Table(FileShareTable).Select(fileShareColumns).Create(share).Error; err != nil {
		return err
	}
	if len(memberIDs) == 0 {
		return nil
	}
	members := make([]*model.FileShareMember, 0, len(memberIDs))
	for _, userID := range memberIDs {
		members = append(members, &model.FileShareMember{ShareID: share.ID, UserID: userID, CreateTime: share.CreateTime})
	}
	return tx.Table(FileShareMemberTable).Select("share_id", "user_id", "create_time").Create(&members).Error
}

func FindShare(database *gorm.DB, id int64, lock bool) (*model.FileShare, error) {
	query := database.Table(FileShareTable).Select(fileShareColumns).Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var share model.FileShare
	err := query.Take(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &share, err
}

func FindShareByToken(database *gorm.DB, token string, lock bool) (*model.FileShare, error) {
	query := database.Session(&gorm.Session{Logger: logger.Discard}).Table(FileShareTable).Select(fileShareColumns).Where("token = ?", token)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var share model.FileShare
	err := query.Take(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &share, err
}

func FindOwnedShare(database *gorm.DB, ownerID, fileID, shareID int64, lock bool) (*model.FileShare, error) {
	query := database.Table(FileShareTable).Select(fileShareColumns).Where("owner_user_id = ? AND file_id = ? AND id = ?", ownerID, fileID, shareID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var share model.FileShare
	err := query.Take(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &share, err
}

func ListOwnedShares(database *gorm.DB, ownerID, fileID, cursor int64, limit int) ([]*model.FileShare, error) {
	query := database.Table(FileShareTable).Select(fileShareColumns).Where("owner_user_id = ? AND file_id = ?", ownerID, fileID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	var shares []*model.FileShare
	err := query.Order("id DESC").Limit(limit).Find(&shares).Error
	return shares, err
}

func ListShareMembers(database *gorm.DB, shareIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64, len(shareIDs))
	if len(shareIDs) == 0 {
		return result, nil
	}
	var rows []*model.FileShareMember
	err := database.Table(FileShareMemberTable).Select("share_id", "user_id", "create_time").Where("share_id IN ?", shareIDs).Order("share_id ASC, user_id ASC").Find(&rows).Error
	for _, row := range rows {
		result[row.ShareID] = append(result[row.ShareID], row.UserID)
	}
	return result, err
}

func IsFileShareMember(database *gorm.DB, shareID, userID int64, lock bool) (bool, error) {
	query := database.Table(FileShareMemberTable).Select("share_id", "user_id").Where("share_id = ? AND user_id = ?", shareID, userID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var member model.FileShareMember
	err := query.Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func RevokeOwnedShare(tx *gorm.DB, ownerID, fileID, shareID int64, revokedAt time.Time) (bool, error) {
	result := tx.Table(FileShareTable).Where("owner_user_id = ? AND file_id = ? AND id = ? AND revoked_at IS NULL", ownerID, fileID, shareID).Updates(map[string]interface{}{"revoked_at": revokedAt, "update_time": revokedAt})
	return result.RowsAffected == 1, result.Error
}

func IncrementDownloadCount(tx *gorm.DB, shareID int64, now time.Time) (bool, error) {
	result := tx.Table(FileShareTable).Where("id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND (max_downloads = 0 OR download_count < max_downloads)", shareID, now).Updates(map[string]interface{}{"download_count": gorm.Expr("download_count + 1"), "update_time": now})
	return result.RowsAffected == 1, result.Error
}
