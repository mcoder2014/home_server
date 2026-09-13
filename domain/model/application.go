package model

import "time"

const (
	ApplicationStatusEnabled  = 1
	ApplicationStatusDisabled = 2
	ApplicationStatusRevoked  = 3
)

// Application stores an owner's application credential. SecretDigest is the
// only persisted representation of the secret key.
type Application struct {
	ActorAuthVersion int64      `json:"-" gorm:"-"`
	ID               int64      `json:"-" gorm:"column:id"`
	OwnerUserID      int64      `json:"-" gorm:"column:owner_user_id"`
	Name             string     `json:"-" gorm:"column:name"`
	Description      string     `json:"-" gorm:"column:description"`
	AccessKey        string     `json:"-" gorm:"column:access_key"`
	SecretDigest     []byte     `json:"-" gorm:"column:secret_digest"`
	Scopes           []string   `json:"-" gorm:"column:scopes;serializer:json"`
	Status           int        `json:"-" gorm:"column:status"`
	ActiveSlot       *int       `json:"-" gorm:"column:active_slot"`
	Revision         int64      `json:"-" gorm:"column:revision"`
	SecretVersion    int64      `json:"-" gorm:"column:secret_version"`
	ExpiresAt        time.Time  `json:"-" gorm:"column:expires_at"`
	LastIssuedAt     *time.Time `json:"-" gorm:"column:last_issued_at"`
	CreateTime       time.Time  `json:"-" gorm:"column:create_time"`
	UpdateTime       time.Time  `json:"-" gorm:"column:update_time"`
}

// ApplicationAccessToken stores an opaque bearer token digest and the
// application authorization snapshot against which it was issued.
type ApplicationAccessToken struct {
	ID                  int64     `json:"-" gorm:"column:id"`
	ApplicationID       int64     `json:"-" gorm:"column:application_id"`
	TokenDigest         []byte    `json:"-" gorm:"column:token_digest"`
	SecretVersion       int64     `json:"-" gorm:"column:secret_version"`
	ApplicationRevision int64     `json:"-" gorm:"column:application_revision"`
	ScopeSnapshot       []string  `json:"-" gorm:"column:scope_snapshot;serializer:json"`
	ExpiredAt           time.Time `json:"-" gorm:"column:expired_at"`
	CreateTime          time.Time `json:"-" gorm:"column:create_time"`
}
