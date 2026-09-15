package webanalytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type rejectLuaCommands struct {
	seenLua  atomic.Bool
	seenPing atomic.Bool
}

func (h *rejectLuaCommands) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *rejectLuaCommands) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "eval", "evalsha", "script", "fcall", "fcall_ro":
			h.seenLua.Store(true)
			return errors.New("test: server-side scripting is forbidden")
		case "ping":
			h.seenPing.Store(true)
			return errors.New("NOPERM test: PING not allowed")
		default:
			return next(ctx, cmd)
		}
	}
}
func (h *rejectLuaCommands) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		for _, cmd := range commands {
			switch cmd.Name() {
			case "eval", "evalsha", "script", "fcall", "fcall_ro":
				h.seenLua.Store(true)
				return errors.New("test: server-side scripting is forbidden")
			}
		}
		return next(ctx, commands)
	}
}

func TestAnalyticsUsesNoRedisServerSideScriptsOrPingProbe(t *testing.T) {
	s, client, _, clock := testService(t)
	hook := &rejectLuaCommands{}
	client.AddHook(hook)
	mustRecord(t, s, "visitor-a", *clock)
	stats := mustFlush(t, s)
	if hook.seenLua.Load() || hook.seenPing.Load() || stats.Total.PV != 1 || stats.Total.UV != 1 {
		t.Fatalf("analytics used a Redis script/PING or lost the visit: lua=%v ping=%v stats=%+v", hook.seenLua.Load(), hook.seenPing.Load(), stats.Total)
	}
}

// A successful Redis write followed by a transport error is the important
// ambiguity: a replay would count the same HTML response twice.
type lostRedisReply struct {
	fired atomic.Bool
	armed atomic.Bool
}

func (h *lostRedisReply) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *lostRedisReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		err := next(ctx, commands)
		if err != nil || !h.armed.Load() {
			return err
		}
		for _, cmd := range commands {
			args := cmd.Args()
			if len(args) > 2 && cmd.Name() == "hincrby" && args[2] == "pv" && !h.fired.Swap(true) {
				return errors.New("test: Redis reply lost after mutation")
			}
		}
		return nil
	}
}
func (h *lostRedisReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return next
}

func TestBusinessLayerDoesNotReplayFailedRedisWrite(t *testing.T) {
	s, client, _, clock := testService(t)
	fault := &lostRedisReply{}
	client.AddHook(fault)
	fault.armed.Store(true)
	mustRecord(t, s, "visitor-a", *clock)
	stats := mustFlush(t, s)
	if !fault.fired.Load() || stats.Total.PV != 1 || stats.Total.UV != 1 || stats.Quality != "degraded" {
		t.Fatalf("ambiguous reply: %+v fault=%v", stats, fault.fired.Load())
	}
	mustRecord(t, s, "visitor-a", *clock)
	stats = mustFlush(t, s)
	if stats.Total.PV != 2 || stats.Total.UV != 1 || s.Health().Unknown != 1 {
		t.Fatalf("PV replayed: %+v %+v", stats, s.Health())
	}
}

// faultCommitPool reports one lost COMMIT response after the real server commit.
// Subsequent retry must observe last_seq and leave both table snapshots unchanged.
type faultCommitPool struct {
	gorm.ConnPool
	fired   atomic.Bool
	enabled atomic.Bool
}
type faultCommitTx struct {
	*sql.Tx
	owner  *faultCommitPool
	inject bool
}

func (p *faultCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	raw := p.ConnPool.(*sql.DB)
	tx, err := raw.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &faultCommitTx{Tx: tx, owner: p, inject: options == nil || !options.ReadOnly}, nil
}
func (t *faultCommitTx) Commit() error {
	err := t.Tx.Commit()
	if err == nil && t.inject && t.owner.enabled.Load() && !t.owner.fired.Swap(true) {
		return errors.New("test: SQL COMMIT response lost")
	}
	return err
}

func TestSQLCommitResponseLostAndOlderSequenceAreIdempotent(t *testing.T) {
	s, _, database, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	older, err := s.readRedis(context.Background(), testProjectID(s))
	if err != nil {
		t.Fatal(err)
	}
	pool := &faultCommitPool{ConnPool: database.ConnPool}
	pool.enabled.Store(true)
	faulty, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// Initialization has already completed; only the persistence transaction below
	// uses this pool, so the injected commit error applies to the write transaction.
	s.database = faulty
	mustRecord(t, s, "visitor-b", *clock)
	if err := s.Flush(context.Background()); err == nil {
		t.Fatal("lost commit response was not injected")
	}
	stats, err := s.Stats(context.Background(), testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.PV != 2 || stats.Total.UV != 2 {
		t.Fatalf("actual SQL commit missing: %+v", stats.Total)
	}
	replay := mustFlush(t, s)
	if replay.Total != stats.Total {
		t.Fatal("SQL retry accumulated counters")
	}
	applied, err := dal.PersistWebProjectStats(context.Background(), database, older.total, older.daily)
	if err != nil || applied {
		t.Fatalf("older sequence applied: %v %v", applied, err)
	}
	after := mustFlush(t, s)
	if after.Total != stats.Total {
		t.Fatal("older snapshot overwrote newer data")
	}
}

func TestHigherSequenceCannotReduceDurableSnapshots(t *testing.T) {
	for _, target := range []string{"total", "daily"} {
		t.Run(target, func(t *testing.T) {
			s, _, database, clock := testService(t)
			mustRecord(t, s, "visitor-a", *clock)
			mustRecord(t, s, "visitor-b", *clock)
			mustFlush(t, s)
			snapshot, err := s.readRedis(context.Background(), testProjectID(s))
			if err != nil {
				t.Fatal(err)
			}
			snapshot.total.LastSeq++
			snapshot.total.PersistedAt = *clock
			for i := range snapshot.daily {
				snapshot.daily[i].SnapshotSeq = snapshot.total.LastSeq
				snapshot.daily[i].PersistedAt = *clock
			}
			if target == "total" {
				snapshot.total.PV--
				snapshot.total.UV--
			} else {
				snapshot.daily[0].PV--
				snapshot.daily[0].UV--
			}
			if _, err = dal.PersistWebProjectStats(context.Background(), database, snapshot.total, snapshot.daily); err == nil {
				t.Fatal("higher sequence replaced durable counters with smaller values")
			}
			stats, err := s.Stats(context.Background(), testProjectID(s), 7)
			if err != nil || stats.Total.PV != 2 || stats.Total.UV != 2 || stats.Daily[6].PV != 2 || stats.Daily[6].UV != 2 {
				t.Fatalf("durable snapshot regressed: %+v %v", stats, err)
			}
		})
	}
}

func TestRedisHLLReplacementMergesDurableIdentityBaseline(t *testing.T) {
	s, client, _, clock := testService(t)
	id := testProjectID(s)
	ctx := context.Background()
	mustRecord(t, s, "visitor-a", *clock)
	mustRecord(t, s, "visitor-b", *clock)
	mustFlush(t, s)

	base := s.projectBase(id)
	state := base + "state"
	date := clock.Format("2006-01-02")
	if err := client.Del(ctx, base+"uv:all").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.PFAdd(ctx, base+"uv:all", "visitor-c", "visitor-d").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.PFAdd(ctx, base+"uv:"+date, "visitor-c", "visitor-d").Err(); err != nil {
		t.Fatal(err)
	}
	seq, err := client.HIncrBy(ctx, state, "seq", 2).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err = client.HIncrBy(ctx, state, "pv", 2).Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.HIncrBy(ctx, state, "pv:"+date, 2).Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.HSet(ctx, state, "seq:"+date, seq).Err(); err != nil {
		t.Fatal(err)
	}
	if err = client.SAdd(ctx, s.projectKeys(id)[2], id).Err(); err != nil {
		t.Fatal(err)
	}
	stats := mustFlush(t, s)
	if stats.Total.PV != 4 || stats.Total.UV != 4 {
		t.Fatalf("durable HLL baseline was replaced: %+v", stats.Total)
	}
	mustRecord(t, s, "visitor-a", *clock)
	stats = mustFlush(t, s)
	if stats.Total.PV != 5 || stats.Total.UV != 4 {
		t.Fatalf("restored identity was counted again: %+v", stats.Total)
	}
}

func TestDirtyDaysHaveNoTTLAndOnlyCleanColdDaysAreRemoved(t *testing.T) {
	s, client, _, clock := testService(t)
	first := *clock
	if err := s.process(context.Background(), visit{testProjectID(s), "visitor-a", first}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.AddDate(0, 0, 5)
	key := s.projectBase(testProjectID(s)) + "uv:" + first.Format("2006-01-02")
	if ttl, err := client.TTL(context.Background(), key).Result(); err != nil || ttl != -time.Nanosecond {
		t.Fatalf("dirty TTL=%v err=%v", ttl, err)
	}
	stats := mustFlush(t, s)
	if stats.Total.PV != 1 || stats.Total.UV != 1 {
		t.Fatalf("old dirty day lost: %+v", stats.Total)
	}
	if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 0 {
		t.Fatalf("clean cold key remains: %d %v", n, err)
	}
}

func TestCleanupBatchIsBoundedAtFiveHundredRows(t *testing.T) {
	s, _, database, clock := testService(t)
	id := testProjectID(s)
	rows := make([]model.WebProjectStatDaily, 501)
	for i := range rows {
		rows[i] = model.WebProjectStatDaily{ProjectID: id, StatDate: clock.AddDate(0, 0, -100-i).Format("2006-01-02"), UVHLL: []byte{}, PersistedAt: *clock, Quality: "ok"}
	}
	if err := database.Table(dal.WebProjectStatDailyTable).CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	removed, err := dal.CleanupWebProjectStats(context.Background(), database, clock.AddDate(0, 0, -89).Format("2006-01-02"))
	if err != nil || removed != 500 {
		t.Fatalf("cleanup=%d %v", removed, err)
	}
	var count int64
	if err := database.Table(dal.WebProjectStatDailyTable).Where("project_id = ?", id).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("remaining=%d %v", count, err)
	}
}

func TestRedisRestartWithOlderCompleteStateRestoresBeforeMoreCounting(t *testing.T) {
	s, client, _, clock := testService(t)
	id := testProjectID(s)
	ctx := context.Background()
	base := s.projectBase(id)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	old, err := client.HGetAll(ctx, base+"state").Result()
	if err != nil {
		t.Fatal(err)
	}
	oldTotal, err := client.Get(ctx, base+"uv:all").Result()
	if err != nil {
		t.Fatal(err)
	}
	dayKey := base + "uv:" + clock.Format("2006-01-02")
	oldDaily, err := client.Get(ctx, dayKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, "visitor-b", *clock)
	mustRecord(t, s, "visitor-c", *clock)
	mustFlush(t, s)
	// Simulate a Redis persistence image older than the durable SQL snapshot.
	if err := client.HSet(ctx, base+"state", old).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, base+"uv:all", oldTotal, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, dayKey, oldDaily, 0).Err(); err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, "visitor-a", *clock)
	stats := mustFlush(t, s)
	if stats.Total.PV != 3 || stats.Total.UV != 3 || stats.Quality != "degraded" {
		t.Fatalf("restart regression: %+v", stats)
	}
	restored, err := s.readRedis(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.total.PV != 3 || restored.total.UV != 3 {
		t.Fatalf("Redis stayed older than SQL: %+v", restored.total)
	}
	mustRecord(t, s, "visitor-a", *clock)
	stats = mustFlush(t, s)
	if stats.Total.PV != 4 || stats.Total.UV != 3 {
		t.Fatalf("next event changed restored identities: %+v", stats.Total)
	}
}

func TestColdDayRegressionRestoresInsteadOfRetryingForever(t *testing.T) {
	s, client, _, clock := testService(t)
	id := testProjectID(s)
	ctx := context.Background()
	base := s.projectBase(id)
	first := clock.AddDate(0, 0, -10)
	*clock = first
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	oldState, err := client.HGetAll(ctx, base+"state").Result()
	if err != nil {
		t.Fatal(err)
	}
	oldTotal, err := client.Get(ctx, base+"uv:all").Result()
	if err != nil {
		t.Fatal(err)
	}
	oldDayKey := base + "uv:" + first.Format("2006-01-02")
	oldDay, err := client.Get(ctx, oldDayKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, "visitor-b", *clock)
	mustRecord(t, s, "visitor-c", *clock)
	mustFlush(t, s)

	// Replaying the old image after the day becomes cold must still compare that
	// retained Redis day with SQL. New visits can otherwise hide the total loss.
	*clock = first.AddDate(0, 0, 10)
	if err := client.HSet(ctx, base+"state", oldState).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, base+"uv:all", oldTotal, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, oldDayKey, oldDay, 0).Err(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		mustRecord(t, s, fmt.Sprintf("current-%d", i), *clock)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("cold regression was not restored: %v", err)
	}
	stats, err := s.Stats(ctx, id, 90)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.PV != 3 || stats.Total.UV != 3 || stats.Quality != "degraded" || s.Health().RestoreCount == 0 {
		t.Fatalf("cold regression restore: stats=%+v health=%+v", stats.Total, s.Health())
	}
}

func TestQueueOverflowIsVisibleBeforeNextSQLFlush(t *testing.T) {
	s, _, _, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	for i := 0; i < cap(s.queue); i++ {
		mustRecord(t, s, "visitor-a", *clock)
	}
	if s.Record(testProjectID(s), "lost", *clock) {
		t.Fatal("overflow accepted")
	}
	stats, err := s.Stats(context.Background(), testProjectID(s), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.PV != 1 || stats.Quality != "degraded" || stats.Daily[6].Quality != "degraded" {
		t.Fatalf("pending loss is invisible: %+v", stats)
	}
}

type unsafePolicyReply struct{ denied bool }

func (h unsafePolicyReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h unsafePolicyReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h unsafePolicyReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "config" && len(cmd.Args()) > 1 && cmd.Args()[1] == "get" {
			if h.denied {
				return errors.New("NOPERM test: CONFIG GET not allowed")
			}
			result, ok := cmd.(*redis.MapStringStringCmd)
			if !ok {
				return errors.New("unexpected CONFIG GET result type")
			}
			result.SetVal(map[string]string{"maxmemory-policy": "allkeys-lru"})
			return nil
		}
		return next(ctx, cmd)
	}
}

func TestEvictionPolicyDoesNotPauseBestEffortStatistics(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint("permission_denied=", denied), func(t *testing.T) {
			s, client, _, clock := testService(t)
			client.AddHook(unsafePolicyReply{denied: denied})
			mustRecord(t, s, "visitor-a", *clock)
			stats := mustFlush(t, s)
			if stats.Total.PV != 1 || stats.Total.UV != 1 || s.Health().UnavailableReason != "" {
				t.Fatalf("eviction policy reduced availability: %+v %+v", stats.Total, s.Health())
			}
		})
	}
}

func TestNewVisitAfterSnapshotLeavesDirtyForNextCommit(t *testing.T) {
	s, client, database, clock := testService(t)
	id := testProjectID(s)
	ctx := context.Background()
	if err := s.process(ctx, visit{id, "visitor-a", *clock}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.readRedis(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.process(ctx, visit{id, "visitor-b", *clock}); err != nil {
		t.Fatal(err)
	}
	snapshot.total.PersistedAt = *clock
	for i := range snapshot.daily {
		snapshot.daily[i].PersistedAt = *clock
	}
	if _, err := dal.PersistWebProjectStats(ctx, database, snapshot.total, snapshot.daily); err != nil {
		t.Fatal(err)
	}
	if err := s.acknowledge(ctx, id, snapshot.total.LastSeq, clock.AddDate(0, 0, -2).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	dirty, err := client.SIsMember(ctx, s.projectKeys(id)[2], id).Result()
	if err != nil || !dirty {
		t.Fatalf("new visit dirty marker cleared: %v %v", dirty, err)
	}
	stats := mustFlush(t, s)
	if stats.Total.PV != 2 || stats.Total.UV != 2 {
		t.Fatalf("post-snapshot visit lost: %+v", stats.Total)
	}
}

func TestAnalyticsQueryPlansUseProjectDateAndRetentionIndexes(t *testing.T) {
	s, _, database, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	type explainRow struct {
		Table string
		Type  string
		Key   *string
		Rows  int64
		Extra string
	}
	cases := []struct {
		query string
		args  []interface{}
		key   string
	}{
		{"EXPLAIN SELECT stat_date, pv, uv, quality, quality_reason, persisted_at FROM web_project_stat_daily WHERE project_id = ? AND stat_date >= ? AND stat_date <= ? ORDER BY stat_date ASC", []interface{}{testProjectID(s), clock.AddDate(0, 0, -89).Format("2006-01-02"), clock.Format("2006-01-02")}, "PRIMARY"},
		{"EXPLAIN SELECT project_id, stat_date FROM web_project_stat_daily WHERE stat_date < ? ORDER BY stat_date ASC, project_id ASC LIMIT 500", []interface{}{clock.AddDate(0, 0, -89).Format("2006-01-02")}, "idx_stat_date_project"},
	}
	for _, item := range cases {
		var rows []explainRow
		if err := database.Raw(item.query, item.args...).Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Key == nil || *rows[0].Key != item.key {
			t.Fatalf("expected index %s: %+v", item.key, rows)
		}
		t.Logf("table=%s type=%s key=%s estimated_rows=%d extra=%s", rows[0].Table, rows[0].Type, *rows[0].Key, rows[0].Rows, rows[0].Extra)
	}
}

func TestLargeSequenceRemainsAnExactDecimalInteger(t *testing.T) {
	s, client, _, clock := testService(t)
	mustRecord(t, s, "visitor-a", *clock)
	mustFlush(t, s)
	if err := client.HSet(context.Background(), s.projectBase(testProjectID(s))+"state", "seq", "100000000000000").Err(); err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, "visitor-b", *clock)
	stats := mustFlush(t, s)
	if stats.Total.PV != 2 || stats.Total.UV != 2 {
		t.Fatalf("large seq lost visit: %+v", stats.Total)
	}
	daySeq, err := client.HGet(context.Background(), s.projectBase(testProjectID(s))+"state", "seq:"+clock.Format("2006-01-02")).Result()
	if err != nil || daySeq != "100000000000001" {
		t.Fatalf("sequence changed precision: %s %v", daySeq, err)
	}
}
