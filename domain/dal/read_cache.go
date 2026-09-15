package dal

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils/cache"
	"github.com/sirupsen/logrus"
)

var readCaches atomic.Pointer[map[string]*cache.Client]
var bookInfoColumns = []string{"id", "title", "author", "publisher", "pubdate", "isbn13", "isbn10", "price", "pages", "image", "summary", "create_time", "update_time"}
var bookAddressColumns = []string{"id", "address", "short_name", "create_time", "update_time"}

// ConfigureReadCache replaces the complete namespace snapshot. Only disposable
// entity data is eligible; permissions, sessions and project control rows are not.
func ConfigureReadCache(client *cache.Client, namespaces []string) {
	next := make(map[string]*cache.Client)
	for _, namespace := range namespaces {
		switch namespace {
		case "book", "book_address", "web_release", "app_token":
			next[namespace] = client
		}
	}
	readCaches.Store(&next)
}

func ReadCache(namespace string) *cache.Client {
	current := readCaches.Load()
	if current == nil {
		return nil
	}
	return (*current)[namespace]
}

func invalidateReadCache(ctx context.Context, namespace string, ids []string) {
	client := ReadCache(namespace)
	if client == nil || len(ids) == 0 {
		return
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = client.Key(namespace, id)
	}
	if client.Delete(ctx, keys) != nil {
		// Redis errors may include a key; keep invalidation diagnostics namespace-only.
		logrus.WithField("namespace", namespace).Warn("read cache invalidation failed")
	}
}

// InvalidateBookInfoCache must run after a successful source write. Both ISBN
// aliases and the historical ISBN10-only query mode are independently evicted.
func InvalidateBookInfoCache(ctx context.Context, book *model.BookInfo) {
	if book == nil {
		return
	}
	ids := make([]string, 0, 24)
	// Collations may ignore case or trailing ASCII spaces. Over-evict these
	// spellings after writes without merging their independent read keys.
	for _, isbn := range []string{book.Isbn, book.Isbn10, strings.TrimRight(book.Isbn, " "), strings.TrimRight(book.Isbn10, " ")} {
		upper, lower := strings.ToUpper(isbn), strings.ToLower(isbn)
		ids = append(ids, "any:"+isbn, "any:"+upper, "any:"+lower,
			"isbn10:"+isbn, "isbn10:"+upper, "isbn10:"+lower)
	}
	invalidateReadCache(ctx, "book", ids)
}

func InvalidateBookAddressCache(ctx context.Context, ids ...int64) {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = strconv.FormatInt(id, 10)
	}
	invalidateReadCache(ctx, "book_address", keys)
}

func InvalidateWebReleaseCache(ctx context.Context, projectID, releaseID int64) {
	invalidateReadCache(ctx, "web_release", []string{fmt.Sprintf("%d:%d", projectID, releaseID)})
}

// Query only missing aliases in one batch. A value holds all matching rows so
// historical duplicate ISBNs are not silently dropped by an alias->single-row map.
func queryCachedBookInfo(isbns []string, isbn10Only bool) ([]*model.BookInfo, error) {
	for _, isbn := range isbns {
		if !cacheableBookISBN(isbn) {
			return nil, errors.New("noncanonical book alias requires direct source lookup")
		}
	}
	mode := "any:"
	if isbn10Only {
		mode = "isbn10:"
	}
	values, err := cache.MGet(context.Background(), ReadCache("book"), isbns,
		func(isbn string) string { return mode + isbn }, cache.Policy{Namespace: "book", TTL: 6 * time.Hour},
		func(ctx context.Context, missing []string) (map[string][]model.BookInfo, error) {
			query := db.MasterDB().WithContext(ctx).Table(BookInfoTable).Select(bookInfoColumns)
			if isbn10Only {
				query = query.Where("isbn10 IN ?", missing)
			} else {
				query = query.Where("isbn13 IN ? OR isbn10 IN ?", missing, missing)
			}
			var books []model.BookInfo
			if err := query.Find(&books).Error; err != nil {
				return nil, err
			}
			result := make(map[string][]model.BookInfo)
			for _, book := range books {
				// A row may match one requested alias exactly while also matching
				// another through collation. Do not cache any part of an uncertain batch.
				if (book.Isbn != "" && !cacheableBookISBN(book.Isbn)) || (book.Isbn10 != "" && !cacheableBookISBN(book.Isbn10)) {
					return nil, errors.New("noncanonical book alias requires direct source lookup")
				}
				matched := false
				for _, isbn := range missing {
					if book.Isbn10 == isbn || (!isbn10Only && book.Isbn == isbn) {
						result[isbn] = append(result[isbn], book)
						matched = true
					}
				}
				if !matched {
					// Collations may equate different spellings. Keep raw keys and
					// let this caller use its original query rather than guessing equivalence.
					return nil, errors.New("book cache alias requires direct source lookup")
				}
			}
			return result, nil // No negative entries: an absent book may still be fetched by RPC.
		})
	result := make([]*model.BookInfo, 0)
	seen := make(map[int64]bool)
	for _, isbn := range isbns {
		for _, book := range values[isbn] {
			if !seen[book.Id] {
				copy := book
				result, seen[book.Id] = append(result, &copy), true
			}
		}
	}
	// SQL batch reads have no requested-key ordering; keep one deterministic row
	// order across mixed cache/source results. Inventory ordering stays in its caller.
	sort.Slice(result, func(i, j int) bool { return result[i].Id < result[j].Id })
	return result, err
}

// Cache only canonical ASCII spellings. Legacy lengths, lowercase check digits
// and whitespace retain their SQL behavior without guessing collation equivalence.
func cacheableBookISBN(isbn string) bool {
	if len(isbn) != 10 && len(isbn) != 13 {
		return false
	}
	for i := 0; i < len(isbn); i++ {
		if isbn[i] >= '0' && isbn[i] <= '9' {
			continue
		}
		if len(isbn) == 10 && i == 9 && isbn[i] == 'X' {
			continue
		}
		return false
	}
	return true
}

func queryCachedBookAddresses(ids []int64) ([]*model.BookAddress, error) {
	values, err := cache.MGet(context.Background(), ReadCache("book_address"), ids,
		func(id int64) string { return strconv.FormatInt(id, 10) }, cache.Policy{Namespace: "book_address", TTL: 10 * time.Minute},
		func(ctx context.Context, missing []int64) (map[int64]model.BookAddress, error) {
			var rows []model.BookAddress
			err := db.MasterDB().WithContext(ctx).Table(BookAddressTable).Select(bookAddressColumns).Where("id IN ?", missing).Find(&rows).Error
			result := make(map[int64]model.BookAddress, len(rows))
			if err == nil {
				for _, row := range rows {
					result[row.Id] = row
				}
			}
			return result, err
		})
	result := make([]*model.BookAddress, 0, len(values))
	for _, value := range values {
		copy := value
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Id < result[j].Id })
	return result, err
}

// Explicit DTOs retain storage/ownership and immutable credential fields that
// the public model JSON intentionally omits. They are never API response types.
type releaseCacheDTO struct {
	ID             int64     `json:"id"`
	ProjectID      int64     `json:"project_id"`
	UploadedBy     int64     `json:"uploaded_by"`
	StorageKey     string    `json:"storage_key"`
	Status         uint8     `json:"status"`
	EntryFile      string    `json:"entry_file"`
	SHA256         string    `json:"sha256"`
	FileCount      int       `json:"file_count"`
	TotalBytes     int64     `json:"total_bytes"`
	IdempotencyKey *string   `json:"idempotency_key"`
	Extra          string    `json:"extra"`
	CreateTime     time.Time `json:"create_time"`
	UpdateTime     time.Time `json:"update_time"`
	CacheUntil     time.Time `json:"cache_until"`
}

func (value releaseCacheDTO) CacheExpiresAt() time.Time {
	if value.Status != uint8(model.WebProjectReleaseReady) {
		return time.Time{}
	}
	return value.CacheUntil
}

// QueryPublishedWebProjectRelease is exclusive to the published read path.
// Locking, management and cleanup DAL functions deliberately remain uncached.
func QueryPublishedWebProjectRelease(ctx context.Context, projectID, releaseID, ownerID int64) (*model.WebProjectRelease, error) {
	client := ReadCache("web_release")
	if client == nil {
		return QueryWebProjectRelease(projectID, releaseID, db.MasterDB().WithContext(ctx))
	}
	key := fmt.Sprintf("%d:%d", projectID, releaseID)
	values, err := cache.MGet(ctx, client, []string{key}, func(key string) string { return key },
		cache.Policy{Namespace: "web_release", TTL: 5 * time.Minute},
		func(loadCtx context.Context, _ []string) (map[string]releaseCacheDTO, error) {
			release, sourceErr := QueryWebProjectRelease(projectID, releaseID, db.MasterDB().WithContext(loadCtx))
			if sourceErr != nil || release == nil {
				return nil, sourceErr
			}
			value := releaseCacheDTO{ID: release.ID, ProjectID: release.ProjectID, UploadedBy: release.UploadedBy, StorageKey: release.StorageKey,
				Status: uint8(release.Status), EntryFile: release.EntryFile, SHA256: release.SHA256, FileCount: release.FileCount,
				TotalBytes: release.TotalBytes, IdempotencyKey: release.IdempotencyKey, Extra: release.Extra, CreateTime: release.CreateTime, UpdateTime: release.UpdateTime}
			if release.ID == releaseID && release.ProjectID == projectID && release.UploadedBy == ownerID && ownerID > 0 && release.StorageKey != "" {
				value.CacheUntil = time.Now().Add(5 * time.Minute)
			}
			return map[string]releaseCacheDTO{key: value}, nil
		})
	if err != nil {
		return QueryWebProjectRelease(projectID, releaseID, db.MasterDB().WithContext(ctx))
	}
	value, found := values[key]
	if !found {
		return nil, nil
	}
	if value.ID != releaseID || value.ProjectID != projectID || value.UploadedBy != ownerID || ownerID <= 0 || value.StorageKey == "" {
		InvalidateWebReleaseCache(ctx, projectID, releaseID)
		return QueryWebProjectRelease(projectID, releaseID, db.MasterDB().WithContext(ctx))
	}
	release := &model.WebProjectRelease{ID: value.ID, ProjectID: value.ProjectID, UploadedBy: value.UploadedBy, StorageKey: value.StorageKey,
		Status: model.WebProjectReleaseStatus(value.Status), EntryFile: value.EntryFile, SHA256: value.SHA256, FileCount: value.FileCount, TotalBytes: value.TotalBytes,
		IdempotencyKey: value.IdempotencyKey, Extra: value.Extra, CreateTime: value.CreateTime, UpdateTime: value.UpdateTime}
	if err := release.DecodeExtra(); err != nil {
		InvalidateWebReleaseCache(ctx, projectID, releaseID)
		return QueryWebProjectRelease(projectID, releaseID, db.MasterDB().WithContext(ctx))
	}
	return release, nil
}

type applicationTokenCacheDTO struct {
	ID                  int64     `json:"id"`
	ApplicationID       int64     `json:"application_id"`
	TokenDigest         []byte    `json:"token_digest"`
	SecretVersion       int64     `json:"secret_version"`
	ApplicationRevision int64     `json:"application_revision"`
	ScopeSnapshot       []string  `json:"scope_snapshot"`
	ExpiredAt           time.Time `json:"expired_at"`
	CreateTime          time.Time `json:"create_time"`
	CacheUntil          time.Time `json:"cache_until"`
}

func (value applicationTokenCacheDTO) CacheExpiresAt() time.Time {
	if value.ExpiredAt.Before(value.CacheUntil) {
		return value.ExpiredAt
	}
	return value.CacheUntil
}

// Only the issued token snapshot is cached. Authentication still fetches the
// current application, owner, revision, secret version and scopes in the service.
func QueryCachedApplicationTokenByDigest(ctx context.Context, digest []byte) (*model.ApplicationAccessToken, error) {
	client := ReadCache("app_token")
	if client == nil {
		return QueryApplicationTokenByDigest(db.MasterDB().WithContext(ctx), digest)
	}
	digest = append([]byte(nil), digest...)
	key := hex.EncodeToString(digest)
	values, err := cache.MGet(ctx, client, []string{key}, func(key string) string { return key },
		cache.Policy{Namespace: "app_token", TTL: 120 * time.Second},
		func(loadCtx context.Context, _ []string) (map[string]applicationTokenCacheDTO, error) {
			token, sourceErr := QueryApplicationTokenByDigest(db.MasterDB().WithContext(loadCtx), digest)
			if sourceErr != nil || token == nil {
				return nil, sourceErr
			}
			value := applicationTokenCacheDTO{ID: token.ID, ApplicationID: token.ApplicationID, TokenDigest: token.TokenDigest,
				SecretVersion: token.SecretVersion, ApplicationRevision: token.ApplicationRevision,
				ScopeSnapshot: token.ScopeSnapshot, ExpiredAt: token.ExpiredAt, CreateTime: token.CreateTime,
				CacheUntil: time.Now().Add(120 * time.Second)}
			return map[string]applicationTokenCacheDTO{key: value}, nil
		})
	if err != nil {
		return QueryApplicationTokenByDigest(db.MasterDB().WithContext(ctx), digest)
	}
	value, found := values[key]
	if !found {
		return nil, nil
	}
	if !bytes.Equal(value.TokenDigest, digest) || value.ID <= 0 || value.ApplicationID <= 0 {
		invalidateReadCache(ctx, "app_token", []string{key})
		return QueryApplicationTokenByDigest(db.MasterDB().WithContext(ctx), digest)
	}
	return &model.ApplicationAccessToken{ID: value.ID, ApplicationID: value.ApplicationID,
		TokenDigest: append([]byte(nil), value.TokenDigest...), SecretVersion: value.SecretVersion,
		ApplicationRevision: value.ApplicationRevision, ScopeSnapshot: append([]string(nil), value.ScopeSnapshot...),
		ExpiredAt: value.ExpiredAt, CreateTime: value.CreateTime}, nil
}
