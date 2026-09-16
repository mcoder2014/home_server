package webcomments

import (
	"crypto/rand"
	"encoding/binary"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/mcoder2014/home_server/errors"
)

type Anchor struct {
	Kind     string `json:"kind"`
	TargetID string `json:"target_id,omitempty"`
	Exact    string `json:"exact,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
	Label    string `json:"label,omitempty"`
	PageID   string `json:"page_id,omitempty"`
}

type Thread struct {
	ID                  int64     `json:"id,string"`
	ProjectID           int64     `json:"project_id,string"`
	ReleaseID           int64     `json:"release_id,string"`
	OriginalReleaseID   int64     `json:"original_release_id,string"`
	PageKey             string    `json:"page_key"`
	PagePath            string    `json:"page_path"`
	OriginalPageKey     string    `json:"original_page_key"`
	OriginalPagePath    string    `json:"original_page_path"`
	Anchor              Anchor    `json:"anchor" gorm:"serializer:json"`
	OriginalAnchor      Anchor    `json:"original_anchor" gorm:"serializer:json"`
	Status              string    `json:"status"`
	Revision            int64     `json:"revision"`
	AuthorUserID        int64     `json:"author_user_id,string"`
	AuthorApplicationID int64     `json:"author_application_id,string"`
	AuthorNameSnapshot  string    `json:"author_name_snapshot"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Event struct {
	ID                 int64     `json:"id,string"`
	ProjectID          int64     `json:"project_id,string"`
	ThreadID           int64     `json:"thread_id,string"`
	Sequence           int64     `json:"sequence"`
	Kind               string    `json:"kind"`
	Body               string    `json:"body"`
	Anchor             Anchor    `json:"anchor" gorm:"serializer:json"`
	SourceReleaseID    int64     `json:"source_release_id,string"`
	PageKey            string    `json:"page_key"`
	PagePath           string    `json:"page_path"`
	ActorUserID        int64     `json:"actor_user_id,string"`
	ActorApplicationID int64     `json:"actor_application_id,string"`
	ActorNameSnapshot  string    `json:"actor_name_snapshot"`
	RequestID          string    `json:"request_id,omitempty"`
	PayloadHash        string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
}

type Input struct {
	RequestID string `json:"request_id"`
	ReleaseID string `json:"release_id"`
	PageKey   string `json:"page_key"`
	PagePath  string `json:"page_path"`
	Anchor    Anchor `json:"anchor"`
	Body      string `json:"body"`
}

var stableID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func Validate(input Input, action string) error {
	if !requestIDPattern.MatchString(input.RequestID) || !utf8.ValidString(input.Body) || utf8.RuneCountInString(input.Body) > 4000 {
		return apperrors.ErrInvalid
	}
	if (action == "comment" || action == "reply") && strings.TrimSpace(input.Body) == "" {
		return apperrors.ErrInvalid
	}
	if action != "comment" && action != "reanchor" {
		return nil
	}
	a := input.Anchor
	if len(a.TargetID) > 96 || len(a.PageID) > 96 || !utf8.ValidString(a.Exact+a.Prefix+a.Suffix+a.Label) ||
		utf8.RuneCountInString(a.Exact) > 4096 || utf8.RuneCountInString(a.Prefix) > 128 ||
		utf8.RuneCountInString(a.Suffix) > 128 || utf8.RuneCountInString(a.Label) > 256 {
		return apperrors.ErrInvalid
	}
	if (a.TargetID != "" && !stableID.MatchString(a.TargetID)) || (a.PageID != "" && !stableID.MatchString(a.PageID)) {
		return apperrors.ErrInvalid
	}
	switch a.Kind {
	case "text":
		if strings.TrimSpace(a.Exact) == "" {
			return apperrors.ErrInvalid
		}
	case "image", "module":
		if a.TargetID == "" {
			return apperrors.ErrInvalid
		}
	case "page":
	default:
		return apperrors.ErrInvalid
	}
	if input.PagePath == "" || len(input.PagePath) > 2048 || strings.ContainsAny(input.PagePath, "\\\x00?#") || strings.HasPrefix(input.PagePath, "/") || path.Clean(input.PagePath) != input.PagePath || strings.HasPrefix(input.PagePath, "../") {
		return apperrors.ErrInvalid
	}
	if len(input.PageKey) > 256 || (input.PageKey != "path:"+input.PagePath && (a.PageID == "" || input.PageKey != "id:"+a.PageID)) {
		return apperrors.ErrInvalid
	}
	return nil
}

func newID() (int64, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0, apperrors.ErrDependency
	}
	return int64(binary.BigEndian.Uint64(value[:]) & 0x7fffffffffffffff), nil
}
