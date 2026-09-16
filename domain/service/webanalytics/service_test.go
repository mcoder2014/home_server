package webanalytics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testService(t *testing.T) (*Service, *redis.Client, *gorm.DB, *time.Time) {
	t.Helper()
	addr, dsn := os.Getenv("HOME_SERVER_TEST_REDIS_ADDR"), os.Getenv("HOME_SERVER_ANALYTICS_TEST_DSN")
	if addr == "" || dsn == "" {
		t.Skip("isolated Redis and analytics MySQL test settings required")
	}
	if !strings.Contains(dsn, "/home_server_analytics_test_") {
		t.Fatal("refusing non-isolated analytics DSN")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, ContextTimeoutEnabled: true})
	database, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated test database:", err)
	}
	_, file, _, _ := runtime.Caller(0)
	ddl, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../dal/migrations/20260914_web_project_stats.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(stmt) != "" {
			if err := database.Exec(stmt).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	clock := time.Date(2026, 9, 14, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	service, err := New(client, database, Options{Prefix: "test:analytics:" + uuid.NewString(), Timezone: "Asia/Shanghai", Now: func() time.Time { return clock }, FlushInterval: time.Hour, QueueSize: 16, OperationTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Close(ctx)
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, service.options.Prefix+":analytics:v1:*", 100).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				_ = client.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		for _, table := range []string{"web_project_stat_daily", "web_project_stat_total"} {
			_ = database.Exec("DELETE FROM "+table+" WHERE project_id = ?", testProjectID(service)).Error
		}
		_ = client.Close()
		raw, _ := database.DB()
		_ = raw.Close()
	})
	return service, client, database, &clock
}

func testProjectID(s *Service) int64 {
	// Stable test-specific positive identifier without a global shared project row.
	var id int64 = 17
	for _, b := range []byte(s.options.Prefix) {
		id = (id*31 + int64(b)) % 9000000000000000
	}
	return id + 1000000
}

func mustRecord(t *testing.T, s *Service, visitor string, at time.Time) {
	t.Helper()
	if !s.Record(testProjectID(s), visitor, at) {
		t.Fatal("event unexpectedly rejected")
	}
}

func mustFlush(t *testing.T, s *Service) *Stats {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx, testProjectID(s), 90)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestSnapshotsCountRefreshAndCrossMidnightUV(t *testing.T) {
	s, _, _, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustRecord(t, s, "visitor-a", *clock)
	*clock = clock.Add(2 * time.Second)
	mustRecord(t, s, "visitor-a", *clock)
	mustRecord(t, s, "visitor-b", *clock)
	stats := mustFlush(t, s)
	if stats.Total.PV != 4 || stats.Total.UV != 2 {
		t.Fatalf("total=%+v", stats.Total)
	}
	if len(stats.Daily) != 90 {
		t.Fatalf("daily rows=%d", len(stats.Daily))
	}
	prev, today := stats.Daily[88], stats.Daily[89]
	if prev.PV != 2 || prev.UV != 1 || today.PV != 2 || today.UV != 2 {
		t.Fatalf("midnight days: %+v %+v", prev, today)
	}
	retry := mustFlush(t, s)
	if retry.Total != stats.Total {
		t.Fatal("flush replay changed totals")
	}
}

func TestRedisCompleteNewerStateSurvivesWorkerRestart(t *testing.T) {
	s, client, database, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	if err := s.process(context.Background(), visit{projectID: testProjectID(s), visitor: "visitor-b", at: *clock}); err != nil {
		t.Fatal(err)
	}
	resumed, err := New(client, database, s.options)
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, resumed, "visitor-a", *clock)
	stats := mustFlush(t, resumed)
	if stats.Total.PV != 3 || stats.Total.UV != 2 {
		t.Fatalf("lost newer Redis data: %+v", stats.Total)
	}
	_ = resumed.Close(context.Background())
}

func TestPartialRedisLossRestoresHLLAndMarksGap(t *testing.T) {
	s, client, _, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	if err := client.Del(context.Background(), s.projectBase(testProjectID(s))+"uv:all").Err(); err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, "visitor-a", *clock)
	stats := mustFlush(t, s)
	if stats.Total.PV != 1 || stats.Total.UV != 1 {
		t.Fatalf("uncertain event must not blindly replay: %+v", stats.Total)
	}
	if stats.Quality != "degraded" {
		t.Fatalf("loss not marked: %+v", stats)
	}
	mustRecord(t, s, "visitor-a", *clock)
	recovered := mustFlush(t, s)
	if recovered.Total.PV != 2 || recovered.Total.UV != 1 {
		t.Fatalf("restored HLL not reused: %+v", recovered.Total)
	}
	if recovered.Daily[89].Quality != "degraded" {
		t.Fatal("historical gap silently cleared")
	}
}

func TestMissingRedisRestoresPersistedTotal(t *testing.T) {
	s, client, database, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	keys, err := client.Keys(context.Background(), s.projectBase(testProjectID(s))+"*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Del(context.Background(), keys...).Err(); err != nil {
		t.Fatal(err)
	}
	resumed, err := New(client, database, s.options)
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, resumed, "visitor-a", *clock)
	stats := mustFlush(t, resumed)
	if stats.Total.PV != 2 || stats.Total.UV != 1 || stats.Quality != "degraded" {
		t.Fatalf("restore=%+v", stats)
	}
	_ = resumed.Close(context.Background())
}

func TestNinetyCalendarDaysRetainPermanentTotals(t *testing.T) {
	s, client, _, clock := testService(t)
	*clock = time.Date(2026, 12, 31, 12, 0, 0, 0, clock.Location())
	first := *clock
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	*clock = first.AddDate(0, 0, 89)
	mustRecord(t, s, "visitor-a", *clock)
	stats := mustFlush(t, s)
	if stats.Daily[0].Date != first.Format("2006-01-02") || stats.Daily[0].PV != 1 {
		t.Fatalf("day90 boundary: %+v", stats.Daily[0])
	}
	*clock = first.AddDate(0, 0, 90)
	mustRecord(t, s, "visitor-a", *clock)
	stats = mustFlush(t, s)
	if stats.Total.PV != 3 || stats.Total.UV != 1 || len(stats.Daily) != 90 || stats.Daily[0].PV != 0 {
		t.Fatalf("retention=%+v", stats)
	}
	exists, err := client.Exists(context.Background(), s.projectBase(testProjectID(s))+"uv:"+first.Format("2006-01-02")).Result()
	if err != nil || exists != 0 {
		t.Fatalf("cold persisted HLL retained: %d %v", exists, err)
	}
}

func TestNotStartedIsNotHealthyHistoricalZero(t *testing.T) {
	s, _, _, _ := testService(t)
	stats, err := s.Stats(context.Background(), testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TrackingStartedAt != nil || stats.PersistedAt != nil || stats.Quality != "not_started" || len(stats.Daily) != 0 {
		t.Fatalf("not started=%+v", stats)
	}
}

func TestBoundedQueueAndCloseRejectFurtherEvents(t *testing.T) {
	s, _, _, clock := testService(t)
	for i := 0; i < cap(s.queue); i++ {
		mustRecord(t, s, fmt.Sprint(i), *clock)
	}
	start := time.Now()
	if s.Record(testProjectID(s), "dropped", *clock) {
		t.Fatal("overflow was accepted")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("queue overflow blocked response")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Record(testProjectID(s), "after-close", *clock) {
		t.Fatal("closed service accepts events")
	}
	if s.Health().Dropped < 1 {
		t.Fatal("drop is not observable")
	}
	stats, err := s.Stats(ctx, testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Quality != "degraded" {
		t.Fatal("overflow gap missing from persisted stats")
	}
}

func TestConcurrentWorkerFlushAndClosePreserveAcceptedEvents(t *testing.T) {
	s, _, _, clock := testService(t)
	s.Start(context.Background())
	var producers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		producers.Add(1)
		go func() {
			defer producers.Done()
			for i := 0; i < 20; i++ {
				s.Record(testProjectID(s), "same-browser", *clock)
			}
		}()
	}
	flushed := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		flushed <- s.Flush(ctx)
	}()
	producers.Wait()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx, testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.PV != s.Health().Accepted || stats.Total.UV != 1 {
		t.Fatalf("accepted=%d persisted=%+v", s.Health().Accepted, stats.Total)
	}
}

func TestCloseDeadlineBoundsWaitingForAnotherFlush(t *testing.T) {
	s, _, _, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	s.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := s.Close(ctx)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("close deadline=%v duration=%v", err, time.Since(start))
	}
	<-s.gate
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background(), testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.PV != 1 {
		t.Fatalf("retry close lost event: %+v", stats.Total)
	}
}
