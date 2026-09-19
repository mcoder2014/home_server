package fileshare

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"golang.org/x/crypto/bcrypt"
)

type shareAccess struct {
	Share  *model.FileShare
	File   *model.StoredFile
	Member bool
}

type UnlockGrant struct {
	UnlockedUntil time.Time `json:"unlocked_until"`
	CookieName    string    `json:"-"`
	CookieValue   string    `json:"-"`
}

func (application *Application) PublicState(ctx context.Context, token string, principal *utils.Principal, request *http.Request) (*service.PublicShareState, error) {
	access, err := application.resolveShare(ctx, token, principal)
	if err != nil {
		return nil, err
	}
	unlocked := application.grantAuthorized(request, access.Share, application.now())
	viewerID := int64(0)
	if principal != nil {
		viewerID = principal.UserID
	}
	return service.PublicState(snapshot(access), service.Viewer{UserID: viewerID}, unlocked, application.now())
}

func (application *Application) Unlock(ctx context.Context, token, secret, clientKey string, principal *utils.Principal) (*UnlockGrant, error) {
	access, err := application.resolveShare(ctx, token, principal)
	if err != nil {
		return nil, err
	}
	viewerID := int64(0)
	if principal != nil {
		viewerID = principal.UserID
	}
	state, err := service.PublicState(snapshot(access), service.Viewer{UserID: viewerID}, false, application.now())
	if err != nil {
		return nil, err
	}
	if state.State == "login_required" {
		return nil, service.ErrUnauthorized
	}
	expiresAt := application.now().Add(time.Hour)
	if access.Share.ExpiresAt != nil && access.Share.ExpiresAt.Before(expiresAt) {
		expiresAt = *access.Share.ExpiresAt
	}
	if state.State == "available" {
		return &UnlockGrant{UnlockedUntil: expiresAt}, nil
	}
	if !application.allowSecretAttempt(token, clientKey, application.now()) {
		return nil, service.ErrRateLimited
	}
	select {
	case application.bcryptGate <- struct{}{}:
		defer func() { <-application.bcryptGate }()
	default:
		return nil, service.ErrRateLimited
	}
	if bcrypt.CompareHashAndPassword([]byte(access.Share.SecretHash), []byte(secret)) != nil {
		return nil, apperrors.WithMessage(service.ErrForbidden, "分享口令错误")
	}
	application.clearSecretAttempts(token, clientKey)
	value, err := application.signGrant(access.Share, expiresAt)
	if err != nil {
		return nil, service.ErrDependency
	}
	return &UnlockGrant{UnlockedUntil: expiresAt, CookieName: shareCookieName(token), CookieValue: value}, nil
}

func (application *Application) resolveShare(ctx context.Context, token string, principal *utils.Principal) (*shareAccess, error) {
	if !service.ValidToken(token) {
		return nil, service.ErrNotFound
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	share, err := dal.FindShareByToken(database, token, false)
	if err != nil {
		return nil, service.ErrDependency
	}
	if share == nil || shareInvariant(share) != nil {
		return nil, service.ErrNotFound
	}
	file, err := dal.FindFile(database, share.FileID, false)
	if err != nil {
		return nil, service.ErrDependency
	}
	if file == nil || file.OwnerUserID != share.OwnerUserID || fileInvariant(file) != nil {
		return nil, service.ErrNotFound
	}
	active, err := ownerActive(ctx, share.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, service.ErrNotFound
	}
	member := false
	if principal != nil && principal.UserID > 0 && share.AccessMode == model.FileShareAccessMembers && principal.UserID != share.OwnerUserID {
		member, err = dal.IsFileShareMember(database, share.ID, principal.UserID, false)
		if err != nil {
			return nil, service.ErrDependency
		}
	}
	return &shareAccess{Share: share, File: file, Member: member}, nil
}

func snapshot(access *shareAccess) service.ShareSnapshot {
	return service.ShareSnapshot{AccessMode: access.Share.AccessMode.String(), SecretMode: access.Share.SecretMode.String(), ExpiresAt: access.Share.ExpiresAt, MaxDownloads: access.Share.MaxDownloads, DownloadCount: access.Share.DownloadCount, RevokedAt: access.Share.RevokedAt, FileDeleted: access.File.DeletedAt != nil, Member: access.Member, OwnerUserID: access.Share.OwnerUserID, FileName: access.File.OriginalName, FileSize: access.File.SizeBytes}
}

func (application *Application) allowSecretAttempt(token, clientKey string, now time.Time) bool {
	digest := sha256.Sum256([]byte(token + "\x00" + clientKey))
	key := string(digest[:])
	application.attempts.Lock()
	defer application.attempts.Unlock()
	entry := application.attempts.entries[key]
	if !entry.ExpiresAt.After(now) {
		if len(application.attempts.entries) >= 4096 {
			for existingKey, existing := range application.attempts.entries {
				if !existing.ExpiresAt.After(now) {
					delete(application.attempts.entries, existingKey)
				}
			}
		}
		if len(application.attempts.entries) >= 4096 {
			return false
		}
		entry = attemptEntry{ExpiresAt: now.Add(5 * time.Minute)}
	}
	if entry.Count >= 8 {
		return false
	}
	entry.Count++
	application.attempts.entries[key] = entry
	return true
}

func (application *Application) clearSecretAttempts(token, clientKey string) {
	digest := sha256.Sum256([]byte(token + "\x00" + clientKey))
	application.attempts.Lock()
	delete(application.attempts.entries, string(digest[:]))
	application.attempts.Unlock()
}

func shareCookieName(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "hs_fs_" + fmt.Sprintf("%x", digest[:8])
}

func (application *Application) signGrant(share *model.FileShare, expiresAt time.Time) (string, error) {
	if share == nil || expiresAt.IsZero() {
		return "", service.ErrDependency
	}
	tokenDigest := sha256.Sum256([]byte(share.Token))
	payload := strconv.FormatInt(share.ID, 10) + "." + base64.RawURLEncoding.EncodeToString(tokenDigest[:]) + "." + strconv.FormatInt(expiresAt.Unix(), 10)
	mac := hmac.New(sha256.New, application.cookieKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (application *Application) grantAuthorized(request *http.Request, share *model.FileShare, now time.Time) bool {
	if request == nil || share == nil {
		return false
	}
	cookie, err := request.Cookie(shareCookieName(share.Token))
	if err != nil || len(cookie.Value) > 512 {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, application.cookieKey)
	_, _ = mac.Write(payload)
	if subtle.ConstantTimeCompare(signature, mac.Sum(nil)) != 1 {
		return false
	}
	fields := strings.Split(string(payload), ".")
	if len(fields) != 3 || fields[0] != strconv.FormatInt(share.ID, 10) {
		return false
	}
	expectedDigest := sha256.Sum256([]byte(share.Token))
	if subtle.ConstantTimeCompare([]byte(fields[1]), []byte(base64.RawURLEncoding.EncodeToString(expectedDigest[:]))) != 1 {
		return false
	}
	expires, err := strconv.ParseInt(fields[2], 10, 64)
	return err == nil && time.Unix(expires, 0).After(now)
}
