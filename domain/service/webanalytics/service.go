// Package webanalytics records best-effort browser analytics with ordinary
// Redis commands and durable MySQL snapshots. It never runs Redis scripts.
package webanalytics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const DefaultOperationTimeout = 2 * time.Second

type Options struct {
	Prefix           string
	Timezone         string
	RetentionDays    int
	HotDays          int
	QueueSize        int
	FlushInterval    time.Duration
	OperationTimeout time.Duration
	MaxQueueAge      time.Duration
	FlushEvents      int
	Now              func() time.Time
}

type visit struct {
	projectID int64
	visitor   string
	at        time.Time
}
type gapKey struct {
	projectID int64
	date      string
}

type Health struct {
	Accepted          uint64     `json:"accepted"`
	Dropped           uint64     `json:"dropped"`
	Unknown           uint64     `json:"unknown"`
	SnapshotRetry     uint64     `json:"snapshot_retry"`
	RestoreCount      uint64     `json:"restore_count"`
	QueueLength       int        `json:"queue_length"`
	LastSuccess       *time.Time `json:"last_success"`
	FlushLagSeconds   int64      `json:"flush_lag_seconds"`
	Quality           string     `json:"quality"`
	UnavailableReason string     `json:"unavailable_reason"`
}

type Service struct {
	client            redis.UniversalClient
	database          *gorm.DB
	options           Options
	location          *time.Location
	queue             chan visit
	gate              chan struct{}
	lifecycle         sync.Mutex
	closed            bool
	started           bool
	stop              chan struct{}
	done              chan struct{}
	gapsMu            sync.Mutex
	gaps              map[gapKey]string
	pendingGaps       map[gapKey]string
	gapOverflow       bool
	ready             map[int64]bool // accessed only by the serial worker gate
	known             map[int64]bool
	pendingEvents     int
	scanCursor        uint64
	accepted          atomic.Uint64
	dropped           atomic.Uint64
	unknown           atomic.Uint64
	retries           atomic.Uint64
	restores          atomic.Uint64
	lastSuccess       atomic.Int64
	degraded          atomic.Bool
	unavailable       atomic.Bool
	unavailableReason atomic.Value
}

func New(client redis.UniversalClient, database *gorm.DB, options Options) (*Service, error) {
	if client == nil || database == nil {
		return nil, errors.New("analytics Redis and database are required")
	}
	if _, ok := client.(*redis.Client); !ok {
		return nil, errors.New("analytics requires standalone Redis")
	}
	options.Prefix = strings.TrimRight(options.Prefix, ":")
	if options.Prefix == "" || len(options.Prefix) > 128 || strings.ContainsAny(options.Prefix, " \t\r\n*?[]{}") {
		return nil, errors.New("invalid analytics key prefix")
	}
	if options.Timezone == "" {
		options.Timezone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(options.Timezone)
	if err != nil {
		return nil, fmt.Errorf("analytics timezone: %w", err)
	}
	if options.RetentionDays == 0 {
		options.RetentionDays = 90
	}
	if options.HotDays == 0 {
		options.HotDays = 3
	}
	if options.QueueSize == 0 {
		options.QueueSize = 4096
	}
	if options.FlushInterval == 0 {
		options.FlushInterval = time.Minute
	}
	if options.OperationTimeout == 0 {
		options.OperationTimeout = DefaultOperationTimeout
	}
	if options.MaxQueueAge == 0 {
		options.MaxQueueAge = 5 * time.Second
	}
	if options.FlushEvents == 0 {
		options.FlushEvents = 1000
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RetentionDays < 1 || options.RetentionDays > 90 || options.HotDays < 1 || options.HotDays > options.RetentionDays || options.QueueSize < 1 || options.QueueSize > 65536 || options.FlushInterval <= 0 || options.OperationTimeout <= 0 || options.MaxQueueAge <= 0 || options.FlushEvents < 1 {
		return nil, errors.New("invalid analytics budgets or retention window")
	}
	return &Service{client: client, database: database, options: options, location: location, queue: make(chan visit, options.QueueSize), gate: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), gaps: make(map[gapKey]string), pendingGaps: make(map[gapKey]string), ready: make(map[int64]bool), known: make(map[int64]bool)}, nil
}

// Record only copies one bounded event into memory. It never calls Redis/SQL or
// starts a goroutine; rejection changes analytics health, not the HTML response.
func (s *Service) Record(projectID int64, visitorHash string, at time.Time) bool {
	if projectID <= 0 || len(visitorHash) == 0 || len(visitorHash) > 128 || at.IsZero() {
		return false
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.closed {
		return false
	}
	event := visit{projectID: projectID, visitor: visitorHash, at: at}
	select {
	case s.queue <- event:
		s.accepted.Add(1)
		return true
	default:
		s.dropped.Add(1)
		s.noteGap(projectID, at, "queue_full")
		return false
	}
}

func (s *Service) Start(ctx context.Context) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	go s.run(ctx)
}

func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.options.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case event := <-s.queue:
			op, cancel := context.WithTimeout(context.Background(), s.options.OperationTimeout)
			select {
			case s.gate <- struct{}{}:
				if err := s.process(op, event); err != nil {
					s.degraded.Store(true)
				}
				s.pendingEvents++
				flush := s.pendingEvents >= s.options.FlushEvents
				<-s.gate
				cancel()
				if flush {
					flushCtx, done := context.WithTimeout(context.Background(), s.options.FlushInterval)
					_ = s.Flush(flushCtx)
					done()
				}
			case <-op.Done():
				cancel()
				s.dropped.Add(1)
				s.noteGap(event.projectID, event.at, "worker_busy")
			}
		case <-ticker.C:
			op, cancel := context.WithTimeout(context.Background(), s.options.FlushInterval)
			_ = s.Flush(op)
			cancel()
		}
	}
}

// Close first stops admission and the background consumer, then drains with the
// caller's deadline. Repeated calls may retry a failed final snapshot safely.
func (s *Service) Close(ctx context.Context) error {
	s.lifecycle.Lock()
	if !s.closed {
		s.closed = true
		close(s.stop)
	}
	started := s.started
	s.lifecycle.Unlock()
	if started {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Flush(ctx)
}

func (s *Service) noteGap(projectID int64, at time.Time, reason string) {
	s.degraded.Store(true)
	key := gapKey{projectID: projectID, date: at.In(s.location).Format("2006-01-02")}
	s.gapsMu.Lock()
	defer s.gapsMu.Unlock()
	if _, ok := s.gaps[key]; ok {
		return
	}
	if len(s.gaps) >= s.options.QueueSize {
		s.gapOverflow = true
		return
	}
	s.gaps[key] = reason
}

func (s *Service) Health() Health {
	health := Health{Accepted: s.accepted.Load(), Dropped: s.dropped.Load(), Unknown: s.unknown.Load(), SnapshotRetry: s.retries.Load(), RestoreCount: s.restores.Load(), QueueLength: len(s.queue), Quality: "ok"}
	if s.degraded.Load() {
		health.Quality = "degraded"
	}
	if reason := s.unavailableReason.Load(); reason != nil {
		health.UnavailableReason = reason.(string)
	}
	if stamp := s.lastSuccess.Load(); stamp != 0 {
		at := time.Unix(0, stamp)
		health.LastSuccess = &at
		health.FlushLagSeconds = int64(s.options.Now().Sub(at) / time.Second)
		if health.FlushLagSeconds < 0 {
			health.FlushLagSeconds = 0
		}
	}
	return health
}

func (s *Service) projectBase(projectID int64) string {
	return fmt.Sprintf("%s:analytics:v1:{%d}:", s.options.Prefix, projectID)
}

func (s *Service) projectKeys(projectID int64) []string {
	base := s.projectBase(projectID)
	return []string{base + "state", base + "uv:all", s.options.Prefix + ":analytics:v1:dirty", s.options.Prefix + ":analytics:v1:projects"}
}

// process is called only under the worker gate. A Redis mutation error has an
// unknown outcome: preserve the event's gap and never replay its PV increment.
func (s *Service) process(ctx context.Context, event visit) error {
	s.known[event.projectID] = true
	age := s.options.Now().Sub(event.at)
	if age > s.options.MaxQueueAge || age < -time.Minute {
		s.dropped.Add(1)
		s.noteGap(event.projectID, event.at, "event_expired")
		return nil
	}
	if err := s.ensure(ctx, event.projectID, event.at); err != nil {
		s.dropped.Add(1)
		s.noteGap(event.projectID, event.at, "initialization_failed")
		return err
	}
	err := s.writeVisit(ctx, event.projectID, event.at.In(s.location).Format("2006-01-02"), event.visitor)
	if err != nil {
		delete(s.ready, event.projectID)
		s.unknown.Add(1)
		s.noteGap(event.projectID, event.at, "redis_write_unknown")
		s.unavailable.Store(true)
		s.unavailableReason.Store("redis_unavailable")
	} else {
		s.unavailable.Store(false)
		s.unavailableReason.Store("")
	}
	return err
}
