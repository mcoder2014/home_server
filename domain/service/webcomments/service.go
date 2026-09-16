package webcomments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var threadColumns = []string{"id", "project_id", "release_id", "original_release_id", "page_key", "page_path", "original_page_key", "original_page_path", "anchor", "original_anchor", "status", "revision", "author_user_id", "author_application_id", "author_name_snapshot", "created_at", "updated_at"}
var eventColumns = []string{"id", "project_id", "thread_id", "sequence", "kind", "body", "anchor", "source_release_id", "page_key", "page_path", "actor_user_id", "actor_application_id", "actor_name_snapshot", "request_id", "payload_hash", "created_at"}

type Page struct {
	Items      []*Thread `json:"items"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}
type EventPage struct {
	Items        []*Event `json:"items"`
	HasMore      bool     `json:"has_more"`
	NextCursor   string   `json:"next_cursor"`
	NextSequence string   `json:"next_seq"`
}

func List(ctx context.Context, projectID int64, p *utils.Principal, status string, cursor int64, limit int, request string) (*Page, error) {
	if status != "" && status != "all" && status != "open" && status != "resolved" {
		return nil, apperrors.ErrInvalid
	}
	if request != "" && !requestIDPattern.MatchString(request) {
		return nil, apperrors.ErrInvalid
	}
	tx := db.MasterDB().WithContext(ctx)
	if _, err := Authorize(tx, projectID, p, false, true); err != nil {
		return nil, err
	}
	q := tx.Table("web_comment_thread").Select(threadColumns).Where("project_id = ?", projectID)
	if cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	if request != "" {
		var event Event
		e := tx.Table("web_comment_event").Select(eventColumns).Where("project_id = ? AND actor_user_id = ? AND actor_application_id = ? AND request_id = ?", projectID, p.UserID, p.ApplicationID, request).Take(&event).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return &Page{Items: []*Thread{}}, nil
		}
		if e != nil {
			return nil, apperrors.ErrDependency
		}
		q = q.Where("id = ?", event.ThreadID)
	}
	result := &Page{Items: []*Thread{}}
	if err := q.Order("id DESC").Limit(limit + 1).Find(&result.Items).Error; err != nil {
		return nil, apperrors.ErrDependency
	}
	result.HasMore = len(result.Items) > limit
	if result.HasMore {
		result.Items = result.Items[:limit]
		result.NextCursor = strconv.FormatInt(result.Items[limit-1].ID, 10)
	}
	return result, nil
}

func Detail(ctx context.Context, projectID, threadID int64, p *utils.Principal) (*Thread, error) {
	tx := db.MasterDB().WithContext(ctx)
	if _, err := Authorize(tx, projectID, p, false, true); err != nil {
		return nil, err
	}
	var thread Thread
	err := tx.Table("web_comment_thread").Select(threadColumns).Where("project_id = ? AND id = ?", projectID, threadID).Take(&thread).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	return &thread, nil
}

func Events(ctx context.Context, projectID, threadID int64, p *utils.Principal, cursor int64, limit int, requestID string) (*EventPage, error) {
	if _, err := Detail(ctx, projectID, threadID, p); err != nil {
		return nil, err
	}
	result := &EventPage{Items: []*Event{}}
	query := db.MasterDB().WithContext(ctx).Table("web_comment_event").Select(eventColumns).Where("project_id = ? AND thread_id = ? AND sequence > ?", projectID, threadID, cursor)
	if requestID != "" {
		if !requestIDPattern.MatchString(requestID) {
			return nil, apperrors.ErrInvalid
		}
		query = query.Where("actor_user_id = ? AND actor_application_id = ? AND request_id = ?", p.UserID, p.ApplicationID, requestID)
	}
	err := query.Order("sequence ASC").Limit(limit + 1).Find(&result.Items).Error
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	result.HasMore = len(result.Items) > limit
	if result.HasMore {
		result.Items = result.Items[:limit]
		result.NextCursor = strconv.FormatInt(result.Items[limit-1].Sequence, 10)
		result.NextSequence = result.NextCursor
	}
	if requestID == "" {
		for _, event := range result.Items {
			event.RequestID = ""
		}
	}
	return result, nil
}

// Mutate serializes writes per project, then thread. A durable request key and
// payload digest are committed with the event, so retries cannot duplicate or
// silently change feedback. Replies also advance revision to prevent resolving
// a thread while another participant adds unread feedback.
func Mutate(ctx context.Context, projectID, threadID, revision int64, p *utils.Principal, action string, input Input) (*Thread, error) {
	if err := Validate(input, action); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, apperrors.ErrUnauthorized
	}
	raw, _ := json.Marshal(struct {
		Action             string
		ThreadID, Revision int64
		Input              Input
	}{action, threadID, revision, input})
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var result Thread
	err := db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, e := Authorize(tx, projectID, p, true, false)
		if e != nil {
			return e
		}
		if project.ContainerMode != "enhanced" {
			return apperrors.ErrForbidden
		}
		var prior Event
		e = tx.Table("web_comment_event").Select(eventColumns).Where("project_id = ? AND actor_user_id = ? AND actor_application_id = ? AND request_id = ?", projectID, p.UserID, p.ApplicationID, input.RequestID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&prior).Error
		if e == nil {
			if prior.PayloadHash != hash {
				return apperrors.ErrConflict
			}
			if e = finalPrincipalCheck(tx, p); e != nil {
				return e
			}
			e = tx.Table("web_comment_thread").Select(threadColumns).Where("project_id = ? AND id = ?", projectID, prior.ThreadID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&result).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			if e != nil {
				return apperrors.ErrDependency
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return apperrors.ErrDependency
		}
		now := time.Now()
		actorName, e := actorNameSnapshot(tx, p)
		if e != nil {
			return e
		}
		if action == "comment" {
			rid, e := strconv.ParseInt(input.ReleaseID, 10, 64)
			if e != nil || rid <= 0 {
				return apperrors.ErrInvalid
			}
			if project.CurrentReleaseID == nil || rid != *project.CurrentReleaseID {
				return apperrors.WithMessage(apperrors.ErrConflict, "page_version_changed")
			}
			id, e := newID()
			if e != nil {
				return e
			}
			result = Thread{ID: id, ProjectID: projectID, ReleaseID: rid, OriginalReleaseID: rid, PageKey: input.PageKey, PagePath: input.PagePath, OriginalPageKey: input.PageKey, OriginalPagePath: input.PagePath, Anchor: input.Anchor, OriginalAnchor: input.Anchor, Status: "open", Revision: 1, AuthorUserID: p.UserID, AuthorApplicationID: p.ApplicationID, AuthorNameSnapshot: actorName, CreatedAt: now, UpdatedAt: now}
			if e = tx.Table("web_comment_thread").Create(&result).Error; e != nil {
				return apperrors.ErrDependency
			}
		} else {
			e = tx.Table("web_comment_thread").Select(threadColumns).Where("project_id = ? AND id = ?", projectID, threadID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&result).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			if e != nil {
				return apperrors.ErrDependency
			}
			if action != "reply" {
				if p.UserID != result.AuthorUserID && p.UserID != project.OwnerUserID {
					return apperrors.ErrForbidden
				}
				if revision != result.Revision {
					return apperrors.ErrConflict
				}
			}
			switch action {
			case "reply":
			case "resolve":
				if result.Status != "open" {
					return apperrors.ErrConflict
				}
				result.Status = "resolved"
			case "reopen":
				if result.Status != "resolved" {
					return apperrors.ErrConflict
				}
				result.Status = "open"
			case "reanchor":
				rid, e := strconv.ParseInt(input.ReleaseID, 10, 64)
				if e != nil || project.CurrentReleaseID == nil || rid != *project.CurrentReleaseID {
					return apperrors.ErrConflict
				}
				result.Anchor = input.Anchor
				result.ReleaseID = rid
				result.PageKey = input.PageKey
				result.PagePath = input.PagePath
			default:
				return apperrors.ErrInvalid
			}
			result.Revision++
			result.UpdatedAt = now
			fields := map[string]interface{}{"revision": result.Revision, "updated_at": result.UpdatedAt}
			if action == "resolve" || action == "reopen" {
				fields["status"] = result.Status
			}
			if action == "reanchor" {
				anchor, marshalErr := json.Marshal(result.Anchor)
				if marshalErr != nil {
					return apperrors.ErrInvalid
				}
				fields["release_id"] = result.ReleaseID
				fields["page_key"] = result.PageKey
				fields["page_path"] = result.PagePath
				fields["anchor"] = string(anchor)
			}
			if e = tx.Table("web_comment_thread").Where("project_id = ? AND id = ?", projectID, result.ID).Updates(fields).Error; e != nil {
				return apperrors.ErrDependency
			}
		}
		eid, e := newID()
		if e != nil {
			return e
		}
		sourceReleaseID := *project.CurrentReleaseID
		eventPageKey, eventPagePath := result.PageKey, result.PagePath
		eventAnchor := input.Anchor
		if action == "comment" || action == "reanchor" {
			sourceReleaseID = result.ReleaseID
			eventPageKey = input.PageKey
			eventPagePath = input.PagePath
		} else {
			if input.ReleaseID != "" {
				rid, parseErr := strconv.ParseInt(input.ReleaseID, 10, 64)
				if parseErr != nil || rid <= 0 {
					return apperrors.ErrInvalid
				}
				known := rid == result.ReleaseID || rid == result.OriginalReleaseID
				if !known {
					known, e = knownProjectRelease(tx, projectID, rid)
					if e != nil {
						return apperrors.ErrDependency
					}
				}
				if !known {
					return apperrors.WithMessage(apperrors.ErrConflict, "page_version_changed")
				}
				sourceReleaseID = rid
			}
			eventAnchor = result.Anchor
		}
		event := Event{ID: eid, ProjectID: projectID, ThreadID: result.ID, Sequence: result.Revision, Kind: action, Body: strings.TrimSpace(input.Body), Anchor: eventAnchor, SourceReleaseID: sourceReleaseID, PageKey: eventPageKey, PagePath: eventPagePath, ActorUserID: p.UserID, ActorApplicationID: p.ApplicationID, ActorNameSnapshot: actorName, RequestID: input.RequestID, PayloadHash: hash, CreatedAt: now}
		if e = finalPrincipalCheck(tx, p); e != nil {
			return e
		}
		if e = tx.Table("web_comment_event").Create(&event).Error; e != nil {
			return apperrors.ErrDependency
		}
		return nil
	})
	return &result, err
}

// A pruned release loses its file row, but comment metadata remains durable.
// Historical thread/event references therefore remain valid ownership proof.
func knownProjectRelease(tx *gorm.DB, projectID, releaseID int64) (bool, error) {
	queries := []struct {
		table string
		where string
		args  []interface{}
	}{
		{dal.WebProjectReleaseTable, "project_id = ? AND id = ?", []interface{}{projectID, releaseID}},
		{"web_comment_thread", "project_id = ? AND (release_id = ? OR original_release_id = ?)", []interface{}{projectID, releaseID, releaseID}},
		{"web_comment_event", "project_id = ? AND source_release_id = ?", []interface{}{projectID, releaseID}},
	}
	for _, query := range queries {
		var count int64
		if err := tx.Table(query.table).Where(query.where, query.args...).Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func finalPrincipalCheck(tx *gorm.DB, p *utils.Principal) error {
	if p == nil || p.TokenExpiresAt.IsZero() || !p.TokenExpiresAt.After(time.Now()) {
		return apperrors.ErrUnauthorized
	}
	if p.Kind == "application" {
		return accounts.RequireApplicationSnapshotTx(tx, p, "web-comments:write")
	}
	return nil
}

func actorNameSnapshot(tx *gorm.DB, p *utils.Principal) (string, error) {
	if p.Kind == "application" {
		var row struct{ Name string }
		if err := tx.Table(dal.ApplicationTable).Select("name").Where("id = ?", p.ApplicationID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return "", apperrors.ErrUnauthorized
		}
		return truncateRunes(row.Name, 256), nil
	}
	if !accounts.DatabaseMode() {
		return "用户 " + strconv.FormatInt(p.UserID, 10), nil
	}
	var row struct {
		Username    string
		DisplayName string
	}
	if err := tx.Table(dal.AccountTable).Select("username", "display_name").Where("id = ?", p.UserID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
		return "", apperrors.ErrUnauthorized
	}
	if row.DisplayName != "" {
		return truncateRunes(row.DisplayName, 256), nil
	}
	return truncateRunes(row.Username, 256), nil
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
