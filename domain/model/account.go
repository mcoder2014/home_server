package model

import (
	"strconv"
	"time"
)

const (
	AccountActive         = "active"
	AccountBanned         = "banned"
	AccountDeleted        = "deleted"
	RoleUser              = "user"
	RoleAdmin             = "admin"
	WebDAVNone            = "none"
	WebDAVRead            = "read"
	WebDAVWrite           = "write"
	SessionUser           = "user"
	SessionPasswordChange = "password_change"
)

// UserAccount is the database identity. Passwords and legacy login aliases are
// never part of an API response; handlers return an explicit AccountView.
type UserAccount struct {
	ID                 int64      `gorm:"column:id;primaryKey" json:"id,string"`
	Username           string     `gorm:"column:username" json:"user_name"`
	UsernameKey        string     `gorm:"column:username_key" json:"-"`
	DisplayName        string     `gorm:"column:display_name" json:"display_name"`
	AvatarVersion      int64      `gorm:"column:avatar_version" json:"avatar_version"`
	AvatarURL          string     `gorm:"-" json:"avatar_url"`
	ContactEmail       string     `gorm:"column:contact_email" json:"contact_email"`
	ContactMobile      string     `gorm:"column:contact_mobile" json:"contact_mobile"`
	PasswordHash       string     `gorm:"column:password_hash" json:"-"`
	Status             string     `gorm:"column:status" json:"status"`
	Role               string     `gorm:"column:role" json:"role"`
	LibraryEnabled     bool       `gorm:"column:library_enabled" json:"library_enabled"`
	WebDAVPermission   string     `gorm:"column:webdav_permission" json:"webdav_permission"`
	AuthVersion        int64      `gorm:"column:auth_version" json:"-"`
	Revision           int64      `gorm:"column:revision" json:"revision"`
	MustChangePassword bool       `gorm:"column:must_change_password" json:"must_change_password"`
	PasswordExpiresAt  *time.Time `gorm:"column:password_expires_at" json:"password_expires_at,omitempty"`
	InviteEligibleAt   time.Time  `gorm:"column:invite_eligible_at" json:"invite_eligible_at"`
	Source             string     `gorm:"column:source" json:"source"`
	InvitedByUserID    *int64     `gorm:"column:invited_by_user_id" json:"invited_by_user_id,omitempty,string"`
	CreatedByUserID    *int64     `gorm:"column:created_by_user_id" json:"created_by_user_id,omitempty,string"`
	ImportedAt         *time.Time `gorm:"column:imported_at" json:"imported_at,omitempty"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at" json:"last_login_at,omitempty"`
	DeletedAt          *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
	CreateTime         time.Time  `gorm:"column:create_time" json:"create_time"`
	UpdateTime         time.Time  `gorm:"column:update_time" json:"update_time"`
}

// AccountAvatarURL derives an authorized, versioned address without reading image bytes.
func AccountAvatarURL(user *UserAccount) string {
	if user == nil || user.Status != AccountActive || user.MustChangePassword || user.AvatarVersion <= 0 {
		return ""
	}
	return "/api/account/avatars/" + strconv.FormatInt(user.ID, 10) + "/" + strconv.FormatInt(user.AvatarVersion, 10)
}

type UserDisplay struct {
	UserID      int64  `json:"user_id,string"`
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

type UserAvatar struct {
	UserID      int64     `gorm:"column:user_id;primaryKey" json:"-"`
	Version     int64     `gorm:"column:version" json:"-"`
	ContentType string    `gorm:"column:content_type" json:"-"`
	ContentBlob []byte    `gorm:"column:content_blob" json:"-"`
	ByteSize    uint32    `gorm:"column:byte_size" json:"-"`
	UpdateTime  time.Time `gorm:"column:update_time" json:"-"`
}

type LoginAlias struct {
	LoginKey string `gorm:"column:login_key;primaryKey"`
	UserID   int64  `gorm:"column:user_id"`
	Kind     string `gorm:"column:kind"`
}

type AccountSession struct {
	ID              int64     `gorm:"column:id;primaryKey" json:"-"`
	UserID          int64     `gorm:"column:user_id" json:"-"`
	TokenDigest     []byte    `gorm:"column:token_digest" json:"-"`
	AuthVersion     int64     `gorm:"column:auth_version" json:"-"`
	Purpose         string    `gorm:"column:purpose" json:"-"`
	IsExpired       int       `gorm:"column:is_expired" json:"-"`
	AuthenticatedAt time.Time `gorm:"column:authenticated_at" json:"-"`
	LoginIP         string    `gorm:"column:login_ip" json:"-"`
	UserAgent       string    `gorm:"column:user_agent" json:"-"`
	ClientName      string    `gorm:"column:client_name" json:"-"`
	OSName          string    `gorm:"column:os_name" json:"-"`
	DeviceType      string    `gorm:"column:device_type" json:"-"`
	LoginSource     string    `gorm:"column:login_source" json:"-"`
	ExpireTime      time.Time `gorm:"column:expire_time" json:"-"`
	CreateTime      time.Time `gorm:"column:create_time" json:"-"`
	UpdateTime      time.Time `gorm:"column:update_time" json:"-"`
}

type UserInvitation struct {
	ID                int64      `gorm:"column:id;primaryKey" json:"id,string"`
	InviterUserID     int64      `gorm:"column:inviter_user_id" json:"inviter_user_id,string"`
	QuotaMonth        string     `gorm:"column:quota_month" json:"quota_month"`
	Slot              int        `gorm:"column:slot" json:"-"`
	TokenDigest       []byte     `gorm:"column:token_digest" json:"-"`
	TokenHint         string     `gorm:"column:token_hint" json:"token_hint"`
	RegistrationEpoch int64      `gorm:"column:registration_epoch" json:"-"`
	Status            string     `gorm:"column:status" json:"status"`
	ExpiresAt         time.Time  `gorm:"column:expires_at" json:"expires_at"`
	UsedByUserID      *int64     `gorm:"column:used_by_user_id" json:"used_by_user_id,omitempty,string"`
	UsedAt            *time.Time `gorm:"column:used_at" json:"used_at,omitempty"`
	RevokedAt         *time.Time `gorm:"column:revoked_at" json:"revoked_at,omitempty"`
	RevokeReason      string     `gorm:"column:revoke_reason" json:"revoke_reason,omitempty"`
	Note              string     `gorm:"column:note" json:"note"`
	RequestID         string     `gorm:"column:request_id" json:"-"`
	CreateTime        time.Time  `gorm:"column:create_time" json:"create_time"`
	UsedByUserName    string     `gorm:"-" json:"used_by_user_name,omitempty"`

	UsedByUser *UserDisplay `gorm:"-" json:"used_by_user,omitempty"`
}

type AdminAuditLog struct {
	ID            int64     `gorm:"column:id;primaryKey" json:"id,string"`
	ActorUserID   int64     `gorm:"column:actor_user_id" json:"actor_user_id,string"`
	Action        string    `gorm:"column:action" json:"action"`
	TargetType    string    `gorm:"column:target_type" json:"target_type"`
	TargetID      int64     `gorm:"column:target_id" json:"target_id,string"`
	BeforeSummary string    `gorm:"column:before_summary" json:"before_summary"`
	AfterSummary  string    `gorm:"column:after_summary" json:"after_summary"`
	Reason        string    `gorm:"column:reason" json:"reason"`
	Result        string    `gorm:"column:result" json:"result"`
	RequestID     string    `gorm:"column:request_id" json:"request_id"`
	CreateTime    time.Time `gorm:"column:create_time" json:"create_time"`
}
