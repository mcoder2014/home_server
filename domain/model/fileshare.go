package model

import "time"

type FileShareAccess uint8

const (
	FileShareAccessPublic        FileShareAccess = 1
	FileShareAccessAuthenticated FileShareAccess = 2
	FileShareAccessMembers       FileShareAccess = 3
)

func (value FileShareAccess) String() string {
	switch value {
	case FileShareAccessPublic:
		return "public"
	case FileShareAccessAuthenticated:
		return "authenticated"
	case FileShareAccessMembers:
		return "members"
	default:
		return ""
	}
}

func ParseFileShareAccess(value string) (FileShareAccess, bool) {
	switch value {
	case "public":
		return FileShareAccessPublic, true
	case "authenticated":
		return FileShareAccessAuthenticated, true
	case "members":
		return FileShareAccessMembers, true
	default:
		return 0, false
	}
}

type FileShareSecret uint8

const (
	FileShareSecretNone     FileShareSecret = 1
	FileShareSecretCode     FileShareSecret = 2
	FileShareSecretPassword FileShareSecret = 3
)

func (value FileShareSecret) String() string {
	switch value {
	case FileShareSecretNone:
		return "none"
	case FileShareSecretCode:
		return "code"
	case FileShareSecretPassword:
		return "password"
	default:
		return ""
	}
}

func ParseFileShareSecret(value string) (FileShareSecret, bool) {
	switch value {
	case "none":
		return FileShareSecretNone, true
	case "code":
		return FileShareSecretCode, true
	case "password":
		return FileShareSecretPassword, true
	default:
		return 0, false
	}
}

type StoredFile struct {
	ID           int64      `gorm:"column:id"`
	OwnerUserID  int64      `gorm:"column:owner_user_id"`
	OriginalName string     `gorm:"column:original_name"`
	StorageKey   string     `gorm:"column:storage_key"`
	SizeBytes    int64      `gorm:"column:size_bytes"`
	SHA256       string     `gorm:"column:sha256"`
	DeletedAt    *time.Time `gorm:"column:deleted_at"`
	CreateTime   time.Time  `gorm:"column:create_time"`
}

type FileShare struct {
	ID            int64           `gorm:"column:id"`
	FileID        int64           `gorm:"column:file_id"`
	OwnerUserID   int64           `gorm:"column:owner_user_id"`
	Token         string          `gorm:"column:token"`
	AccessMode    FileShareAccess `gorm:"column:access_mode"`
	SecretMode    FileShareSecret `gorm:"column:secret_mode"`
	SecretHash    string          `gorm:"column:secret_hash"`
	ExpiresAt     *time.Time      `gorm:"column:expires_at"`
	MaxDownloads  int64           `gorm:"column:max_downloads"`
	DownloadCount int64           `gorm:"column:download_count"`
	RevokedAt     *time.Time      `gorm:"column:revoked_at"`
	CreateTime    time.Time       `gorm:"column:create_time"`
	UpdateTime    time.Time       `gorm:"column:update_time"`
}

type FileShareMember struct {
	ShareID    int64     `gorm:"column:share_id"`
	UserID     int64     `gorm:"column:user_id"`
	CreateTime time.Time `gorm:"column:create_time"`
}
