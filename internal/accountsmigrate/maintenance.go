package accountsmigrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const metadataBatchSize = 500
const sessionMetadataPresent = "(login_ip<>'' OR user_agent<>'' OR client_name<>'' OR os_name<>'' OR device_type<>'' OR login_source<>'')"

type MetadataMaintenanceResult struct {
	Database    string `json:"database"`
	ClearedRows int64  `json:"cleared_rows"`
	Batches     int    `json:"batches"`
}

// ClearSessionMetadata erases six descriptive fields strictly thirty days after
// natural expiry. It uses the expiry/id cursor, commits at most 500 rows per
// transaction and stops within five minutes. Token rows, authentication state,
// account data and audit records are preserved, including after interruption.
func ClearSessionMetadata(ctx context.Context, db *sql.DB, database string, now time.Time) (*MetadataMaintenanceResult, error) {
	if !identifier.MatchString(database) || len(database) > 64 || now.IsZero() {
		return nil, errors.New("valid database and maintenance time are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, dbError("open maintenance connection", err)
	}
	defer conn.Close()
	release, err := acquireAccountMigrationLock(ctx, conn, database)
	if err != nil {
		return nil, err
	}
	defer release()
	opts, err := AccountCenterOptions(database)
	if err != nil {
		return nil, err
	}
	plan, err := buildAdditivePlan(ctx, conn, opts)
	if err != nil {
		return nil, err
	}
	if len(plan.PendingSteps) != 0 || len(plan.PendingMarkers) != 0 {
		return nil, errors.New("account center migration must be completed before metadata maintenance")
	}
	result := &MetadataMaintenanceResult{Database: database}
	cutoff := now.Add(-30 * 24 * time.Hour)
	var cursorExpiry time.Time
	var cursorID int64
	for {
		query := "SELECT id,expire_time FROM login_token WHERE expire_time < ? AND " + sessionMetadataPresent
		args := []interface{}{cutoff}
		if !cursorExpiry.IsZero() {
			query += " AND (expire_time > ? OR (expire_time=? AND id>?))"
			args = append(args, cursorExpiry, cursorExpiry, cursorID)
		}
		query += " ORDER BY expire_time,id LIMIT 500"
		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			return result, dbError("select expired metadata batch", err)
		}
		ids := make([]interface{}, 0, metadataBatchSize)
		for rows.Next() {
			if err := rows.Scan(&cursorID, &cursorExpiry); err != nil {
				rows.Close()
				return result, dbError("read expired metadata batch", err)
			}
			ids = append(ids, cursorID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, dbError("read expired metadata batch", err)
		}
		if len(ids) == 0 {
			return result, nil
		}
		cleared, err := clearMetadataBatch(ctx, conn, ids, cutoff)
		if err != nil {
			return result, err
		}
		result.ClearedRows += cleared
		result.Batches++
	}
}

func clearMetadataBatch(ctx context.Context, conn *sql.Conn, ids []interface{}, cutoff time.Time) (int64, error) {
	if len(ids) == 0 || len(ids) > metadataBatchSize {
		return 0, errors.New("metadata batch must contain between one and five hundred rows")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, dbError("begin metadata batch", err)
	}
	defer tx.Rollback()
	query := "UPDATE login_token SET login_ip='',user_agent='',client_name='',os_name='',device_type='',login_source='' WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ") AND expire_time < ? AND " + sessionMetadataPresent
	args := append(append([]interface{}{}, ids...), cutoff)
	updated, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, dbError("clear expired metadata batch", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return 0, dbError("read metadata batch result", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, dbError("commit metadata batch", err)
	}
	return count, nil
}
