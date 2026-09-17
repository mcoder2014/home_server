package accountsmigrate

import (
	"testing"

	"github.com/mcoder2014/home_server/domain/dal/migrations"
)

func TestDDLIsSplitIntoRecoverableSteps(t *testing.T) {
	ddl := []byte("-- note\nCREATE TABLE account_fixture (id BIGINT NOT NULL PRIMARY KEY, value VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', UNIQUE KEY uk_value (value)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4; ALTER TABLE old_fixture ADD COLUMN revision BIGINT NOT NULL DEFAULT 1, ADD KEY idx_revision (revision);")
	steps, err := ParseDDL("fixture.sql", ddl)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("want three independently tracked DDL steps, got %d", len(steps))
	}
	if steps[0].Table != "account_fixture" || len(steps[0].Columns) != 2 || len(steps[0].Indexes) != 2 {
		t.Fatal("CREATE definitions must be verified")
	}
	if steps[1].Columns[0].Name != "revision" || steps[2].Indexes[0].Name != "idx_revision" {
		t.Fatal("ALTER definitions missing")
	}
	if steps[0].Checksum == "" || steps[0].ID == steps[1].ID {
		t.Fatal("steps must have checksums and unique IDs")
	}
}

func TestDDLRejectsDestructiveOrUnparsedOperations(t *testing.T) {
	for _, input := range []string{"DROP TABLE users;", "ALTER TABLE users DROP COLUMN x;", "UPDATE users SET x=1;", "CREATE TABLE t (value GEOMETRY NOT NULL);"} {
		if _, err := ParseDDL("bad.sql", []byte(input)); err == nil {
			t.Fatalf("unsafe or unsupported DDL accepted: %s", input)
		}
	}
}

func TestBooleanDefaultsMatchMariaDB(t *testing.T) {
	steps, err := ParseDDL("bool.sql", []byte("CREATE TABLE bool_fixture (enabled BOOLEAN NOT NULL DEFAULT FALSE) ENGINE=InnoDB;"))
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Columns[0].Default == nil || *steps[0].Columns[0].Default != "0" {
		t.Fatal("BOOLEAN FALSE must match MariaDB numeric default")
	}
}

func TestDDLParsesBoundedAvatarBlob(t *testing.T) {
	steps, err := ParseDDL("avatar.sql", []byte("CREATE TABLE user_avatar (user_id BIGINT NOT NULL PRIMARY KEY,content_blob MEDIUMBLOB NOT NULL) ENGINE=InnoDB;"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Columns[1].Type != "mediumblob" || steps[0].Columns[1].Nullable {
		t.Fatal("avatar content must preserve the non-null MEDIUMBLOB definition")
	}
}

func TestHistoricalAccountCreateAllowsOnlyReviewedAvatarVersion(t *testing.T) {
	raw, err := migrations.SQL.ReadFile("20260913_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParseDDL("20260913_accounts.sql", raw)
	if err != nil {
		t.Fatal(err)
	}
	step := steps[0]
	zero := "0"
	avatar := Column{Name: "avatar_version", Type: "bigint", Default: &zero}
	schema := &TableSchema{Name: step.Table, Engine: "InnoDB", Columns: append(append([]Column{}, step.Columns...), avatar), Indexes: step.Indexes, Checks: step.Checks}
	if complete, err := verifyStep(step, schema); err != nil || !complete {
		t.Fatalf("reviewed additive avatar pointer must remain compatible with historical migration: %v", err)
	}
	for _, invalid := range []Column{{Name: "unknown", Type: "bigint", Default: &zero}, {Name: "avatar_version", Type: "int", Default: &zero}, {Name: "avatar_version", Type: "bigint", Nullable: true, Default: &zero}, {Name: "avatar_version", Type: "bigint"}} {
		schema.Columns[len(schema.Columns)-1] = invalid
		if _, err := verifyStep(step, schema); err == nil {
			t.Fatalf("historical CREATE accepted unknown or incompatible addition: %+v", invalid)
		}
	}
	schema.Columns[len(schema.Columns)-1] = avatar
	schema.Columns = append(schema.Columns, avatar)
	if _, err := verifyStep(step, schema); err == nil {
		t.Fatal("historical CREATE accepted duplicate additions")
	}
}
