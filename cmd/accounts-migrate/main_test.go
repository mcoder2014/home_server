package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestApplyRequiresOfflineAndPlanConfirmationsBeforeReadingSecrets(t *testing.T) {
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"--config", "nonexistent", "--binary", "nonexistent", "--apply"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "maintenance-confirmed") {
		t.Fatal("apply must require explicit offline and plan confirmations first")
	}
	if out.Len() != 0 {
		t.Fatal("failed validation must not claim successful application")
	}
}
func TestGrantIDParsingRejectsNonpositiveOrAmbiguousIDs(t *testing.T) {
	for _, value := range []string{"-1", "0", "1,,2", "owner", "123.0"} {
		if _, err := parseIDs(value); err == nil {
			t.Fatal("invalid grant ID accepted")
		}
	}
	ids, err := parseIDs(" 123, 456 ")
	if err != nil || len(ids) != 2 || ids[1] != 456 {
		t.Fatal("valid explicit grant IDs rejected")
	}
}
