package accounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mileusna/useragent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SessionWarningThreshold = 10

// SessionMetadata describes issuance, never the device presenting a token later.
// These values are display hints and are not used for authentication.
type SessionMetadata struct {
	LoginIP, UserAgent, ClientName, OSName, DeviceType, LoginSource string
}

// NewSessionMetadata parses a bounded UTF-8 UA using the pinned parser. Unknown
// clients may still log in; untrusted IP/source values get an explicit fallback.
func NewSessionMetadata(loginIP, rawUA, source string) SessionMetadata {
	metadata := SessionMetadata{LoginIP: "unknown", UserAgent: sessionText(rawUA, 1024), ClientName: "未知", OSName: "未知", DeviceType: "unknown", LoginSource: "unknown"}
	if ip := net.ParseIP(strings.TrimSpace(loginIP)); ip != nil {
		metadata.LoginIP = ip.String()
	}
	if source == "web" || source == "legacy" {
		metadata.LoginSource = source
	}
	parsed := useragent.Parse(metadata.UserAgent)
	if parsed.Name != "" && parsed.Name != parsed.String {
		metadata.ClientName = sessionText(strings.TrimSpace(parsed.Name+" "+parsed.Version), 128)
	}
	if parsed.OS != "" {
		metadata.OSName = sessionText(strings.TrimSpace(parsed.OS+" "+parsed.OSVersion), 128)
	}
	switch {
	case parsed.Bot:
		metadata.DeviceType = "bot"
	case parsed.Tablet:
		metadata.DeviceType = "tablet"
	case parsed.Mobile:
		metadata.DeviceType = "mobile"
	case parsed.Desktop:
		metadata.DeviceType = "desktop"
	}
	return metadata
}

func sessionText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

// sessionUsableAt matches activeSessionQuery, including the temporary password's
// deadline. A password-change session is usable only on explicitly limited routes.
func sessionUsableAt(user *model.UserAccount, session *model.AccountSession, now time.Time) bool {
	if user == nil || session == nil || user.Status != model.AccountActive || session.UserID != user.ID || len(session.TokenDigest) == 0 || session.IsExpired != 0 || session.AuthVersion != user.AuthVersion || !session.ExpireTime.After(now) {
		return false
	}
	if session.Purpose == model.SessionUser {
		return !user.MustChangePassword
	}
	return session.Purpose == model.SessionPasswordChange && user.PasswordExpiresAt != nil && user.PasswordExpiresAt.After(now)
}

// activeSessionQuery is shared by lists, page counts, user aggregates and the
// revoke-others selection. Neither expired rows nor unknown purposes count.
func activeSessionQuery(tx *gorm.DB, now time.Time) *gorm.DB {
	return tx.Table(dal.TableUserToken+" AS s").
		Joins("JOIN "+dal.AccountTable+" AS u ON u.id = s.user_id").
		Where("u.status = ? AND s.token_digest IS NOT NULL AND s.token_digest <> ? AND s.is_expired = 0 AND s.auth_version = u.auth_version AND s.expire_time > ? AND ((s.purpose = ? AND u.must_change_password = ?) OR (s.purpose = ? AND u.password_expires_at > ?))",
			model.AccountActive, []byte{}, now, model.SessionUser, false, model.SessionPasswordChange, now)
}

type sessionCount struct {
	UserID     int64
	Total      int64
	Restricted int64
}

// sessionCountsTx performs one aggregate for the users already read by the
// caller. Constant user/version pairs let the expiry index skip obsolete rows.
func sessionCountsTx(tx *gorm.DB, users []*model.UserAccount, now time.Time) ([]sessionCount, error) {
	var counts []sessionCount
	if len(users) == 0 {
		return counts, nil
	}
	pairs := make([]string, 0, len(users))
	args := make([]interface{}, 0, 2*len(users))
	for _, user := range users {
		pairs = append(pairs, "(s.user_id = ? AND s.auth_version = ?)")
		args = append(args, user.ID, user.AuthVersion)
	}
	err := activeSessionQuery(tx, now).Where("("+strings.Join(pairs, " OR ")+")", args...).
		Select("s.user_id, COUNT(*) AS total, SUM(s.purpose = ?) AS restricted", model.SessionPasswordChange).
		Group("s.user_id").Scan(&counts).Error
	return counts, err
}

// actingSessionTx bypasses request-level user snapshots. Writes always acquire
// the user lock before the acting session lock, as session issuance does.
func actingSessionTx(tx *gorm.DB, token string, lock, admin bool) (*model.UserAccount, *model.AccountSession, time.Time, error) {
	if len(token) < 16 || len(token) > 256 {
		return nil, nil, time.Time{}, apperrors.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	session, err := dal.QueryAccountSession(tx, digest[:])
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if session == nil {
		return nil, nil, time.Time{}, apperrors.ErrUnauthorized
	}
	user, err := dal.QueryAccount(tx, session.UserID, lock)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if lock {
		query := tx.Table(dal.TableUserToken).Select(dal.AccountSessionColumns).Where("id = ? AND token_digest = ?", session.ID, digest[:]).Clauses(clause.Locking{Strength: "UPDATE"})
		if err := query.Take(session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = apperrors.ErrUnauthorized
			}
			return nil, nil, time.Time{}, err
		}
	}
	now := time.Now()
	if !sessionUsableAt(user, session, now) {
		return nil, nil, now, apperrors.ErrUnauthorized
	}
	if user.MustChangePassword || session.Purpose != model.SessionUser {
		return nil, nil, now, apperrors.ErrForbidden
	}
	if admin && user.Role != model.RoleAdmin {
		return nil, nil, now, apperrors.ErrForbidden
	}
	return user, session, now, nil
}

type SessionFilter struct {
	Cursor string
	Limit  int
}

type sessionCursor struct {
	CreateTime time.Time `json:"create_time"`
	ID         int64     `json:"id,string"`
}

type SessionView struct {
	ID               string     `json:"id"`
	LoginIP          string     `json:"login_ip"`
	UserAgent        string     `json:"user_agent"`
	ClientName       string     `json:"client_name"`
	OSName           string     `json:"os_name"`
	DeviceType       string     `json:"device_type"`
	LoginSource      string     `json:"login_source"`
	Purpose          string     `json:"purpose"`
	AuthenticatedAt  *time.Time `json:"authenticated_at"`
	CreateTime       time.Time  `json:"create_time"`
	ExpireTime       time.Time  `json:"expire_time"`
	RemainingSeconds int64      `json:"remaining_seconds"`
}

type SessionPage struct {
	CurrentSession    *SessionView  `json:"current_session"`
	Items             []SessionView `json:"items"`
	TotalCount        int64         `json:"total_count"`
	RestrictedCount   int64         `json:"restricted_count"`
	MaxActiveSessions int           `json:"max_active_sessions"`
	ServerTime        time.Time     `json:"server_time"`
	HasMore           bool          `json:"has_more"`
	NextCursor        string        `json:"next_cursor"`
}

var sessionDisplayColumns = []string{"s.id", "s.login_ip", "s.user_agent", "s.client_name", "s.os_name", "s.device_type", "s.login_source", "s.purpose", "s.authenticated_at", "s.create_time", "s.expire_time"}

func sessionView(user *model.UserAccount, session *model.AccountSession, now time.Time) SessionView {
	expire := session.ExpireTime
	if session.Purpose == model.SessionPasswordChange && user.PasswordExpiresAt != nil && user.PasswordExpiresAt.Before(expire) {
		expire = *user.PasswordExpiresAt
	}
	value := SessionView{ID: strconv.FormatInt(session.ID, 10), LoginIP: session.LoginIP, UserAgent: sessionText(session.UserAgent, 1024), ClientName: sessionText(session.ClientName, 128), OSName: sessionText(session.OSName, 128), DeviceType: session.DeviceType, LoginSource: session.LoginSource, Purpose: session.Purpose, CreateTime: session.CreateTime, ExpireTime: expire, RemainingSeconds: int64(expire.Sub(now) / time.Second)}
	if !session.AuthenticatedAt.IsZero() {
		stamp := session.AuthenticatedAt
		value.AuthenticatedAt = &stamp
	}
	if value.LoginSource == "" {
		value.LoginSource = "unknown"
	}
	if value.DeviceType == "" {
		value.DeviceType = "unknown"
	}
	return value
}

// ListSessions returns the authenticated session separately and counts it in
// total_count. All rows and counts use one read-only repeatable-read snapshot.
func ListSessions(ctx context.Context, token string, filter SessionFilter) (*SessionPage, error) {
	if len(token) < 16 || len(token) > 256 {
		return nil, apperrors.ErrUnauthorized
	}
	return listSessionPage(ctx, token, 0, filter, false)
}

// AdminUserSessions reads every effective session for the target without giving
// administrators a per-session revoke capability.
func AdminUserSessions(ctx context.Context, token string, targetID int64, filter SessionFilter) (*SessionPage, error) {
	if targetID <= 0 {
		return nil, apperrors.ErrInvalid
	}
	return listSessionPage(ctx, token, targetID, filter, true)
}

// listSessionPage revalidates the acting token after middleware, rereads the
// target and computes current/items/counts using a single time and DB snapshot.
func listSessionPage(ctx context.Context, token string, targetID int64, filter SessionFilter, admin bool) (*SessionPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 512 {
		return nil, apperrors.ErrInvalid
	}
	var cursor sessionCursor
	if filter.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.ID == 0 || cursor.CreateTime.Year() < 1000 || cursor.CreateTime.Year() > 9999 {
			return nil, apperrors.ErrInvalid
		}
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	page := &SessionPage{Items: []SessionView{}, MaxActiveSessions: config.Runtime().AccountPolicy.MaxActiveSessions}
	err = database.Transaction(func(tx *gorm.DB) error {
		user, current, now, e := actingSessionTx(tx, token, false, admin)
		if e != nil {
			return e
		}
		page.ServerTime = now
		if admin {
			user, e = dal.QueryAccount(tx, targetID, false)
			if e != nil {
				return e
			}
			if user == nil {
				return apperrors.ErrNotFound
			}
		} else {
			value := sessionView(user, current, now)
			page.CurrentSession = &value
		}
		var counts struct{ TotalCount, RestrictedCount int64 }
		if e = activeSessionQuery(tx, now).Where("s.user_id = ? AND s.auth_version = ?", user.ID, user.AuthVersion).Select("COUNT(*) AS total_count, COALESCE(SUM(s.purpose = ?), 0) AS restricted_count", model.SessionPasswordChange).Scan(&counts).Error; e != nil {
			return e
		}
		page.TotalCount, page.RestrictedCount = counts.TotalCount, counts.RestrictedCount
		// Pin the version already read in this snapshot for the index prefix.
		query := activeSessionQuery(tx, now).Select(sessionDisplayColumns).Where("s.user_id = ? AND s.auth_version = ?", user.ID, user.AuthVersion)
		// Within the largest permitted capacity, range candidates skip expired
		// history. Larger legacy sets retain the paging index's LIMIT early exit.
		if counts.TotalCount <= 100 {
			query = query.Table(dal.TableUserToken + " AS s USE INDEX (idx_login_token_effective_range)")
		}
		if !admin {
			query = query.Where("s.id <> ?", current.ID)
		}
		if filter.Cursor != "" {
			query = query.Where("s.create_time < ? OR (s.create_time = ? AND s.id < ?)", cursor.CreateTime, cursor.CreateTime, cursor.ID)
		}
		var sessions []model.AccountSession
		if e = query.Order("s.create_time DESC, s.id DESC").Limit(filter.Limit + 1).Find(&sessions).Error; e != nil {
			return e
		}
		page.HasMore = len(sessions) > filter.Limit
		if page.HasMore {
			sessions = sessions[:filter.Limit]
			last := sessions[len(sessions)-1]
			raw, e := json.Marshal(sessionCursor{CreateTime: last.CreateTime, ID: last.ID})
			if e != nil {
				return e
			}
			page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		for _, session := range sessions {
			page.Items = append(page.Items, sessionView(user, &session, now))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return page, normalizeError(err)
}

type SessionRevokeResult struct {
	RevokedCount         int64 `json:"revoked_count"`
	AlreadyInactiveCount int64 `json:"already_inactive_count"`
}

// RevokeSessions validates the complete owned ID set before updating any row.
// Retries count already inactive owned records; invisible IDs reject the batch.
func RevokeSessions(ctx context.Context, token string, rawIDs []string) (*SessionRevokeResult, error) {
	if len(rawIDs) < 1 || len(rawIDs) > 100 {
		return nil, apperrors.ErrInvalid
	}
	unique := map[int64]bool{}
	for _, raw := range rawIDs {
		// Existing login_token IDs use a signed generator; stored negative IDs
		// are valid identities and must remain manageable without regeneration.
		digits := strings.TrimPrefix(raw, "-")
		if len(digits) == 0 || len(raw) > 20 || strings.Trim(digits, "0123456789") != "" {
			return nil, apperrors.ErrInvalid
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id == 0 {
			return nil, apperrors.ErrInvalid
		}
		unique[id] = true
	}
	ids := make([]int64, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	result := &SessionRevokeResult{}
	err = database.Transaction(func(tx *gorm.DB) error {
		user, acting, now, e := actingSessionTx(tx, token, true, false)
		if e != nil {
			return e
		}
		if unique[acting.ID] {
			return apperrors.WithMessage(apperrors.ErrInvalid, "当前会话请使用退出登录")
		}
		var sessions []model.AccountSession
		if e = tx.Table(dal.TableUserToken).Select(dal.AccountSessionColumns).Where("user_id = ? AND id IN ?", user.ID, ids).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&sessions).Error; e != nil {
			return e
		}
		if len(sessions) != len(ids) {
			return apperrors.ErrNotFound
		}
		activeIDs := make([]int64, 0, len(sessions))
		for _, session := range sessions {
			if sessionUsableAt(user, &session, now) {
				activeIDs = append(activeIDs, session.ID)
			} else {
				result.AlreadyInactiveCount++
			}
		}
		if len(activeIDs) > 0 {
			updated := tx.Table(dal.TableUserToken).Where("user_id = ? AND id IN ?", user.ID, activeIDs).Updates(map[string]interface{}{"is_expired": 1, "update_time": now})
			if updated.Error != nil {
				return updated.Error
			}
			result.RevokedCount = updated.RowsAffected
		}
		return nil
	})
	return result, normalizeError(err)
}

// RevokeOtherSessions shares the user's issuance lock, then locks effective
// other sessions in ID order. A login created after this transaction survives.
func RevokeOtherSessions(ctx context.Context, token string) (*SessionRevokeResult, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	result := &SessionRevokeResult{}
	err = database.Transaction(func(tx *gorm.DB) error {
		user, acting, now, e := actingSessionTx(tx, token, true, false)
		if e != nil {
			return e
		}
		var sessions []struct{ ID int64 }
		if e = activeSessionQuery(tx, now).Where("s.user_id = ? AND s.auth_version = ? AND s.id <> ?", user.ID, user.AuthVersion, acting.ID).Select("s.id").Order("s.id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&sessions).Error; e != nil {
			return e
		}
		ids := make([]int64, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.ID)
		}
		if len(ids) > 0 {
			updated := tx.Table(dal.TableUserToken).Where("user_id = ? AND id IN ?", user.ID, ids).Updates(map[string]interface{}{"is_expired": 1, "update_time": now})
			if updated.Error != nil {
				return updated.Error
			}
			result.RevokedCount = updated.RowsAffected
		}
		return nil
	})
	return result, normalizeError(err)
}
