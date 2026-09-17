package accountsmigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/dal/migrations"
)

const accountCenterTenStepSHA256 = "82525759688e6df72d479d7410a7541222f294cbf74bc2f79f0fdae285a24147"
const accountCenterElevenStepSHA256 = "d370264390b339674d48a04cf6883da9f913823d4eb91ccf423c8b3357ecffc0"

func TestAccountCenterOptionsEmbedReviewedEffectiveRangeWithoutChangingPriorSteps(t *testing.T) {
	const name = "20260917_account_center.sql"
	opts, err := AccountCenterOptions("home_server_accounts_migration_test_options")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Steps) != 11 {
		t.Fatalf("want eleven independently recoverable additions, got %d", len(opts.Steps))
	}
	raw, err := migrations.SQL.ReadFile(name)
	if err != nil || digest(raw) != accountCenterElevenStepSHA256 || opts.DDLChecksums[name] != accountCenterElevenStepSHA256 {
		t.Fatal("embedded DDL or options checksum differs from the reviewed eleven-step migration")
	}
	addition := "-- Bound effective-session reads by account version and natural expiry.\nALTER TABLE login_token ADD KEY idx_login_token_effective_range (user_id,auth_version,is_expired,expire_time,create_time,id);\n"
	priorRaw := []byte(strings.TrimSuffix(string(raw), addition))
	prior, err := ParseDDL(name, priorRaw)
	if err != nil || len(prior) != 10 || digest(priorRaw) != accountCenterTenStepSHA256 || !reflect.DeepEqual(prior, opts.Steps[:10]) {
		t.Fatal("appending the effective-range index changed a prior stage ID, statement or checksum")
	}
	last := opts.Steps[10]
	want := []Index{{Name: "idx_login_token_effective_range", Columns: []string{"user_id", "auth_version", "is_expired", "expire_time", "create_time", "id"}}}
	if last.ID != name+":011" || last.Table != "login_token" || last.Create || !reflect.DeepEqual(last.Indexes, want) {
		t.Fatal("the appended stage must contain the exact reviewed effective-session range index")
	}
}

func accountCenterFixture(t *testing.T) (*sql.DB, *Source, Options, AdditiveOptions) {
	t.Helper()
	db, source, old := fixtureDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec("ALTER TABLE login_token ADD create_time DATETIME(6) NULL"); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(ctx, db, source, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, source, old, plan.SHA256, old.Database); err != nil {
		t.Fatal(err)
	}
	addition, err := AccountCenterOptions(old.Database)
	if err != nil {
		t.Fatal(err)
	}
	return db, source, old, addition
}

func TestAccountCenterUpgradePreservesHistoricalImportAndChangedData(t *testing.T) {
	db, source, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	before, err := BuildPlan(ctx, db, source, old)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.PendingSteps) != 11 {
		t.Fatalf("want eleven independently recoverable additions, got %d", len(plan.PendingSteps))
	}
	var avatarTables int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='user_avatar'", old.Database).Scan(&avatarTables); err != nil || avatarTables != 0 {
		t.Fatal("default upgrade plan must remain read-only")
	}
	for _, protection := range []struct {
		hash, database string
		maintenance    bool
	}{{plan.SHA256, old.Database, false}, {plan.SHA256, "wrong_database", true}, {strings.Repeat("0", 64), old.Database, true}} {
		if err := ApplyAdditive(ctx, db, addition, protection.hash, protection.database, protection.maintenance); err == nil {
			t.Fatal("upgrade accepted missing maintenance, wrong database or stale plan")
		}
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE user_account SET display_name='changed-profile',auth_version=8,revision=9,avatar_version=9 WHERE id=123"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO user_avatar VALUES (123,9,'image/jpeg',?,3,NOW(6))", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	after, err := BuildPlan(ctx, db, source, old)
	if err != nil {
		t.Fatal(err)
	}
	if before.ImportSHA256 != after.ImportSHA256 || !after.DataAlreadyImported {
		t.Fatal("additive upgrade changed historical source/DDL checksum or import completion")
	}
	plan, err = BuildAdditivePlan(ctx, db, addition)
	if err != nil || len(plan.PendingSteps) != 0 {
		t.Fatalf("upgrade readback must have no pending schema: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_migration SET completed_at='2026-09-17 01:02:03' WHERE migration_id=?", addition.Steps[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	var completedAt string
	if err := db.QueryRow("SELECT DATE_FORMAT(completed_at,'%Y-%m-%d %H:%i:%s') FROM schema_migration WHERE migration_id=?", addition.Steps[0].ID).Scan(&completedAt); err != nil || completedAt != "2026-09-17 01:02:03" {
		t.Fatal("repeat upgrade changed the original completion time")
	}
	var name string
	var auth, revision, version int64
	if err := db.QueryRow("SELECT display_name,auth_version,revision,avatar_version FROM user_account WHERE id=123").Scan(&name, &auth, &revision, &version); err != nil || name != "changed-profile" || auth != 8 || revision != 9 || version != 9 {
		t.Fatal("repeat upgrade changed user profile, auth version, revision or avatar")
	}
}

// An already applied ten-stage fixture can append the expiry index without
// rewriting completed stages or changing the historical account/config import.
func TestAccountCenterUpgradeAppendsEffectiveRangeToCompletedTenStageSchema(t *testing.T) {
	db, source, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	before, err := BuildPlan(ctx, db, source, old)
	if err != nil {
		t.Fatal(err)
	}
	prior := addition
	prior.Steps = addition.Steps[:10]
	prior.DDLChecksums = map[string]string{"20260917_account_center.sql": accountCenterTenStepSHA256}
	plan, err := BuildAdditivePlan(ctx, db, prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, prior, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_migration SET completed_at='2026-09-17 01:02:03' WHERE migration_id LIKE '20260917_account_center.sql:%'"); err != nil {
		t.Fatal(err)
	}
	completed, err := BuildAdditivePlan(ctx, db, prior)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = BuildAdditivePlan(ctx, db, addition)
	if err != nil || !reflect.DeepEqual(plan.PendingSteps, []string{"20260917_account_center.sql:011"}) || !reflect.DeepEqual(plan.PendingMarkers, plan.PendingSteps) {
		t.Fatalf("completed ten-stage schema must plan only the appended range index: %v", err)
	}
	if !reflect.DeepEqual(completed.Markers, plan.Markers[:10]) || completed.SHA256 == plan.SHA256 {
		t.Fatal("new file checksum must change the plan without changing completed stage checksums")
	}
	if err := ApplyAdditive(ctx, db, addition, completed.SHA256, old.Database, true); err == nil || !strings.Contains(err.Error(), "plan drift") {
		t.Fatal("a plan reviewed before appending the range index must be rejected")
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	readback, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil || len(readback.PendingSteps) != 0 || len(readback.PendingMarkers) != 0 || !reflect.DeepEqual(completed.Markers, readback.Markers[:10]) {
		t.Fatalf("appended range index changed a completed stage or remained unfinished: %v", err)
	}
	var untouched int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migration WHERE migration_id LIKE '20260917_account_center.sql:%' AND migration_id < '20260917_account_center.sql:011' AND completed_at='2026-09-17 01:02:03'").Scan(&untouched); err != nil || untouched != 10 {
		t.Fatal("appending the index changed a prior completion time")
	}
	after, err := BuildPlan(ctx, db, source, old)
	if err != nil || before.ImportSHA256 != after.ImportSHA256 || !after.DataAlreadyImported {
		t.Fatalf("appending the range index changed the original import checksum: %v", err)
	}
}

func TestAccountCenterUpgradeResumesStartedDDLAndRejectsMarkerDrift(t *testing.T) {
	db, _, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	first := addition.Steps[0]
	if _, err := db.Exec(first.SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migration (migration_id,checksum,phase,started_at,summary_json) VALUES (?,?,'started',NOW(6),'{}')", first.ID, first.Checksum); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil || len(plan.PendingSteps) != 10 {
		t.Fatalf("applied DDL must resume its started marker: %v", err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	var phase string
	if err := db.QueryRow("SELECT phase FROM schema_migration WHERE migration_id=?", first.ID).Scan(&phase); err != nil || phase != "completed" {
		t.Fatal("interrupted marker was not completed")
	}
	if _, err := db.Exec("UPDATE schema_migration SET checksum=? WHERE migration_id=?", strings.Repeat("0", 64), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAdditivePlan(ctx, db, addition); err == nil {
		t.Fatal("changed historical additive checksum accepted")
	}
}

func TestAccountCenterUpgradeAcceptsFreshSchemaWithoutImportMetadata(t *testing.T) {
	db, _, old, addition := accountCenterFixture(t)
	if _, err := db.Exec("DROP TABLE schema_migration"); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAdditivePlan(context.Background(), db, addition)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(context.Background(), db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	var imported int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migration WHERE migration_id=?", dataMigrationID).Scan(&imported); err != nil || imported != 0 {
		t.Fatal("fresh additive upgrade must not fabricate a historical import marker")
	}
}

func TestAccountCenterUpgradeRejectsWrongExistingStructure(t *testing.T) {
	db, _, _, addition := accountCenterFixture(t)
	if _, err := db.Exec("ALTER TABLE user_account ADD avatar_version INT NOT NULL DEFAULT 0"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAdditivePlan(context.Background(), db, addition); err == nil || !strings.Contains(err.Error(), "definition conflict") {
		t.Fatal("same-name incompatible avatar pointer accepted")
	}
}

func TestAccountCenterUpgradeRejectsUnparsedSQLBeforePlanning(t *testing.T) {
	db, _, _, addition := accountCenterFixture(t)
	addition.Steps[0].SQL = "DROP TABLE user_account"
	addition.Steps[0].Checksum = digest([]byte(addition.Steps[0].SQL))
	if _, err := BuildAdditivePlan(context.Background(), db, addition); err == nil {
		t.Fatal("additive API accepted destructive SQL with a self-consistent checksum")
	}
}

func TestAccountCenterUpgradeRejectsSchemaAndMarkerPlanDrift(t *testing.T) {
	db, _, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	plan, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE login_token ADD fixture_extra BIGINT NULL"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err == nil || !strings.Contains(err.Error(), "plan drift") {
		t.Fatal("schema changed after planning but old hash was accepted")
	}
	plan, err = BuildAdditivePlan(ctx, db, addition)
	if err != nil {
		t.Fatal(err)
	}
	first := addition.Steps[0]
	if _, err := db.Exec("INSERT INTO schema_migration (migration_id,checksum,phase,started_at,summary_json) VALUES (?,?,'started',NOW(6),'{}')", first.ID, first.Checksum); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err == nil || !strings.Contains(err.Error(), "plan drift") {
		t.Fatal("stage marker changed after planning but old hash was accepted")
	}
}

func TestAccountCenterUpgradeRejectsMissingCompletedStructureAndUnknownPhase(t *testing.T) {
	db, _, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	plan, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	first := addition.Steps[0]
	if _, err := db.Exec("UPDATE schema_migration SET phase='unexpected' WHERE migration_id=?", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAdditivePlan(ctx, db, addition); err == nil || !strings.Contains(err.Error(), "unknown DDL phase") {
		t.Fatal("unknown stage marker was accepted")
	}
	if _, err := db.Exec("UPDATE schema_migration SET phase='completed' WHERE migration_id=?", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE user_account DROP avatar_version"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAdditivePlan(ctx, db, addition); err == nil || !strings.Contains(err.Error(), "completed DDL has missing structure") {
		t.Fatal("completed stage marker concealed a missing column")
	}
}

func TestSessionMetadataMaintenanceUsesExpiryAndKeepsTokensAndAudit(t *testing.T) {
	db, _, old, addition := accountCenterFixture(t)
	ctx := context.Background()
	plan, err := BuildAdditivePlan(ctx, db, addition)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyAdditive(ctx, db, addition, plan.SHA256, old.Database, true); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 1003; i++ {
		expires := now.Add(-31 * 24 * time.Hour)
		if i == 1001 {
			expires = now.Add(-30 * 24 * time.Hour)
		} else if i == 1002 {
			expires = now.Add(-29 * 24 * time.Hour)
		} else if i == 1003 {
			expires = now.Add(time.Hour)
		}
		if _, err := db.Exec("INSERT INTO login_token (id,user_id,token,is_expired,expire_time,login_ip,user_agent,client_name,os_name,device_type,login_source) VALUES (?,123,'kept-token',1,?,'198.51.100.4','synthetic-private-agent','browser','system','desktop','web')", 10000+i, expires); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO admin_audit_log (actor_user_id,action,target_type,target_id,before_summary,after_summary,reason,create_time) VALUES (123,'fixture','user',123,'before','after','synthetic',NOW(6))"); err != nil {
		t.Fatal(err)
	}
	var auditBefore int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_audit_log").Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	result, err := ClearSessionMetadata(ctx, db, old.Database, now)
	if err != nil || result.ClearedRows != 1000 || result.Batches != 2 {
		t.Fatalf("cleanup must clear 500-row batches strictly before expiry + 30 days: %+v, %v", result, err)
	}
	var rows, audit, retained int
	if err := db.QueryRow("SELECT COUNT(*) FROM login_token WHERE token='kept-token'").Scan(&rows); err != nil || rows != 1003 {
		t.Fatal("maintenance deleted authentication rows or changed token contents")
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_audit_log").Scan(&audit); err != nil || audit != auditBefore {
		t.Fatal("maintenance changed audit rows")
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM login_token WHERE login_ip='198.51.100.4'").Scan(&retained); err != nil || retained != 3 {
		t.Fatal("maintenance cleared a boundary, recent or future session")
	}
	result, err = ClearSessionMetadata(ctx, db, old.Database, now)
	if err != nil || result.ClearedRows != 0 {
		t.Fatalf("repeat maintenance must be idempotent: %+v, %v", result, err)
	}
	raw, err := json.Marshal(result)
	if err != nil || strings.Contains(string(raw), "private-agent") || strings.Contains(string(raw), "198.51.100") {
		t.Fatal("maintenance report exposed private session metadata")
	}
}
