package webanalytics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/redis/go-redis/v9"
)

var errInvalidRedisState = errors.New("invalid analytics Redis state")

// readRedis uses two ordinary pipelines: the first reads the state and total
// HLL, while the second reads only dates named by that state. Missing or
// malformed pieces trigger MySQL recovery; no Redis server-side script runs.
func (s *Service) readRedis(ctx context.Context, id int64) (*redisSnapshot, error) {
	keys := s.projectKeys(id)
	pipe := s.client.Pipeline()
	stateCommand := pipe.HGetAll(ctx, keys[0])
	totalHLLCommand := pipe.Get(ctx, keys[1])
	totalUVCommand := pipe.PFCount(ctx, keys[1])
	if _, err := pipe.Exec(ctx); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("%w: required key missing", errInvalidRedisState)
		}
		return nil, err
	}

	values := stateCommand.Val()
	for _, field := range []string{"pv", "seq", "ack", "started", "quality", "reason", "timezone", "version"} {
		if _, ok := values[field]; !ok {
			return nil, fmt.Errorf("%w: field %s missing", errInvalidRedisState, field)
		}
	}
	if values["version"] != "1" || values["timezone"] != s.options.Timezone {
		return nil, fmt.Errorf("%w: format or timezone mismatch", errInvalidRedisState)
	}
	pv, err := redisCounter(values, "pv")
	if err != nil {
		return nil, err
	}
	seq, err := redisCounter(values, "seq")
	if err != nil {
		return nil, err
	}
	ack, err := redisCounter(values, "ack")
	if err != nil || ack > seq {
		return nil, fmt.Errorf("%w: invalid acknowledgment", errInvalidRedisState)
	}
	started, err := time.Parse(time.RFC3339Nano, values["started"])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid start time", errInvalidRedisState)
	}
	totalHLL := totalHLLCommand.Val()
	totalUV := totalUVCommand.Val()
	if totalUV < 0 || len(totalHLL) < 4 || totalHLL[:4] != "HYLL" {
		return nil, fmt.Errorf("%w: invalid total HLL", errInvalidRedisState)
	}

	dates := make([]string, 0, s.options.RetentionDays)
	for field := range values {
		if !strings.HasPrefix(field, "pv:") {
			continue
		}
		date := strings.TrimPrefix(field, "pv:")
		parsed, parseErr := time.Parse("2006-01-02", date)
		if parseErr != nil || parsed.Format("2006-01-02") != date {
			return nil, fmt.Errorf("%w: invalid date", errInvalidRedisState)
		}
		for _, prefix := range []string{"seq:", "quality:", "reason:"} {
			if _, ok := values[prefix+date]; !ok {
				return nil, fmt.Errorf("%w: incomplete date %s", errInvalidRedisState, date)
			}
		}
		if _, parseErr = redisCounter(values, "pv:"+date); parseErr != nil {
			return nil, parseErr
		}
		if _, parseErr = redisCounter(values, "seq:"+date); parseErr != nil {
			return nil, parseErr
		}
		dates = append(dates, date)
	}
	if len(dates) > 366 {
		return nil, fmt.Errorf("%w: too many dates", errInvalidRedisState)
	}
	sort.Strings(dates)

	type dailyCommands struct {
		date string
		hll  *redis.StringCmd
		uv   *redis.IntCmd
	}
	daily := make([]dailyCommands, len(dates))
	pipe = s.client.Pipeline()
	for i, date := range dates {
		key := s.projectBase(id) + "uv:" + date
		daily[i] = dailyCommands{date: date, hll: pipe.Get(ctx, key), uv: pipe.PFCount(ctx, key)}
	}
	if len(daily) > 0 {
		if _, err = pipe.Exec(ctx); err != nil {
			if errors.Is(err, redis.Nil) {
				return nil, fmt.Errorf("%w: daily HLL missing", errInvalidRedisState)
			}
			return nil, err
		}
	}

	out := &redisSnapshot{
		ack: ack,
		total: model.WebProjectStatTotal{
			ProjectID: id, PV: pv, UV: uint64(totalUV), UVHLL: []byte(totalHLL), LastSeq: seq,
			TrackingStartedAt: started, Timezone: values["timezone"], FormatVersion: 1,
			Quality: values["quality"], QualityReason: values["reason"],
		},
		daily: make([]model.WebProjectStatDaily, 0, len(daily)),
	}
	for _, commands := range daily {
		hll := commands.hll.Val()
		uv := commands.uv.Val()
		if uv < 0 || len(hll) < 4 || hll[:4] != "HYLL" {
			return nil, fmt.Errorf("%w: invalid daily HLL", errInvalidRedisState)
		}
		dayPV, _ := redisCounter(values, "pv:"+commands.date)
		daySeq, _ := redisCounter(values, "seq:"+commands.date)
		out.daily = append(out.daily, model.WebProjectStatDaily{
			ProjectID: id, StatDate: commands.date, PV: dayPV, UV: uint64(uv), UVHLL: []byte(hll),
			SnapshotSeq: daySeq, Quality: values["quality:"+commands.date], QualityReason: values["reason:"+commands.date],
		})
	}
	return out, nil
}

func redisCounter(values map[string]string, field string) (uint64, error) {
	value, err := strconv.ParseUint(values[field], 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid counter %s", errInvalidRedisState, field)
	}
	return value, nil
}

func redisPVRegressed(current *redisSnapshot, total *model.WebProjectStatTotal, daily []model.WebProjectStatDaily, hotFrom string) bool {
	if current.total.PV < total.PV {
		return true
	}
	currentDays := make(map[string]model.WebProjectStatDaily, len(current.daily))
	for _, day := range current.daily {
		currentDays[day.StatDate] = day
	}
	for _, day := range daily {
		value, ok := currentDays[day.StatDate]
		if !ok {
			if day.StatDate >= hotFrom {
				return true
			}
			continue
		}
		if value.PV < day.PV {
			return true
		}
	}
	return false
}

// mergeHLLBaseline unions the last durable identities into every current Redis
// HLL before a newer snapshot can replace MySQL. Temporary keys expire even if
// the process dies between the two ordinary pipelines.
func (s *Service) mergeHLLBaseline(ctx context.Context, id int64, total *model.WebProjectStatTotal, daily []model.WebProjectStatDaily, current *redisSnapshot) error {
	type mergePair struct {
		destination string
		temporary   string
		baseline    []byte
	}
	base := s.projectBase(id)
	pairs := []mergePair{{destination: base + "uv:all", temporary: base + "merge:all", baseline: total.UVHLL}}
	currentDays := make(map[string]bool, len(current.daily))
	for _, day := range current.daily {
		currentDays[day.StatDate] = true
	}
	for _, day := range daily {
		if currentDays[day.StatDate] && len(day.UVHLL) > 0 {
			pairs = append(pairs, mergePair{destination: base + "uv:" + day.StatDate, temporary: base + "merge:" + day.StatDate, baseline: day.UVHLL})
		}
	}
	filtered := pairs[:0]
	for _, pair := range pairs {
		if len(pair.baseline) == 0 {
			continue
		}
		if len(pair.baseline) < 16 || string(pair.baseline[:4]) != "HYLL" {
			return errors.New("persisted analytics HLL is invalid")
		}
		filtered = append(filtered, pair)
	}
	if len(filtered) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, pair := range filtered {
		pipe.Set(ctx, pair.temporary, pair.baseline, time.Minute)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, pair := range filtered {
			pipe.PFMerge(ctx, pair.destination, pair.destination, pair.temporary)
			pipe.Del(ctx, pair.temporary)
		}
		return nil
	})
	return err
}

// writeVisit deliberately favors availability over exact-once counting. The
// sequence reservation and Redis transaction are separate ordinary commands;
// a transport failure can leave a gap or duplicate after a client retry. Final
// errors are marked as gaps; a successful retry is indistinguishable from one
// successful execution. Neither outcome blocks the hosted response.
func (s *Service) writeVisit(ctx context.Context, id int64, date, visitor string) error {
	keys := s.projectKeys(id)
	dayKey := s.projectBase(id) + "uv:" + date
	pipe := s.client.Pipeline()
	stateExists := pipe.Exists(ctx, keys[0])
	totalExists := pipe.Exists(ctx, keys[1])
	dayExists := pipe.Exists(ctx, dayKey)
	dayInState := pipe.HExists(ctx, keys[0], "pv:"+date)
	seqCommand := pipe.HIncrBy(ctx, keys[0], "seq", 1)
	_, err := pipe.Exec(ctx)
	seq := seqCommand.Val()
	if err != nil || seq < 1 {
		return errors.Join(err, errors.New("analytics sequence increment failed"))
	}
	if stateExists.Val() == 0 || totalExists.Val() == 0 || (dayInState.Val() && dayExists.Val() == 0) {
		return fmt.Errorf("%w: working key evicted", errInvalidRedisState)
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		if !dayInState.Val() && dayExists.Val() > 0 {
			pipe.Del(ctx, dayKey)
		}
		pipe.PFAdd(ctx, dayKey, visitor)
		pipe.PFAdd(ctx, keys[1], visitor)
		pipe.HIncrBy(ctx, keys[0], "pv", 1)
		pipe.HIncrBy(ctx, keys[0], "pv:"+date, 1)
		pipe.HSet(ctx, keys[0], "seq:"+date, strconv.FormatInt(seq, 10))
		pipe.HSetNX(ctx, keys[0], "quality:"+date, "ok")
		pipe.HSetNX(ctx, keys[0], "reason:"+date, "")
		pipe.SAdd(ctx, keys[2], id)
		pipe.SAdd(ctx, keys[3], id)
		return nil
	})
	return err
}

// markQuality creates a zero day when needed and persists a visible degraded
// marker with ordinary Redis commands. Repeating it is safe for counters except
// for the internal sequence, whose only role is snapshot ordering.
func (s *Service) markQuality(ctx context.Context, id int64, date, reason string) error {
	keys := s.projectKeys(id)
	seq, err := s.client.HIncrBy(ctx, keys[0], "seq", 1).Result()
	if err != nil || seq < 1 {
		return errors.Join(err, errors.New("analytics quality sequence failed"))
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.PFAdd(ctx, s.projectBase(id)+"uv:"+date)
		pipe.HSetNX(ctx, keys[0], "pv:"+date, "0")
		pipe.HSet(ctx, keys[0], "seq:"+date, strconv.FormatInt(seq, 10))
		pipe.HSet(ctx, keys[0], "quality", "degraded", "reason", reason)
		pipe.HSet(ctx, keys[0], "quality:"+date, "degraded", "reason:"+date, reason)
		pipe.SAdd(ctx, keys[2], id)
		pipe.SAdd(ctx, keys[3], id)
		return nil
	})
	return err
}

// restoreRedis replaces one project's Redis working set with a MySQL snapshot.
// SCAN also finds orphaned day HLLs left by eviction or partial commands. A
// failed restore is retried later and never affects webpage delivery.
func (s *Service) restoreRedis(ctx context.Context, id int64, fields map[string]string, totalHLL []byte, daily []model.WebProjectStatDaily) error {
	keys := s.projectKeys(id)
	projectKeys := []string{keys[0], keys[1]}
	var cursor uint64
	for {
		found, next, err := s.client.Scan(ctx, cursor, s.projectBase(id)+"uv:*", 100).Result()
		if err != nil {
			return err
		}
		projectKeys = append(projectKeys, found...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].StatDate < daily[j].StatDate })
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, projectKeys...)
		pipe.HSet(ctx, keys[0], fields)
		if len(totalHLL) == 0 {
			pipe.PFAdd(ctx, keys[1])
		} else {
			pipe.Set(ctx, keys[1], totalHLL, 0)
		}
		for _, day := range daily {
			key := s.projectBase(id) + "uv:" + day.StatDate
			if len(day.UVHLL) == 0 {
				pipe.PFAdd(ctx, key)
			} else {
				pipe.Set(ctx, key, day.UVHLL, 0)
			}
		}
		pipe.SAdd(ctx, keys[2], id)
		pipe.SAdd(ctx, keys[3], id)
		return nil
	})
	return err
}

// acknowledge advances the SQL-backed sequence and removes only cold dates
// observed before this call. The service has one writer gate; deployments with
// multiple writers can race here and are outside the statistics contract.
func (s *Service) acknowledge(ctx context.Context, id int64, seq uint64, before string) error {
	keys := s.projectKeys(id)
	values, err := s.client.HGetAll(ctx, keys[0]).Result()
	if err != nil {
		return err
	}
	current, err := redisCounter(values, "seq")
	if err != nil || seq > current {
		return fmt.Errorf("%w: stale acknowledgment", errInvalidRedisState)
	}
	ack, err := redisCounter(values, "ack")
	if err != nil {
		return err
	}
	if seq > ack {
		ack = seq
	}
	type coldDay struct {
		date   string
		fields []string
	}
	cold := make([]coldDay, 0)
	for field := range values {
		if !strings.HasPrefix(field, "pv:") {
			continue
		}
		date := strings.TrimPrefix(field, "pv:")
		daySeq, parseErr := redisCounter(values, "seq:"+date)
		if parseErr != nil {
			return parseErr
		}
		if date < before && daySeq <= ack {
			cold = append(cold, coldDay{date: date, fields: []string{"pv:" + date, "seq:" + date, "quality:" + date, "reason:" + date}})
		}
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, keys[0], "ack", strconv.FormatUint(ack, 10))
		for _, day := range cold {
			pipe.Del(ctx, s.projectBase(id)+"uv:"+day.date)
			pipe.HDel(ctx, keys[0], day.fields...)
		}
		if current == seq {
			pipe.SRem(ctx, keys[2], id)
		}
		return nil
	})
	return err
}
