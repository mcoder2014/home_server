package webanalytics

import (
	"context"
	"errors"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
)

type Counts struct {
	PV uint64 `json:"pv"`
	UV uint64 `json:"uv"`
}

type DailyStats struct {
	Date          string `json:"date"`
	PV            uint64 `json:"pv"`
	UV            uint64 `json:"uv"`
	Quality       string `json:"quality"`
	QualityReason string `json:"quality_reason"`
}

type Stats struct {
	Enabled           bool         `json:"enabled"`
	ProjectID         int64        `json:"project_id,string"`
	Timezone          string       `json:"timezone"`
	UVMethod          string       `json:"uv_method"`
	TrackingStartedAt *time.Time   `json:"tracking_started_at"`
	PersistedAt       *time.Time   `json:"persisted_at"`
	Total             Counts       `json:"total"`
	Daily             []DailyStats `json:"daily"`
	Quality           string       `json:"quality"`
	QualityReason     string       `json:"quality_reason"`
}

// Stats exposes only committed SQL data. Dates before the real tracking start
// remain explicitly not_started; a project with no row has no fabricated history.
func (s *Service) Stats(ctx context.Context, projectID int64, days int) (*Stats, error) {
	if projectID <= 0 || (days != 7 && days != 30 && days != 90) {
		return nil, errors.New("analytics requires a project and days=7,30,90")
	}
	if days > s.options.RetentionDays {
		days = s.options.RetentionDays
	}
	now := s.options.Now().In(s.location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location).AddDate(0, 0, 1-days)
	total, daily, err := dal.ReadWebProjectStats(ctx, s.database, projectID, start.Format("2006-01-02"), now.Format("2006-01-02"), false)
	if err != nil {
		return nil, err
	}
	result := &Stats{Enabled: true, ProjectID: projectID, Timezone: s.options.Timezone, UVMethod: "browser_hll", Daily: []DailyStats{}, Quality: "not_started", QualityReason: "no_persisted_visits"}
	s.gapsMu.Lock()
	pending := make(map[string]string)
	for key, reason := range s.pendingGaps {
		if key.projectID == projectID {
			pending[key.date] = reason
		}
	}
	for key, reason := range s.gaps {
		if key.projectID == projectID {
			pending[key.date] = reason
		}
	}
	s.gapsMu.Unlock()
	if total == nil {
		if len(pending) > 0 {
			result.Quality = "degraded"
			result.QualityReason = "collection_gap_pending"
		}
		return result, nil
	}
	if total.Timezone != s.options.Timezone || total.FormatVersion != 1 {
		return nil, errors.New("persisted analytics format or timezone mismatch")
	}
	tracking := total.TrackingStartedAt.In(s.location)
	persisted := total.PersistedAt.In(s.location)
	result.TrackingStartedAt = &tracking
	result.PersistedAt = &persisted
	result.Total = Counts{PV: total.PV, UV: total.UV}
	result.Quality = total.Quality
	result.QualityReason = total.QualityReason
	indexed := make(map[string]DailyStats, len(daily))
	for _, day := range daily {
		indexed[day.StatDate] = DailyStats{Date: day.StatDate, PV: day.PV, UV: day.UV, Quality: day.Quality, QualityReason: day.QualityReason}
	}
	first := tracking.Format("2006-01-02")
	for date := start; !date.After(now); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		day, ok := indexed[key]
		if !ok {
			day = DailyStats{Date: key, Quality: "ok"}
			if key < first {
				day.Quality = "not_started"
				day.QualityReason = "before_tracking_started"
			}
		}
		if reason, ok := pending[key]; ok {
			day.Quality = "degraded"
			day.QualityReason = reason
		}
		result.Daily = append(result.Daily, day)
	}
	if len(pending) > 0 && result.Quality != "degraded" {
		result.Quality = "degraded"
		result.QualityReason = "collection_gap_pending"
	}
	return result, nil
}
