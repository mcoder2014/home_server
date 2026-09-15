package webanalytics

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

type redisSnapshot struct {
	total model.WebProjectStatTotal
	daily []model.WebProjectStatDaily
	ack   uint64
}

// ensure preserves complete newer Redis state. Recovery reads one SQL snapshot,
// restores daily and permanent HLL bytes, and marks the possible loss interval.
// The serial worker gate prevents visits from racing initialization/recovery.
func (s *Service) ensure(ctx context.Context, id int64, at time.Time) error {
	if s.ready[id] {
		return nil
	}
	now := s.options.Now().In(s.location)
	lower := now.AddDate(0, 0, 1-s.options.RetentionDays).Format("2006-01-02")
	hotFrom := now.AddDate(0, 0, 1-s.options.HotDays).Format("2006-01-02")
	total, daily, err := dal.ReadWebProjectStats(ctx, s.database, id, lower, now.Format("2006-01-02"), true)
	if err != nil {
		return err
	}
	if total != nil && (total.Timezone != s.options.Timezone || total.FormatVersion != 1) {
		return errors.New("persisted analytics format or timezone mismatch")
	}
	current, redisErr := s.readRedis(ctx, id)
	if redisErr == nil && (total == nil || current.total.LastSeq >= total.LastSeq) {
		if total != nil && redisPVRegressed(current, total, daily, hotFrom) {
			redisErr = errInvalidRedisState
		} else {
			if total != nil {
				if err := s.mergeHLLBaseline(ctx, id, total, daily, current); err != nil {
					return err
				}
			}
			if current.total.LastSeq > current.ack {
				if err := s.client.SAdd(ctx, s.projectKeys(id)[2], id).Err(); err != nil {
					return err
				}
			}
			s.ready[id] = true
			s.unavailable.Store(false)
			s.unavailableReason.Store("")
			return nil
		}
	}
	exists, err := s.client.Exists(ctx, s.projectKeys(id)[0], s.projectKeys(id)[1]).Result()
	if err != nil {
		s.unavailable.Store(true)
		s.unavailableReason.Store("redis_unavailable")
		return err
	}
	recovered := total != nil || exists > 0
	if total == nil {
		total = &model.WebProjectStatTotal{ProjectID: id, TrackingStartedAt: at, Quality: "ok", Timezone: s.options.Timezone, FormatVersion: 1}
	}
	if total.LastSeq > 0 && (len(total.UVHLL) < 16 || string(total.UVHLL[:4]) != "HYLL") {
		return errors.New("persisted analytics HLL is invalid")
	}
	seq := total.LastSeq
	fields := map[string]string{"pv": strconv.FormatUint(total.PV, 10), "seq": strconv.FormatUint(seq, 10), "ack": strconv.FormatUint(seq, 10), "started": total.TrackingStartedAt.UTC().Format(time.RFC3339Nano), "quality": total.Quality, "reason": total.QualityReason, "timezone": s.options.Timezone, "version": "1"}
	byDate := make(map[string]model.WebProjectStatDaily, len(daily))
	for _, day := range daily {
		byDate[day.StatDate] = day
	}
	if recovered {
		seq++
		fields["seq"] = strconv.FormatUint(seq, 10)
		if fields["quality"] != "degraded" {
			fields["quality"] = "degraded"
			fields["reason"] = "redis_restored"
		}
		first := at.In(s.location).Format("2006-01-02")
		if !total.PersistedAt.IsZero() {
			first = total.PersistedAt.In(s.location).Format("2006-01-02")
		}
		if first < lower {
			first = lower
		}
		from, err := time.ParseInLocation("2006-01-02", first, s.location)
		if err != nil {
			return err
		}
		for date := from; !date.After(now); date = date.AddDate(0, 0, 1) {
			key := date.Format("2006-01-02")
			day := byDate[key]
			day.ProjectID = id
			day.StatDate = key
			day.SnapshotSeq = seq
			if day.Quality != "degraded" {
				day.Quality = "degraded"
				day.QualityReason = "redis_restored"
			}
			byDate[key] = day
		}
		s.restores.Add(1)
		s.degraded.Store(true)
	}
	restoreDays := make([]model.WebProjectStatDaily, 0, len(byDate))
	for date, day := range byDate {
		if date < hotFrom && day.SnapshotSeq <= total.LastSeq {
			continue
		}
		if len(day.UVHLL) > 0 && (len(day.UVHLL) < 16 || string(day.UVHLL[:4]) != "HYLL") {
			return errors.New("persisted analytics daily HLL is invalid")
		}
		fields["pv:"+date] = strconv.FormatUint(day.PV, 10)
		fields["seq:"+date] = strconv.FormatUint(day.SnapshotSeq, 10)
		fields["quality:"+date] = day.Quality
		fields["reason:"+date] = day.QualityReason
		restoreDays = append(restoreDays, day)
	}
	if err := s.restoreRedis(ctx, id, fields, total.UVHLL, restoreDays); err != nil {
		return err
	}
	s.ready[id] = true
	s.unavailable.Store(false)
	s.unavailableReason.Store("")
	return nil
}

// Flush drains a bounded queue batch, persists all dirty dates and the total
// atomically per project, then acknowledges only committed sequences in Redis.
// SQL failures leave Redis intact; replaying an absolute snapshot is idempotent.
func (s *Service) Flush(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.gate }()
	var firstErr error
	count := len(s.queue)
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-s.queue:
			op, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
			if err := s.process(op, event); err != nil {
				s.degraded.Store(true)
			}
			cancel()
		default:
		}
	}
	if err := s.flushGaps(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	dirtyKey := s.options.Prefix + ":analytics:v1:dirty"
	// SSCAN is bounded per response and visits the complete dirty set. Snapshot
	// acknowledgment mutates membership, so known projects are also included.
	ids := make(map[int64]bool, len(s.known))
	for id := range s.known {
		ids[id] = true
	}
	var cursor uint64
	for {
		entries, next, err := s.client.SScan(ctx, dirtyKey, cursor, "*", 100).Result()
		if err != nil {
			s.degraded.Store(true)
			s.unavailable.Store(true)
			s.unavailableReason.Store("redis_unavailable")
			return err
		}
		for _, entry := range entries {
			id, err := strconv.ParseInt(entry, 10, 64)
			if err == nil && id > 0 {
				ids[id] = true
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	// A rolling registry scan also cleans cold, already persisted keys after an
	// idle restart and recovers an accidentally missing global dirty membership.
	entries, next, err := s.client.SScan(ctx, s.options.Prefix+":analytics:v1:projects", s.scanCursor, "*", 100).Result()
	if err != nil {
		s.unavailable.Store(true)
		s.unavailableReason.Store("redis_unavailable")
		return err
	}
	s.scanCursor = next
	for _, entry := range entries {
		id, err := strconv.ParseInt(entry, 10, 64)
		if err == nil && id > 0 {
			ids[id] = true
		}
	}
	for id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		op, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
		err := s.persistProject(op, id)
		cancel()
		if err != nil {
			s.retries.Add(1)
			s.degraded.Store(true)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	before := s.options.Now().In(s.location).AddDate(0, 0, 1-s.options.RetentionDays).Format("2006-01-02")
	if _, err := dal.CleanupWebProjectStats(ctx, s.database, before); err != nil && firstErr == nil {
		firstErr = err
	}
	s.pendingEvents = 0
	return firstErr
}

func (s *Service) flushGaps(ctx context.Context) error {
	s.gapsMu.Lock()
	gaps := s.gaps
	s.gaps = make(map[gapKey]string)
	overflow := s.gapOverflow
	s.gapOverflow = false
	s.gapsMu.Unlock()
	if overflow {
		for id := range s.known {
			gaps[gapKey{id, s.options.Now().In(s.location).Format("2006-01-02")}] = "unattributed_queue_loss"
		}
	}
	var firstErr error
	for key, reason := range gaps {
		at, err := time.ParseInLocation("2006-01-02", key.date, s.location)
		if err == nil {
			err = s.ensure(ctx, key.projectID, at)
		}
		if err == nil {
			err = s.markQuality(ctx, key.projectID, key.date, reason)
		}
		if err != nil {
			delete(s.ready, key.projectID)
			s.noteGap(key.projectID, at, reason)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			s.known[key.projectID] = true
			s.gapsMu.Lock()
			if len(s.pendingGaps) < s.options.QueueSize {
				s.pendingGaps[key] = reason
			} else {
				s.gapOverflow = true
			}
			s.gapsMu.Unlock()
		}
	}
	return firstErr
}

func (s *Service) persistProject(ctx context.Context, id int64) error {
	// Reconcile with MySQL once per flush. This catches a complete but older
	// Redis image after restart without requiring INFO, Lua, or a strict policy.
	delete(s.ready, id)
	if err := s.ensure(ctx, id, s.options.Now()); err != nil {
		return err
	}
	snapshot, err := s.readRedis(ctx, id)
	if err != nil {
		delete(s.ready, id)
		s.noteGap(id, s.options.Now(), "redis_state_lost")
		return err
	}
	now := s.options.Now().In(s.location)
	before := now.AddDate(0, 0, 1-s.options.RetentionDays).Format("2006-01-02")
	for _, day := range snapshot.daily {
		if day.StatDate < before && day.SnapshotSeq > snapshot.ack {
			if err := s.markQuality(ctx, id, now.Format("2006-01-02"), "retention_late_flush"); err != nil {
				return err
			}
			snapshot, err = s.readRedis(ctx, id)
			if err != nil {
				return err
			}
			break
		}
	}
	if snapshot.total.LastSeq > snapshot.ack {
		snapshot.total.PersistedAt = now
		daily := make([]model.WebProjectStatDaily, 0, len(snapshot.daily))
		for _, day := range snapshot.daily {
			if day.StatDate >= before {
				day.PersistedAt = now
				daily = append(daily, day)
			}
		}
		if _, err := dal.PersistWebProjectStats(ctx, s.database, snapshot.total, daily); err != nil {
			return err
		}
		s.lastSuccess.Store(now.UnixNano())
		s.gapsMu.Lock()
		for key := range s.pendingGaps {
			if key.projectID == id {
				delete(s.pendingGaps, key)
			}
		}
		s.gapsMu.Unlock()
	}
	hotFrom := now.AddDate(0, 0, 1-s.options.HotDays).Format("2006-01-02")
	return s.acknowledge(ctx, id, snapshot.total.LastSeq, hotFrom)
}
