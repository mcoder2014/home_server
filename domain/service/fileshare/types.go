package fileshare

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
)

var (
	ErrInvalid       = apperrors.ErrInvalid
	ErrUnauthorized  = apperrors.ErrUnauthorized
	ErrForbidden     = apperrors.ErrForbidden
	ErrNotFound      = apperrors.ErrNotFound
	ErrConflict      = apperrors.ErrConflict
	ErrTooLarge      = apperrors.ErrTooLarge
	ErrUnprocessable = apperrors.ErrUnprocessable
	ErrRateLimited   = apperrors.ErrRateLimited
	ErrDependency    = apperrors.ErrDependency
)

type CreateShareInput struct {
	AccessMode    string     `json:"access_mode"`
	MemberUserIDs []string   `json:"member_user_ids"`
	SecretMode    string     `json:"secret_mode"`
	Secret        string     `json:"secret"`
	ExpiresAt     *time.Time `json:"expires_at"`
	MaxDownloads  int64      `json:"max_downloads"`
}

type NormalizedShareInput struct {
	AccessMode    model.FileShareAccess
	MemberUserIDs []int64
	SecretMode    model.FileShareSecret
	Secret        string
	ExpiresAt     *time.Time
	MaxDownloads  int64
}

type FileView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SizeBytes  int64      `json:"size_bytes"`
	SHA256     string     `json:"sha256"`
	ShareCount int64      `json:"share_count"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	CreateTime time.Time  `json:"create_time"`
}

type FilePage struct {
	Items      []*FileView `json:"items"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
}

type EligibleUser struct {
	ID          string `json:"id"`
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

type ShareView struct {
	ID            string     `json:"id"`
	FileID        string     `json:"file_id"`
	Token         string     `json:"token"`
	URL           string     `json:"url"`
	AccessMode    string     `json:"access_mode"`
	MemberUserIDs []string   `json:"member_user_ids"`
	SecretMode    string     `json:"secret_mode"`
	ExpiresAt     *time.Time `json:"expires_at"`
	MaxDownloads  int64      `json:"max_downloads"`
	DownloadCount int64      `json:"download_count"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreateTime    time.Time  `json:"create_time"`
}

type SharePage struct {
	Items      []*ShareView `json:"items"`
	NextCursor string       `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
}

type CreateShareResult struct {
	Share  *ShareView `json:"share"`
	Secret string     `json:"secret"`
}

type PublicFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

type PublicShareState struct {
	State              string      `json:"state"`
	File               *PublicFile `json:"file,omitempty"`
	ExpiresAt          *time.Time  `json:"expires_at"`
	MaxDownloads       int64       `json:"max_downloads"`
	DownloadCount      int64       `json:"download_count"`
	RemainingDownloads *int64      `json:"remaining_downloads"`
	SecretMode         string      `json:"secret_mode"`
	AccessMode         string      `json:"access_mode"`
}

type ShareSnapshot struct {
	AccessMode    string
	SecretMode    string
	ExpiresAt     *time.Time
	MaxDownloads  int64
	DownloadCount int64
	RevokedAt     *time.Time
	FileDeleted   bool
	Member        bool
	OwnerUserID   int64
	FileName      string
	FileSize      int64
}

type Viewer struct {
	UserID int64
}

func NormalizeShareInput(input CreateShareInput, random io.Reader, now time.Time) (*NormalizedShareInput, error) {
	access, ok := model.ParseFileShareAccess(input.AccessMode)
	if !ok || input.MaxDownloads < 0 || input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return nil, ErrInvalid
	}
	secretMode, ok := model.ParseFileShareSecret(input.SecretMode)
	if !ok {
		return nil, ErrInvalid
	}
	members, err := parseMemberIDs(input.MemberUserIDs)
	if err != nil || (access == model.FileShareAccessMembers && len(members) == 0) || (access != model.FileShareAccessMembers && len(members) != 0) {
		return nil, ErrInvalid
	}
	secret := input.Secret
	switch secretMode {
	case model.FileShareSecretNone:
		if secret != "" {
			return nil, ErrInvalid
		}
	case model.FileShareSecretCode:
		if access != model.FileShareAccessPublic {
			return nil, ErrInvalid
		}
		if secret == "" {
			secret, err = randomCode(random)
		}
		if err != nil || !ValidCode(secret) {
			return nil, ErrInvalid
		}
	case model.FileShareSecretPassword:
		if access == model.FileShareAccessPublic || !validPassword(secret) {
			return nil, ErrInvalid
		}
	}
	return &NormalizedShareInput{AccessMode: access, MemberUserIDs: members, SecretMode: secretMode, Secret: secret, ExpiresAt: input.ExpiresAt, MaxDownloads: input.MaxDownloads}, nil
}

func ValidCode(value string) bool {
	if len(value) != config.Runtime().AccountPolicy.ShareCodeLength {
		return false
	}
	for _, character := range []byte(value) {
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				if character < 'a' || character > 'z' {
					return false
				}
			}
		}
	}
	return true
}

func validPassword(value string) bool {
	return utf8.ValidString(value) && len([]byte(value)) >= config.Runtime().AccountPolicy.MinSharePasswordLength && len([]byte(value)) <= 72
}

func randomCode(source io.Reader) (string, error) {
	if source == nil {
		source = rand.Reader
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	raw := make([]byte, config.Runtime().AccountPolicy.ShareCodeLength)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", err
	}
	for index := range raw {
		raw[index] = alphabet[int(raw[index])%len(alphabet)]
	}
	return string(raw), nil
}

func NewToken(source io.Reader) (string, error) {
	if source == nil {
		source = rand.Reader
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ValidToken(token string) bool {
	if len(token) < 32 || len(token) > 128 {
		return false
	}
	for _, character := range token {
		if character != '-' && character != '_' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func PublicState(snapshot ShareSnapshot, viewer Viewer, unlocked bool, now time.Time) (*PublicShareState, error) {
	if snapshot.FileDeleted || snapshot.RevokedAt != nil || snapshot.ExpiresAt != nil && !snapshot.ExpiresAt.After(now) || snapshot.MaxDownloads > 0 && snapshot.DownloadCount >= snapshot.MaxDownloads {
		return nil, ErrNotFound
	}
	if snapshot.AccessMode == "authenticated" && viewer.UserID <= 0 || snapshot.AccessMode == "members" && viewer.UserID <= 0 {
		return &PublicShareState{State: "login_required", AccessMode: snapshot.AccessMode, SecretMode: snapshot.SecretMode}, nil
	}
	if snapshot.AccessMode == "members" && viewer.UserID != snapshot.OwnerUserID && !snapshot.Member {
		return nil, ErrNotFound
	}
	state := "available"
	if snapshot.SecretMode != "" && snapshot.SecretMode != "none" && viewer.UserID != snapshot.OwnerUserID && !unlocked {
		return &PublicShareState{State: "locked", AccessMode: snapshot.AccessMode, SecretMode: snapshot.SecretMode}, nil
	}
	result := &PublicShareState{State: state, File: &PublicFile{Name: snapshot.FileName, SizeBytes: snapshot.FileSize}, ExpiresAt: snapshot.ExpiresAt, MaxDownloads: snapshot.MaxDownloads, DownloadCount: snapshot.DownloadCount, SecretMode: snapshot.SecretMode, AccessMode: snapshot.AccessMode}
	if snapshot.MaxDownloads > 0 {
		remaining := snapshot.MaxDownloads - snapshot.DownloadCount
		result.RemainingDownloads = &remaining
	}
	return result, nil
}

func ParsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, ErrInvalid
	}
	return id, nil
}

func ParsePagination(cursorValue, limitValue string) (int64, int, error) {
	cursor, limit := int64(0), 20
	var err error
	if cursorValue != "" {
		cursor, err = ParsePositiveID(cursorValue)
		if err != nil {
			return 0, 0, err
		}
	}
	if limitValue != "" {
		limit, err = strconv.Atoi(limitValue)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, ErrInvalid
		}
	}
	return cursor, limit, nil
}

func SafeFilename(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 255 || strings.ContainsAny(value, "\x00\r\n/\\") {
		return "", ErrInvalid
	}
	return value, nil
}

func parseMemberIDs(values []string) ([]int64, error) {
	if len(values) > 100 {
		return nil, ErrInvalid
	}
	seen := make(map[int64]bool, len(values))
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := ParsePositiveID(value)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func ClassifyStorageError(err error) error {
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrRateLimited) {
		return err
	}
	return fmt.Errorf("%w: file storage failure", ErrDependency)
}
