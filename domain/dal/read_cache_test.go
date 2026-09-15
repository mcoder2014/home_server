package dal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The fixture refuses every database outside its dedicated test prefix and
// every Redis endpoint except the explicitly isolated port. It never flushes Redis.
func readCacheFixture(t *testing.T) (*gorm.DB, *redis.Client, *cache.Client) {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_READ_CACHE_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_READ_CACHE_TEST_DSN is required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(parsed.DBName, "home_server_readcache_test_"), "refuse non-fixture database")
	require.NoError(t, db.InitDatabase(dsn))
	database := db.MasterDB()
	var actual string
	require.NoError(t, database.Raw("SELECT DATABASE()").Scan(&actual).Error)
	require.Equal(t, parsed.DBName, actual)
	for _, ddl := range []string{
		"CREATE TABLE IF NOT EXISTS bookinfo (id BIGINT PRIMARY KEY AUTO_INCREMENT,title VARCHAR(512),author VARCHAR(256),publisher VARCHAR(256),pubdate VARCHAR(64),isbn13 VARCHAR(32),isbn10 VARCHAR(32),price VARCHAR(128),pages INT,image VARCHAR(4096),summary TEXT,create_time DATETIME(6),update_time DATETIME(6),KEY idx_isbn13(isbn13),KEY idx_isbn10(isbn10))",
		"CREATE TABLE IF NOT EXISTS book_address (id BIGINT PRIMARY KEY AUTO_INCREMENT,address VARCHAR(512),short_name VARCHAR(256),create_time DATETIME(6),update_time DATETIME(6))",
		"CREATE TABLE IF NOT EXISTS web_project_release (id BIGINT PRIMARY KEY,project_id BIGINT,uploaded_by BIGINT,storage_key VARCHAR(512),status TINYINT,entry_file VARCHAR(2048),sha256 VARCHAR(64),file_count INT,total_bytes BIGINT,idempotency_key VARCHAR(256),extra TEXT,create_time DATETIME(6),update_time DATETIME(6),KEY idx_project(project_id,id))",
		"CREATE TABLE IF NOT EXISTS application_access_token (id BIGINT PRIMARY KEY AUTO_INCREMENT,application_id BIGINT,token_digest BINARY(32),secret_version BIGINT,application_revision BIGINT,scope_snapshot TEXT,expired_at DATETIME(6),create_time DATETIME(6),UNIQUE KEY uk_digest(token_digest))",
	} {
		require.NoError(t, database.Exec(ddl).Error)
	}
	for _, table := range []string{BookInfoTable, BookAddressTable, WebProjectReleaseTable, ApplicationTokenTable} {
		require.NoError(t, database.Exec("TRUNCATE TABLE "+table).Error)
	}
	address := os.Getenv("HOME_SERVER_TEST_REDIS_ADDR")
	host, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", host)
	require.Equal(t, "16389", port)
	raw := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1, ContextTimeoutEnabled: true})
	require.NoError(t, raw.Ping(context.Background()).Err())
	prefix := fmt.Sprintf("hs:test:bindings:%d", time.Now().UnixNano())
	client := cache.New(raw, prefix)
	ConfigureReadCache(client, []string{"book", "book_address", "web_release", "app_token"})
	t.Cleanup(func() {
		ConfigureReadCache(nil, nil)
		// Some tests close the read client to inject an outage; cleanup uses its
		// own connection and still touches only this test's unique prefix.
		cleanup := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
		keys, scanErr := cleanup.Keys(context.Background(), prefix+":*").Result()
		if scanErr == nil && len(keys) > 0 {
			require.NoError(t, cleanup.Del(context.Background(), keys...).Err())
		}
		_ = cleanup.Close()
		_ = raw.Close()
	})
	return database, raw, client
}

func TestReadCacheNamespaceSelection(t *testing.T) {
	client := cache.New(nil, "hs:test:bindings:config")
	namespaces := []string{"book"}
	ConfigureReadCache(client, namespaces)
	t.Cleanup(func() { ConfigureReadCache(nil, nil) })
	namespaces[0] = "app_token"
	require.Same(t, client, ReadCache("book"))
	require.Nil(t, ReadCache("app_token"))
	ConfigureReadCache(client, []string{})
	require.Nil(t, ReadCache("book"), "explicit empty namespaces enable nothing")
	ConfigureReadCache(client, nil)
	require.Nil(t, ReadCache("book"), "nil namespaces enable nothing")
	ConfigureReadCache(nil, []string{"book"})
	require.Nil(t, ReadCache("book"))
}

func TestReadCacheBookAliasesMissesAndInvalidation(t *testing.T) {
	database, raw, client := readCacheFixture(t)
	book := &model.BookInfo{Id: 10, Title: "original", Isbn: "9781234567890", Isbn10: "1234567890", PubDate: "2020-01-01"}
	require.NoError(t, InsertBookInfo(book))
	first, err := QueryBookInfoByIsbn(book.Isbn)
	require.NoError(t, err)
	require.Equal(t, book.Id, first.Id)
	first.Title = "caller mutated"
	second, err := QueryBookInfoByIsbn(book.Isbn)
	require.NoError(t, err)
	require.Equal(t, "original", second.Title)
	books, err := BatchQueryBookInfoByIsbn([]string{book.Isbn10, book.Isbn, book.Isbn10, "missing"})
	require.NoError(t, err)
	require.Len(t, books, 1, "ISBN aliases and repeated inputs cannot duplicate rows")
	by10, err := QueryBookInfoByIsbn10(book.Isbn10)
	require.NoError(t, err)
	require.Equal(t, book.Id, by10.Id)
	by10, err = QueryBookInfoByIsbn10(book.Isbn)
	require.NoError(t, err)
	require.Nil(t, by10, "ISBN10-only lookup must not reuse the OR lookup cache")
	_, err = QueryBookInfoByIsbn("123456789")
	require.NoError(t, err)
	require.Greater(t, client.Stats()["book"].Hit, uint64(0))
	require.NoError(t, DeleteBookInfoById(book.Id))
	for _, isbn := range []string{book.Isbn, book.Isbn10} {
		value, err := QueryBookInfoByIsbn(isbn)
		require.NoError(t, err)
		require.Nil(t, value)
	}
	value, err := QueryBookInfoByIsbn10(book.Isbn10)
	require.NoError(t, err)
	require.Nil(t, value)
	book.Title = "insert after a miss"
	require.NoError(t, InsertBookInfo(book))
	value, err = QueryBookInfoByIsbn(book.Isbn)
	require.NoError(t, err)
	require.Equal(t, book.Title, value.Title)
	require.NoError(t, raw.Close())
	ConfigureReadCache(cache.New(raw, "hs:test:bindings:closed"), []string{"book"})
	require.NoError(t, database.Table(BookInfoTable).Where("id = ?", book.Id).Update("title", "source fallback").Error)
	value, err = QueryBookInfoByIsbn(book.Isbn)
	require.NoError(t, err)
	require.Equal(t, "source fallback", value.Title)
	require.NoError(t, DeleteBookInfoById(book.Id), "Redis delete failure cannot undo DB deletion")
}

func TestReadCacheBookLengthAndBatchCompatibility(t *testing.T) {
	database, _, _ := readCacheFixture(t)
	book := model.BookInfo{Id: 20, Isbn: "12345678901", Isbn10: "ABCDEFGHIJK", Title: "legacy eleven characters"}
	require.NoError(t, database.Table(BookInfoTable).Create(&book).Error)
	for _, isbn := range []string{book.Isbn, book.Isbn10} {
		value, err := QueryBookInfoByIsbn(isbn)
		require.NoError(t, err)
		require.NotNil(t, value, "existing 10..13 character single lookup is preserved")
	}
	value, err := QueryBookInfoByIsbn10(book.Isbn10)
	require.NoError(t, err)
	require.NotNil(t, value, "ISBN10-specific function historically permits >=10 characters")
	long := model.BookInfo{Id: 21, Isbn: "12345678901234", Title: "batch allows historical long input"}
	require.NoError(t, database.Table(BookInfoTable).Create(&long).Error)
	value, err = QueryBookInfoByIsbn(long.Isbn)
	require.NoError(t, err)
	require.Nil(t, value)
	list, err := BatchQueryBookInfoByIsbn([]string{long.Isbn})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestReadCacheBookISBNCheckDigitCase(t *testing.T) {
	database, _, _ := readCacheFixture(t)
	require.NoError(t, database.Exec("ALTER TABLE bookinfo MODIFY isbn10 VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin").Error)
	book := model.BookInfo{Id: 22, Isbn: "9781234567890", Isbn10: "123456789X", Title: "uppercase check digit"}
	require.NoError(t, database.Table(BookInfoTable).Create(&book).Error)
	value, err := QueryBookInfoByIsbn(book.Isbn10)
	require.NoError(t, err)
	require.NotNil(t, value)
	value, err = QueryBookInfoByIsbn("123456789x")
	require.NoError(t, err)
	require.Nil(t, value, "binary collation queries must not share uppercase/lowercase cache keys")
	require.NoError(t, database.Exec("ALTER TABLE bookinfo MODIFY isbn10 VARCHAR(32) CHARACTER SET ascii COLLATE ascii_general_ci").Error)
	value, err = QueryBookInfoByIsbn("123456789x")
	require.NoError(t, err)
	require.NotNil(t, value, "source collation matches must survive unmappable cache aliases")
	value, err = QueryBookInfoByIsbn10("123456789x")
	require.NoError(t, err)
	require.NotNil(t, value)
	list, err := BatchQueryBookInfoByIsbn([]string{"123456789x", "no-matching-row"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NoError(t, DeleteBookInfoById(book.Id))
	value, err = QueryBookInfoByIsbn("123456789x")
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestReadCacheBookDuplicateSingleKeepsSourceSelection(t *testing.T) {
	database, _, _ := readCacheFixture(t)
	for _, id := range []int64{23, 24} {
		require.NoError(t, database.Table(BookInfoTable).Create(&model.BookInfo{Id: id, Isbn: "9781234567890", Isbn10: "1234567890", Title: "before"}).Error)
	}
	list, err := BatchQueryBookInfoByIsbn([]string{"9781234567890", "1234567890"})
	require.NoError(t, err)
	require.Len(t, list, 2, "distinct rows sharing an ISBN are preserved")
	require.NoError(t, database.Table(BookInfoTable).Where("id IN ?", []int64{23, 24}).Update("title", "source selected").Error)
	value, err := QueryBookInfoByIsbn("9781234567890")
	require.NoError(t, err)
	require.Equal(t, "source selected", value.Title, "multiple matches retain the original Take query rather than selecting from cache order")
	value, err = QueryBookInfoByIsbn10("1234567890")
	require.NoError(t, err)
	require.Equal(t, "source selected", value.Title)
}

func TestReadCacheBookAmbiguousAliasBatch(t *testing.T) {
	for _, scenario := range []struct {
		name, secondAlias string
		requested         []string
	}{
		{"case variant keys", "123456789x", []string{"123456789X", "123456789x"}},
		{"alternate exact alias masks case variant", "123456789x", []string{"123456789X", "9781234567892"}},
		{"alternate exact alias masks trailing space", "123456789X ", []string{"123456789X", "9781234567892"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database, raw, client := readCacheFixture(t)
			require.NoError(t, database.Exec("ALTER TABLE bookinfo MODIFY isbn10 VARCHAR(32) CHARACTER SET ascii COLLATE ascii_general_ci").Error)
			rows := []model.BookInfo{
				{Id: 71, Isbn: "9781234567891", Isbn10: "123456789X", Title: "before"},
				{Id: 72, Isbn: "9781234567892", Isbn10: scenario.secondAlias, Title: "before"},
			}
			require.NoError(t, database.Table(BookInfoTable).Create(&rows).Error)
			cold, err := BatchQueryBookInfoByIsbn(scenario.requested)
			require.NoError(t, err)
			require.Len(t, cold, 2)
			hot, err := BatchQueryBookInfoByIsbn([]string{"123456789X"})
			require.NoError(t, err)
			require.Len(t, hot, 2, "each hot key must retain its complete SQL result, including equivalent spellings")
			require.NoError(t, database.Table(BookInfoTable).Where("id IN ?", []int64{71, 72}).Update("title", "source selected").Error)
			single, err := QueryBookInfoByIsbn("123456789X")
			require.NoError(t, err)
			require.Equal(t, "source selected", single.Title)
			single, err = QueryBookInfoByIsbn10("123456789X")
			require.NoError(t, err)
			require.Equal(t, "source selected", single.Title)
			keys, err := raw.Keys(context.Background(), client.Key("book", "*")).Result()
			require.NoError(t, err)
			require.Empty(t, keys, "uncertain alias batches must not populate partial per-key caches")
		})
	}
}

func TestReadCacheBookCaseVariantInvalidation(t *testing.T) {
	for _, isbn10 := range []string{"123456789X", "123456789x"} {
		t.Run(isbn10, func(t *testing.T) {
			_, raw, client := readCacheFixture(t)
			ctx := context.Background()
			keys := []string{client.Key("book", "any:123456789X"), client.Key("book", "any:123456789x"), client.Key("book", "isbn10:123456789X"), client.Key("book", "isbn10:123456789x")}
			for _, key := range keys {
				require.NoError(t, raw.Set(ctx, key, "historical alias cache", time.Minute).Err())
			}
			book := &model.BookInfo{Id: 73, Isbn: "9781234567893", Isbn10: isbn10, Title: "inserted"}
			require.NoError(t, InsertBookInfo(book))
			require.Equal(t, int64(0), raw.Exists(ctx, keys...).Val(), "insert must evict both safe ISBN10 spellings and query modes")
			for _, key := range keys {
				require.NoError(t, raw.Set(ctx, key, "historical alias cache", time.Minute).Err())
			}
			require.NoError(t, DeleteBookInfoById(book.Id))
			require.Equal(t, int64(0), raw.Exists(ctx, keys...).Val(), "delete must evict both safe ISBN10 spellings and query modes")
		})
	}
}

func TestReadCacheBookPaddedAliasWriteInvalidation(t *testing.T) {
	for _, column := range []string{"isbn10", "isbn13"} {
		t.Run(column, func(t *testing.T) {
			database, raw, client := readCacheFixture(t)
			require.NoError(t, database.Exec("ALTER TABLE bookinfo MODIFY "+column+" VARCHAR(32) CHARACTER SET ascii COLLATE ascii_general_ci").Error)
			original := &model.BookInfo{Id: 74, Isbn: "9781234567894", Isbn10: "123456789X", Title: "original"}
			require.NoError(t, InsertBookInfo(original))
			canonical := original.Isbn10
			if column == "isbn13" {
				canonical = original.Isbn
			}
			warm, err := BatchQueryBookInfoByIsbn([]string{canonical})
			require.NoError(t, err)
			require.Len(t, warm, 1)
			key := client.Key("book", "any:"+canonical)
			require.Equal(t, int64(1), raw.Exists(context.Background(), key).Val(), "canonical snapshot must be hot before insertion")
			padded := &model.BookInfo{Id: 75, Isbn: "9781234567895", Isbn10: canonical + " ", Title: "padded"}
			if column == "isbn13" {
				padded.Isbn, padded.Isbn10 = canonical+" ", "1234567895"
			}
			require.NoError(t, InsertBookInfo(padded))
			fresh, err := BatchQueryBookInfoByIsbn([]string{canonical})
			require.NoError(t, err)
			require.Len(t, fresh, 2, "PAD SPACE insertion must evict the previously hot canonical key")
			// A previously stored canonical alias must also be evicted after
			// deleting its padded source row, even though such rows now bypass caching.
			require.NoError(t, raw.Set(context.Background(), key, "historical canonical alias", time.Minute).Err())
			require.NoError(t, DeleteBookInfoById(padded.Id))
			require.Equal(t, int64(0), raw.Exists(context.Background(), key).Val())
			fresh, err = BatchQueryBookInfoByIsbn([]string{canonical})
			require.NoError(t, err)
			require.Len(t, fresh, 1)
		})
	}
}

func TestReadCacheAddressTransactionAndDelete(t *testing.T) {
	database, _, client := readCacheFixture(t)
	missing, err := QueryBookAddressById(30)
	require.NoError(t, err)
	require.Nil(t, missing)
	tx := database.Begin()
	require.NoError(t, tx.Error)
	address := &model.BookAddress{Id: 30, Address: "room 1", ShortName: "R1"}
	_, err = InsertBookAddress(address, tx)
	require.NoError(t, err)
	missing, err = QueryBookAddressById(address.Id)
	require.NoError(t, err)
	require.Nil(t, missing, "an uncommitted insert must not leak through the cache")
	require.NoError(t, tx.Commit().Error)
	value, err := QueryBookAddressById(address.Id)
	require.NoError(t, err)
	require.Equal(t, "room 1", value.Address)
	value.Address = "caller mutation"
	list, err := BatchQueryBookAddress([]int64{30, 30, 999})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "room 1", list[0].Address)
	require.Greater(t, client.Stats()["book_address"].Hit, uint64(0))
	require.NoError(t, DeleteBookAddress(address.Id))
	value, err = QueryBookAddressById(address.Id)
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestReadCacheReleaseDTOKeepsPrivateFieldsAndDirectReads(t *testing.T) {
	database, raw, client := readCacheFixture(t)
	idempotency := "upload-fixture"
	release := model.WebProjectRelease{ID: 41, ProjectID: 40, UploadedBy: 400, StorageKey: "400/upload/html/40/releases/41/content", Status: model.WebProjectReleaseReady, EntryFile: "index.html", SHA256: strings.Repeat("a", 64), FileCount: 3, TotalBytes: 99, Extra: `{"other":1}`, IdempotencyKey: &idempotency, CreateTime: time.Now(), UpdateTime: time.Now()}
	require.NoError(t, database.Table(WebProjectReleaseTable).Create(&release).Error)
	for i := 0; i < 2; i++ {
		value, err := QueryPublishedWebProjectRelease(context.Background(), 40, 41, 400)
		require.NoError(t, err)
		require.Equal(t, release.StorageKey, value.StorageKey)
		require.Equal(t, release.UploadedBy, value.UploadedBy)
		require.Equal(t, release.Extra, value.Extra)
		require.Equal(t, idempotency, *value.IdempotencyKey)
		*value.IdempotencyKey = "caller mutation"
	}
	require.Greater(t, client.Stats()["web_release"].Hit, uint64(0))
	key := client.Key("web_release", "40:41")
	alterReadCacheValue(t, raw, key, "uploaded_by", 999)
	value, err := QueryPublishedWebProjectRelease(context.Background(), 40, 41, 400)
	require.NoError(t, err)
	require.Equal(t, int64(400), value.UploadedBy, "wrong cached ownership must reload the source")
	require.NoError(t, database.Table(WebProjectReleaseTable).Where("id = ?", 41).Update("status", model.WebProjectReleaseDeleting).Error)
	direct, err := QueryWebProjectRelease(40, 41)
	require.NoError(t, err)
	require.Equal(t, model.WebProjectReleaseDeleting, direct.Status, "management/cleanup reads must be live")
	InvalidateWebReleaseCache(context.Background(), 40, 41)
	value, err = QueryPublishedWebProjectRelease(context.Background(), 40, 41, 400)
	require.NoError(t, err)
	require.Equal(t, model.WebProjectReleaseDeleting, value.Status)
	keys, err := raw.Keys(context.Background(), client.Key("web_release", "*")).Result()
	require.NoError(t, err)
	require.Empty(t, keys, "non-ready releases must not be cached")
}

func TestReadCacheTokenSnapshotExpiryAndCopy(t *testing.T) {
	database, raw, client := readCacheFixture(t)
	digest := sha256.Sum256([]byte("local-fixture-only"))
	token := model.ApplicationAccessToken{ID: 51, ApplicationID: 50, TokenDigest: digest[:], SecretVersion: 2, ApplicationRevision: 3, ScopeSnapshot: []string{"web-projects:read"}, ExpiredAt: time.Now().Add(time.Hour), CreateTime: time.Now()}
	require.NoError(t, CreateApplicationToken(database, &token))
	for i := 0; i < 2; i++ {
		value, err := QueryCachedApplicationTokenByDigest(context.Background(), digest[:])
		require.NoError(t, err)
		require.Equal(t, int64(50), value.ApplicationID)
		require.Equal(t, int64(2), value.SecretVersion)
		require.Equal(t, int64(3), value.ApplicationRevision)
		require.Equal(t, token.ScopeSnapshot, value.ScopeSnapshot)
		require.Equal(t, digest[:], value.TokenDigest)
		value.ScopeSnapshot[0] = "caller mutation"
		value.TokenDigest[0] = 0
	}
	key := client.Key("app_token", hex.EncodeToString(digest[:]))
	ttl, err := raw.PTTL(context.Background(), key).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, 120*time.Second)
	alterReadCacheValue(t, raw, key, "token_digest", []byte("wrong-digest"))
	correct, err := QueryCachedApplicationTokenByDigest(context.Background(), digest[:])
	require.NoError(t, err)
	require.Equal(t, digest[:], correct.TokenDigest, "a cached row for another digest is never used as identity")
	shortDigest := sha256.Sum256([]byte("short-lived-fixture"))
	token.ID = 52
	token.TokenDigest = shortDigest[:]
	token.ExpiredAt = time.Now().Add(2 * time.Second)
	require.NoError(t, CreateApplicationToken(database, &token))
	_, err = QueryCachedApplicationTokenByDigest(context.Background(), shortDigest[:])
	require.NoError(t, err)
	ttl, err = raw.PTTL(context.Background(), client.Key("app_token", hex.EncodeToString(shortDigest[:]))).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, 2*time.Second)
	expired := sha256.Sum256([]byte("expired-fixture"))
	token.ID = 53
	token.TokenDigest = expired[:]
	token.ExpiredAt = time.Now().Add(-time.Second)
	require.NoError(t, CreateApplicationToken(database, &token))
	value, err := QueryCachedApplicationTokenByDigest(context.Background(), expired[:])
	require.NoError(t, err)
	require.NotNil(t, value, "the service still owns expiry authorization")
	require.Equal(t, int64(0), raw.Exists(context.Background(), client.Key("app_token", hex.EncodeToString(expired[:]))).Val())
}

func alterReadCacheValue(t *testing.T, raw *redis.Client, key, field string, value interface{}) {
	t.Helper()
	data, err := raw.Get(context.Background(), key).Bytes()
	require.NoError(t, err)
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &envelope))
	var snapshot map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope["value"], &snapshot))
	snapshot[field], err = json.Marshal(value)
	require.NoError(t, err)
	envelope["value"], err = json.Marshal(snapshot)
	require.NoError(t, err)
	data, err = json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, raw.Set(context.Background(), key, data, time.Minute).Err())
}
