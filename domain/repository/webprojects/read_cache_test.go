package webprojects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestReadCacheRepositoryChanges verifies live project control reads and cache
// invalidation only after release mutations commit. It uses unique rows in the
// explicitly named fixture database and deletes only its own Redis prefix.
func TestReadCacheRepositoryChanges(t *testing.T) {
	dsn := os.Getenv("HOME_SERVER_READ_CACHE_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_READ_CACHE_TEST_DSN is required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(parsed.DBName, "home_server_readcache_test_"))
	require.NoError(t, db.InitDatabase(dsn))
	database := db.MasterDB()
	var actual string
	require.NoError(t, database.Raw("SELECT DATABASE()").Scan(&actual).Error)
	require.Equal(t, parsed.DBName, actual)
	require.NoError(t, database.Exec("CREATE TABLE IF NOT EXISTS web_project (id BIGINT PRIMARY KEY,container_mode VARCHAR(16) NOT NULL DEFAULT 'raw',owner_user_id BIGINT,name VARCHAR(256),description TEXT,slug VARCHAR(256),access_mode TINYINT,status TINYINT,current_release_id BIGINT,revision BIGINT,client_request_id VARCHAR(256),deleted_at DATETIME(6),create_time DATETIME(6),update_time DATETIME(6),UNIQUE KEY uk_slug(slug))").Error)
	require.NoError(t, database.Exec("CREATE TABLE IF NOT EXISTS web_project_release (id BIGINT PRIMARY KEY,project_id BIGINT,uploaded_by BIGINT,storage_key VARCHAR(512),status TINYINT,entry_file VARCHAR(2048),sha256 VARCHAR(64),file_count INT,total_bytes BIGINT,idempotency_key VARCHAR(256),extra TEXT,create_time DATETIME(6),update_time DATETIME(6),KEY idx_project(project_id,id))").Error)
	require.Equal(t, "127.0.0.1:16389", os.Getenv("HOME_SERVER_TEST_REDIS_ADDR"))
	raw := redis.NewClient(&redis.Options{Addr: "127.0.0.1:16389", MaxRetries: -1, ContextTimeoutEnabled: true})
	ctx := context.Background()
	require.NoError(t, raw.Ping(ctx).Err())
	prefix := fmt.Sprintf("hs:test:bindings:repository:%d", time.Now().UnixNano())
	client := cache.New(raw, prefix)
	dal.ConfigureReadCache(client, []string{"web_release"})
	id := time.Now().UnixNano() / 1000
	releaseID := id + 1
	project := &model.WebProject{ID: id, OwnerUserID: id, Slug: fmt.Sprintf("cache-fixture-%d", id), Name: "fixture", AccessMode: model.WebProjectAccessPublic, Status: model.WebProjectStatusEnabled, CurrentReleaseID: &releaseID, Revision: 1}
	release := &model.WebProjectRelease{ID: releaseID, ProjectID: id, UploadedBy: id, StorageKey: "fixture/content", Status: model.WebProjectReleaseReady, EntryFile: "index.html", SHA256: strings.Repeat("a", 64), FileCount: 1, TotalBytes: 10, Extra: "{}"}
	require.NoError(t, database.Table(dal.WebProjectTable).Select(dal.WebProjectColumns()).Create(project).Error)
	require.NoError(t, database.Table(dal.WebProjectReleaseTable).Create(release).Error)
	t.Cleanup(func() {
		dal.ConfigureReadCache(nil, nil)
		_ = database.Table(dal.WebProjectReleaseTable).Where("id = ?", releaseID).Delete(&model.WebProjectRelease{}).Error
		_ = database.Table(dal.WebProjectTable).Where("id = ?", id).Delete(&model.WebProject{}).Error
		keys, _ := raw.Keys(ctx, prefix+":*").Result()
		if len(keys) > 0 {
			_ = raw.Del(ctx, keys...).Err()
		}
		_ = raw.Close()
	})
	repository := New()
	_, value, err := repository.FindPublished(project.Slug, ctx)
	require.NoError(t, err)
	require.Equal(t, release.StorageKey, value.StorageKey)
	key := client.Key("web_release", fmt.Sprintf("%d:%d", id, releaseID))
	require.Equal(t, int64(1), raw.Exists(ctx, key).Val())
	require.NoError(t, database.Table(dal.WebProjectTable).Where("id = ?", id).Update("status", model.WebProjectStatusDisabled).Error)
	current, _, err := repository.FindPublished(project.Slug, ctx)
	require.NoError(t, err)
	require.Equal(t, model.WebProjectStatusDisabled, current.Status)
	abort := errors.New("fixture rollback")
	err = repository.Transaction(func(tx *Transaction) error {
		_, err := tx.MarkReleasesDeleting(id, []int64{releaseID})
		require.NoError(t, err)
		return abort
	})
	require.ErrorIs(t, err, abort)
	require.Equal(t, int64(1), raw.Exists(ctx, key).Val(), "rollback must not evict a committed ready snapshot")
	require.NoError(t, repository.Transaction(func(tx *Transaction) error {
		_, err := tx.MarkReleasesDeleting(id, []int64{releaseID})
		return err
	}))
	require.Equal(t, int64(0), raw.Exists(ctx, key).Val(), "deleting state evicts after transaction commit")
	require.NoError(t, database.Table(dal.WebProjectReleaseTable).Where("id = ?", releaseID).Update("status", model.WebProjectReleaseReady).Error)
	_, _, err = repository.FindPublished(project.Slug, ctx)
	require.NoError(t, err)
	updated, err := repository.CompareAndSwapReleaseStorageKey(releaseID, id, id, release.StorageKey, "fixture/new-content")
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, int64(0), raw.Exists(ctx, key).Val())
	_, value, err = repository.FindPublished(project.Slug, ctx)
	require.NoError(t, err)
	require.Equal(t, "fixture/new-content", value.StorageKey)
}
