package accountsmigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/domain/dal/migrations"
)

type AdditiveOptions struct {
	Database     string
	Steps        []Step
	DDLChecksums map[string]string
}

type AdditivePlan struct {
	Version        int               `json:"version"`
	Database       string            `json:"database"`
	DDLChecksums   map[string]string `json:"ddl_checksums"`
	SchemaSHA256   string            `json:"schema_sha256"`
	PendingSteps   []string          `json:"pending_steps"`
	PendingMarkers []string          `json:"pending_markers"`
	Markers        []AdditiveMarker  `json:"markers"`
	SHA256         string            `json:"plan_sha256"`
}

type AdditiveMarker struct {
	ID       string `json:"id"`
	Checksum string `json:"checksum"`
	Phase    string `json:"phase"`
}

// AccountCenterOptions keeps the new DDL separate from historical import checksums.
func AccountCenterOptions(database string) (AdditiveOptions, error) {
	const name = "20260917_account_center.sql"
	opts := AdditiveOptions{Database: database, DDLChecksums: map[string]string{}}
	raw, err := migrations.SQL.ReadFile(name)
	if err != nil {
		return opts, errors.New("required embedded account center migration is missing")
	}
	opts.Steps, err = ParseDDL(name, raw)
	opts.DDLChecksums[name] = digest(raw)
	return opts, err
}

// BuildAdditivePlan only fingerprints schema, reviewed DDL and its stage markers.
// It never reads configuration identities, session contents or avatar bytes.
func BuildAdditivePlan(ctx context.Context, db *sql.DB, opts AdditiveOptions) (*AdditivePlan, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, dbError("open migration connection", err)
	}
	defer conn.Close()
	return buildAdditivePlan(ctx, conn, opts)
}

// ApplyAdditive binds the reviewed plan to an exact database while writers are
// stopped. Each additive DDL step has its own durable marker; user data and the
// historical accounts-config import checksum remain untouched on every rerun.
func ApplyAdditive(ctx context.Context, db *sql.DB, opts AdditiveOptions, planHash, confirmDatabase string, maintenanceConfirmed bool) error {
	if !maintenanceConfirmed {
		return errors.New("additive migration requires maintenance confirmation")
	}
	if confirmDatabase == "" || confirmDatabase != opts.Database {
		return errors.New("exact database confirmation is required")
	}
	if len(planHash) != 64 {
		return errors.New("reviewed plan SHA256 is required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return dbError("open migration connection", err)
	}
	defer conn.Close()
	release, err := acquireAccountMigrationLock(ctx, conn, opts.Database)
	if err != nil {
		return err
	}
	defer release()
	plan, err := buildAdditivePlan(ctx, conn, opts)
	if err != nil {
		return err
	}
	if plan.SHA256 != planHash {
		return errors.New("plan drift detected; regenerate and review the plan before applying")
	}
	meta, err := metadataStep()
	if err != nil {
		return err
	}
	schema, err := inspectTable(ctx, conn, opts.Database, "schema_migration")
	if err != nil {
		return err
	}
	if schema == nil {
		if _, err := conn.ExecContext(ctx, meta.SQL); err != nil {
			return dbError("create migration metadata", err)
		}
	}
	pendingMarkers := map[string]bool{}
	for _, id := range plan.PendingMarkers {
		pendingMarkers[id] = true
	}
	for _, step := range opts.Steps {
		if !pendingMarkers[step.ID] {
			continue
		}
		if err := applyStep(ctx, conn, opts.Database, step); err != nil {
			return err
		}
	}
	readback, err := buildAdditivePlan(ctx, conn, opts)
	if err != nil {
		return fmt.Errorf("additive migration readback failed: %w", err)
	}
	if len(readback.PendingSteps) != 0 || len(readback.PendingMarkers) != 0 {
		return errors.New("additive migration readback has unfinished schema or markers")
	}
	return nil
}

// buildAdditivePlan checks the connected database, historical account baseline,
// strictly parsed additive definitions and stage integrity before fingerprinting.
// It only reads metadata; schema and markers are both bound to the plan hash so
// an interrupted apply must be reviewed again without importing any user data.
func buildAdditivePlan(ctx context.Context, db queryer, opts AdditiveOptions) (*AdditivePlan, error) {
	if !identifier.MatchString(opts.Database) || len(opts.Database) > 64 || len(opts.Steps) == 0 || len(opts.DDLChecksums) == 0 {
		return nil, errors.New("valid database and reviewed additive DDL are required")
	}
	var currentDatabase string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&currentDatabase); err != nil {
		return nil, dbError("check database", err)
	}
	if currentDatabase != opts.Database {
		return nil, errors.New("connected database differs from requested target")
	}
	names := map[string]bool{"schema_migration": true, "user_account": true, "login_token": true}
	seen := map[string]bool{}
	for _, step := range opts.Steps {
		if !identifier.MatchString(step.Table) || step.ID == "" || len(step.ID) > 128 || seen[step.ID] || step.Checksum != digest([]byte(step.SQL)) {
			return nil, errors.New("additive DDL step definition is invalid")
		}
		parsed, err := ParseDDL("additive-check", []byte(step.SQL))
		if err != nil || len(parsed) != 1 {
			return nil, errors.New("additive DDL step must contain one strictly parsed additive statement")
		}
		parsed[0].ID = step.ID
		if !reflect.DeepEqual(parsed[0], step) {
			return nil, errors.New("additive DDL definition differs from parsed statement")
		}
		seen[step.ID] = true
		names[step.Table] = true
	}
	schemas := map[string]*TableSchema{}
	for name := range names {
		schema, err := inspectTable(ctx, db, opts.Database, name)
		if err != nil {
			return nil, err
		}
		schemas[name] = schema
	}
	if err := verifyAccountCenterBase(schemas); err != nil {
		return nil, err
	}
	plan := &AdditivePlan{Version: 1, Database: opts.Database, DDLChecksums: opts.DDLChecksums, PendingSteps: []string{}, PendingMarkers: []string{}, Markers: []AdditiveMarker{}}
	for _, step := range opts.Steps {
		complete, err := verifyStep(step, schemas[step.Table])
		if err != nil {
			return nil, err
		}
		if !complete {
			plan.PendingSteps = append(plan.PendingSteps, step.ID)
		}
	}
	if schemas["schema_migration"] != nil {
		meta, err := metadataStep()
		if err != nil {
			return nil, err
		}
		if _, err := verifyStep(meta, schemas["schema_migration"]); err != nil {
			return nil, err
		}
		if err := verifyMarkers(ctx, db, opts.Steps, schemas); err != nil {
			return nil, err
		}
	}
	for _, step := range opts.Steps {
		marker := AdditiveMarker{ID: step.ID, Checksum: step.Checksum, Phase: "absent"}
		if schemas["schema_migration"] != nil {
			err := db.QueryRowContext(ctx, "SELECT checksum,phase FROM schema_migration WHERE migration_id=?", step.ID).Scan(&marker.Checksum, &marker.Phase)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, dbError("read additive DDL marker", err)
			}
		}
		plan.Markers = append(plan.Markers, marker)
		if marker.Phase != "completed" {
			plan.PendingMarkers = append(plan.PendingMarkers, step.ID)
		}
	}
	plan.SchemaSHA256 = digest(schemaJSON(schemas))
	raw, _ := json.Marshal(plan)
	plan.SHA256 = digest(raw)
	return plan, nil
}

func verifyAccountCenterBase(schemas map[string]*TableSchema) error {
	raw, err := migrations.SQL.ReadFile("20260913_accounts.sql")
	if err != nil {
		return errors.New("required historical account migration is missing")
	}
	steps, err := ParseDDL("20260913_accounts.sql", raw)
	if err != nil {
		return err
	}
	for _, step := range steps {
		if step.Table != "user_account" && step.Table != "login_token" {
			continue
		}
		complete, err := verifyStep(step, schemas[step.Table])
		if err != nil {
			return err
		}
		if !complete {
			return errors.New("historical account schema is incomplete; install the account migration before this upgrade")
		}
	}
	if err := verifyLegacyTypes(schemas["login_token"]); err != nil {
		return err
	}
	for _, name := range []string{"create_time", "expire_time"} {
		compatible := false
		for _, column := range schemas["login_token"].Columns {
			if column.Name == name && (strings.HasPrefix(column.Type, "datetime") || strings.HasPrefix(column.Type, "timestamp")) {
				compatible = true
			}
		}
		if !compatible {
			return fmt.Errorf("required session time column is missing or incompatible: login_token.%s", name)
		}
	}
	return nil
}

func acquireAccountMigrationLock(ctx context.Context, db queryer, database string) (func(), error) {
	lockName := "home_accounts:" + digest([]byte(database))[:32]
	var locked sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lockName).Scan(&locked); err != nil {
		return nil, dbError("acquire migration lock", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return nil, errors.New("another account migration or maintenance task owns this database lock")
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var ignored sql.NullInt64
		db.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&ignored)
	}, nil
}
