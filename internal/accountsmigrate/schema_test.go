package accountsmigrate

import "testing"

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
