package dal

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WebProjectStatTotalTable = "web_project_stat_total"
	WebProjectStatDailyTable = "web_project_stat_daily"
)

var ErrWebProjectStatsRegression = errors.New("web project statistics cannot decrease")

var statTotalColumns = []string{"project_id", "pv", "uv", "last_seq", "tracking_started_at", "persisted_at", "quality", "quality_reason", "timezone", "format_version"}
var statDailyColumns = []string{"project_id", "CAST(stat_date AS CHAR) AS stat_date", "pv", "uv", "snapshot_seq", "persisted_at", "quality", "quality_reason"}

// ReadWebProjectStats reads daily and total rows from one consistent snapshot.
// Display queries omit HLL blobs. Recovery explicitly requests them.
func ReadWebProjectStats(ctx context.Context, database *gorm.DB, projectID int64, from, until string, withHLL bool) (*model.WebProjectStatTotal, []model.WebProjectStatDaily, error) {
	var total model.WebProjectStatTotal
	var daily []model.WebProjectStatDaily
	found := false
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		totals := append([]string(nil), statTotalColumns...)
		days := append([]string(nil), statDailyColumns...)
		if withHLL {
			totals = append(totals, "uv_hll")
			days = append(days, "uv_hll")
		}
		err := tx.Table(WebProjectStatTotalTable).Select(totals).Where("project_id = ?", projectID).Take(&total).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return tx.Table(WebProjectStatDailyTable).Select(days).Where("project_id = ? AND stat_date >= ? AND stat_date <= ?", projectID, from, until).Order("stat_date ASC").Find(&daily).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil || !found {
		return nil, nil, err
	}
	return &total, daily, nil
}

// PersistWebProjectStats commits absolute daily/total snapshots in one transaction.
// Locking the total row serializes sequence comparison. Replays and older writes
// are no-ops, including a retry after the previous COMMIT response was lost.
func PersistWebProjectStats(ctx context.Context, database *gorm.DB, total model.WebProjectStatTotal, daily []model.WebProjectStatDaily) (bool, error) {
	if total.ProjectID <= 0 || total.LastSeq == 0 {
		return false, errors.New("invalid analytics snapshot identity or sequence")
	}
	for _, day := range daily {
		if day.ProjectID != total.ProjectID || day.SnapshotSeq > total.LastSeq {
			return false, errors.New("inconsistent analytics daily snapshot")
		}
	}
	applied := false
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := total
		seed.LastSeq = 0
		if err := tx.Table(WebProjectStatTotalTable).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		var stored model.WebProjectStatTotal
		if err := tx.Table(WebProjectStatTotalTable).Clauses(clause.Locking{Strength: "UPDATE"}).Select("project_id", "last_seq", "pv", "uv").Where("project_id = ?", total.ProjectID).Take(&stored).Error; err != nil {
			return err
		}
		if stored.LastSeq >= total.LastSeq {
			return nil
		}
		if total.PV < stored.PV || total.UV < stored.UV {
			return ErrWebProjectStatsRegression
		}
		if len(daily) > 0 {
			dates := make([]string, len(daily))
			for i, day := range daily {
				dates[i] = day.StatDate
			}
			var storedDays []model.WebProjectStatDaily
			if err := tx.Table(WebProjectStatDailyTable).Select("project_id", "CAST(stat_date AS CHAR) AS stat_date", "pv", "uv").Where("project_id = ? AND stat_date IN ?", total.ProjectID, dates).Find(&storedDays).Error; err != nil {
				return err
			}
			byDate := make(map[string]model.WebProjectStatDaily, len(storedDays))
			for _, day := range storedDays {
				byDate[day.StatDate] = day
			}
			for _, day := range daily {
				storedDay, ok := byDate[day.StatDate]
				if ok && (day.PV < storedDay.PV || day.UV < storedDay.UV) {
					return ErrWebProjectStatsRegression
				}
			}
			err := tx.Table(WebProjectStatDailyTable).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "stat_date"}}, DoUpdates: clause.AssignmentColumns([]string{"pv", "uv", "uv_hll", "snapshot_seq", "persisted_at", "quality", "quality_reason"})}).CreateInBatches(daily, 100).Error
			if err != nil {
				return err
			}
		}
		fields := map[string]interface{}{"pv": total.PV, "uv": total.UV, "uv_hll": total.UVHLL, "last_seq": total.LastSeq, "tracking_started_at": total.TrackingStartedAt, "persisted_at": total.PersistedAt, "quality": total.Quality, "quality_reason": total.QualityReason, "timezone": total.Timezone, "format_version": total.FormatVersion}
		if err := tx.Table(WebProjectStatTotalTable).Where("project_id = ? AND last_seq < ?", total.ProjectID, total.LastSeq).Updates(fields).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

// CleanupWebProjectStats removes at most 500 expired day rows per invocation.
// The oldest indexed primary-key batch is a resumable cursor: deleted rows are
// absent on the next pass. Permanent totals and HLLs are never touched.
func CleanupWebProjectStats(ctx context.Context, database *gorm.DB, before string) (int64, error) {
	var removed int64
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var keys []model.WebProjectStatDaily
		if err := tx.Table(WebProjectStatDailyTable).Select("project_id", "CAST(stat_date AS CHAR) AS stat_date").Where("stat_date < ?", before).Order("stat_date ASC, project_id ASC").Limit(500).Find(&keys).Error; err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		tuples := make([][]interface{}, len(keys))
		for i, key := range keys {
			tuples[i] = []interface{}{key.ProjectID, key.StatDate}
		}
		result := tx.Table(WebProjectStatDailyTable).Where("(project_id, stat_date) IN ? AND stat_date < ?", tuples, before).Delete(&model.WebProjectStatDaily{})
		removed = result.RowsAffected
		return result.Error
	})
	return removed, err
}
