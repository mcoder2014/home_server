package accounts_test

import (
	"context"
	"fmt"
	"github.com/mcoder2014/home_server/app/acceleration"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/webanalytics"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPStatisticsRemainOwnerOnlyWhenDisabled(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	guest := f.login(f.guest, integrationPassword)
	project, _ := f.privatePage(owner)
	id := project["id"].(string)
	for _, prefix := range []string{"/api/web-share/", "/api/web-projects/"} {
		result := f.request(http.MethodGet, prefix+id+"/stats?days=30", owner, nil, nil)
		if result.Status != http.StatusOK || result.Data["enabled"] != false {
			t.Fatalf("owner must receive explicit disabled statistics: status=%d data=%v", result.Status, result.Data)
		}
		if result.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("statistics must not be browser cached")
		}
		result = f.request(http.MethodGet, prefix+id+"/stats?days=30", guest, nil, nil)
		if result.Status != http.StatusNotFound {
			t.Fatalf("guest must not see owner statistics: %d", result.Status)
		}
		result = f.request(http.MethodGet, prefix+id+"/stats?days=1", owner, nil, nil)
		if result.Status != http.StatusBadRequest {
			t.Fatalf("unsupported range accepted: %d", result.Status)
		}
	}
}

func TestHTTPHostedRequestReusesFreshAccountSnapshot(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	f.privatePage(owner)
	var accountReads atomic.Int64
	database := db.MasterDB()
	callback := "test:read-snapshot"
	if err := database.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		if query.Statement.Table == "user_account" {
			accountReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Callback().Query().Remove(callback) })
	result := f.request(http.MethodGet, "/p/http-private-fixture/index.html", owner, nil, nil)
	if result.Status != http.StatusOK {
		t.Fatalf("page status=%d", result.Status)
	}
	if count := accountReads.Load(); count != 1 {
		t.Fatalf("one request must query the same account once, got %d", count)
	}
	accountReads.Store(0)
	result = f.request(http.MethodGet, "/p/http-private-fixture/style.css", owner, nil, nil)
	if result.Status != http.StatusOK || accountReads.Load() != 1 {
		t.Fatal("next request must obtain its own fresh account snapshot")
	}
}

// TestHTTPAnalyticsCountsOnlyDocuments exercises real handlers, Redis HLL and
// committed SQL snapshots. All users, files, database rows and keys are synthetic.
func TestHTTPAnalyticsCountsOnlyDocumentsAndKeepsRevocation(t *testing.T) {
	t.Setenv("HOME_SERVER_CACHE_HTTP_TEST", "")
	address := os.Getenv("HOME_SERVER_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("isolated Redis required")
	}
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	project, _ := f.privatePage(member)
	id := project["id"].(string)
	numericID, _ := strconv.ParseInt(id, 10, 64)
	ddl, err := migrations.SQL.ReadFile("20260914_web_project_stats.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := f.database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	conf := f.conf
	conf.Redis = config.RedisConfig{Enabled: true, Address: address, KeyPrefix: fmt.Sprintf("hs:test:http-analytics:%d", time.Now().UnixNano()), CommandTimeoutMS: 100}
	conf.Cache = config.CacheConfig{Enabled: true}
	conf.Analytics = config.AnalyticsConfig{Enabled: true, FlushIntervalSeconds: 60, VisitorHMACKeyFile: filepath.Join(t.TempDir(), "visitor-key")}
	if err := os.WriteFile(conf.Analytics.VisitorHMACKeyFile, []byte("synthetic visitor key 01234567890123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err := acceleration.Initialize(conf, db.MasterDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
		client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
		defer client.Close()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, conf.Redis.KeyPrefix+":*", 100).Result()
			if err != nil {
				t.Error(err)
				break
			}
			if len(keys) > 0 {
				client.Del(ctx, keys...)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	})
	path := "/p/http-private-fixture/index.html"
	first := f.request(http.MethodGet, path, member, nil, map[string]string{"Accept": "text/html", "Sec-Fetch-Dest": "document"})
	if first.Status != 200 {
		t.Fatalf("document: %d", first.Status)
	}
	var visitor *http.Cookie
	for _, cookie := range first.Cookies {
		if cookie.Name == "__Host-cq_visit" {
			visitor = cookie
		}
	}
	if visitor == nil {
		t.Fatal("document should issue independent visitor cookie")
	}
	headers := map[string]string{"Accept": "text/html", "Sec-Fetch-Dest": "document", "Cookie": member.Cookie.String() + "; " + visitor.String()}
	if result := f.request("GET", path, member, nil, headers); result.Status != 200 {
		t.Fatal("repeat document failed")
	}
	if result := f.request("GET", "/p/http-private-fixture/style.css", member, nil, headers); result.Status != 200 {
		t.Fatal("stylesheet failed")
	}
	if result := f.request("HEAD", path, member, nil, headers); result.Status != 200 {
		t.Fatal("HEAD failed")
	}
	rangeHeaders := map[string]string{"Accept": "text/html", "Sec-Fetch-Dest": "document", "Cookie": headers["Cookie"], "Range": "bytes=0-9"}
	if result := f.request("GET", path, member, nil, rangeHeaders); result.Status != 206 {
		t.Fatalf("range: %d", result.Status)
	}
	headers["If-None-Match"] = first.Header.Get("ETag")
	if result := f.request("GET", path, member, nil, headers); result.Status != 304 {
		t.Fatalf("conditional: %d", result.Status)
	}
	if result := f.request("GET", "/p/http-private-fixture/missing.html", member, nil, headers); result.Status != 404 {
		t.Fatal("missing file must fail")
	}
	delete(headers, "If-None-Match")
	deadline := time.Now().Add(5 * time.Second)
	var measured *webanalytics.Stats
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = runtime.Analytics.Flush(ctx)
		measured, _ = runtime.Analytics.Stats(ctx, numericID, 30)
		cancel()
		if err == nil && measured != nil && measured.Total.PV == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if measured == nil || measured.Total.PV != 3 || measured.Total.UV != 1 {
		t.Fatalf("document PV/UV wrong: %+v, error=%v", measured, err)
	}
	stats := requireSuccess(t, f.request("GET", "/api/web-share/"+id+"/stats?days=90", member, nil, nil))
	if number(object(stats["total"])["pv"]) != 3 || stats["enabled"] != true {
		t.Fatal("owner stats differ from snapshot")
	}
	if result := f.request("GET", "/api/web-share/"+id+"/stats?days=90", admin, nil, nil); result.Status != 404 {
		t.Fatal("administrator is not implicitly the statistics owner")
	}
	token := f.applicationToken(member, []string{"web-projects:read"})
	bearer := map[string]string{"Authorization": "Bearer " + token}
	requireSuccess(t, f.request("GET", "/api/web-share/"+id+"/stats?days=30", nil, nil, bearer))
	requireSuccess(t, f.request("GET", "/api/web-share/"+id+"/stats?days=30", nil, nil, bearer))
	requireSuccess(t, f.adminAction(admin, member.ID, "ban", "POST", map[string]interface{}{}))
	requireDenied(t, f.request("GET", path, member, nil, headers))
	requireDenied(t, f.request("GET", "/api/web-share/"+id+"/stats?days=30", nil, nil, bearer))
}

// configureFixtureReadCache optionally runs all existing account/permission HTTP
// regressions with real hot caches, each fixture using its own disposable keys.
func configureFixtureReadCache(f *httpFixture) {
	if os.Getenv("HOME_SERVER_CACHE_HTTP_TEST") != "1" {
		return
	}
	address := os.Getenv("HOME_SERVER_TEST_REDIS_ADDR")
	if address == "" {
		f.t.Fatal("cache HTTP regression requires isolated Redis")
	}
	conf := f.conf
	conf.Redis = config.RedisConfig{Enabled: true, Address: address, KeyPrefix: fmt.Sprintf("hs:test:http-cache:%d", time.Now().UnixNano()), CommandTimeoutMS: 100}
	conf.Cache = config.CacheConfig{Enabled: true}
	runtime, err := acceleration.Initialize(conf, db.MasterDB())
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			f.t.Error(err)
		}
		client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
		defer client.Close()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, conf.Redis.KeyPrefix+":*", 100).Result()
			if err != nil {
				f.t.Error(err)
				return
			}
			if len(keys) > 0 {
				if err := client.Del(ctx, keys...).Err(); err != nil {
					f.t.Error(err)
					return
				}
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
}
