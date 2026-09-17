package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyMigrationRequiresReviewedOfflineConfirmations(t *testing.T) {
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"--config", "nonexistent", "--apply-migration"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "maintenance-confirmed") || out.Len() != 0 {
		t.Fatal("apply must require maintenance, exact database and reviewed plan before reading secrets")
	}
}

func TestMaintenanceRejectsMixedModesAndUnboundedTimeout(t *testing.T) {
	for _, args := range [][]string{{"--apply-migration", "--clear-session-metadata"}, {"--timeout", "0"}, {"--timeout", "2h"}, {"unexpected"}} {
		var out, errOut bytes.Buffer
		err := run(context.Background(), append([]string{"--config", "nonexistent"}, args...), &out, &errOut)
		if err == nil || out.Len() != 0 {
			t.Fatal("mixed modes, unbounded timeout or positional argument accepted")
		}
	}
}

func TestMaintenanceConfigAllowsDatabaseModeWithoutLegacyIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	secret := "fixture-private-secret"
	if err := os.WriteFile(path, []byte("identity_source: database\nconfig_source: database\nmysql:\n  master_db: 'fixture:"+secret+"@tcp(127.0.0.1:3306)/fixture'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := readConfig(path)
	if err != nil || !strings.Contains(source.Config.Mysql.MasterDB, secret) || len(source.Users) != 0 {
		t.Fatalf("maintenance must accept active database config without mock_data: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("readable secret config accepted or credential leaked")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(filepath.Dir(path), "link.yaml")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(symlink); err == nil {
		t.Fatal("symlink secret config accepted")
	}
}

func TestMaintenanceConfigErrorsSuppressYAMLContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(path, []byte("mysql: [fixture-private-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err == nil || strings.Contains(err.Error(), "fixture-private-secret") {
		t.Fatal("malformed YAML contents exposed in error")
	}
}
