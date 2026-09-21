package siteconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
)

func TestStoredSessionPolicyDefaultsOnlyTheKnownMissingField(t *testing.T) {
	conf := config.Config{}
	service := New(nil, conf)
	legacy := config.DefaultRuntimeValues(conf)["account_policy"]
	delete(legacy, "max_active_sessions")
	raw, _ := json.Marshal(legacy)
	hash := sha256.Sum256(raw)
	row := model.SiteConfigCurrent{Namespace: "account_policy", Revision: 1, SchemaVersion: 1, ValuesJSON: string(raw), ValuesSHA256: hash[:]}
	values, err := service.checkedValues(row)
	if err != nil || values["max_active_sessions"] != int64(5) {
		t.Fatalf("valid legacy policy must read with website session limit 5: %v %v", values, err)
	}
	if row.ValuesJSON != string(raw) || !bytes.Equal(row.ValuesSHA256, hash[:]) {
		t.Fatal("read compatibility modified stored JSON or checksum")
	}
	for _, change := range []struct {
		name string
		edit func(map[string]interface{})
	}{
		{"other missing key", func(v map[string]interface{}) { delete(v, "min_password_length") }},
		{"unknown key", func(v map[string]interface{}) { v["unknown_policy"] = 5 }},
		{"explicit null", func(v map[string]interface{}) { v["max_active_sessions"] = nil }},
		{"explicit zero", func(v map[string]interface{}) { v["max_active_sessions"] = 0 }},
		{"explicit excessive limit", func(v map[string]interface{}) { v["max_active_sessions"] = 101 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			candidate := config.DefaultRuntimeValues(conf)["account_policy"]
			delete(candidate, "max_active_sessions")
			change.edit(candidate)
			data, _ := json.Marshal(candidate)
			digest := sha256.Sum256(data)
			invalid := row
			invalid.ValuesJSON, invalid.ValuesSHA256 = string(data), digest[:]
			if _, err := service.checkedValues(invalid); err == nil {
				t.Fatal("legacy read compatibility accepted an invalid document")
			}
		})
	}
	nullSHA := sha256.Sum256([]byte("null"))
	for _, invalid := range []model.SiteConfigCurrent{
		{Namespace: "account_policy", Revision: 1, SchemaVersion: 1, ValuesJSON: "null", ValuesSHA256: nullSHA[:]},
		{Namespace: "account_policy", Revision: 1, SchemaVersion: 1, ValuesJSON: string(raw), ValuesSHA256: make([]byte, 32)},
	} {
		if _, err := service.checkedValues(invalid); err == nil {
			t.Fatal("legacy read compatibility bypassed document shape or checksum verification")
		}
	}
}

func TestStoredSessionPolicyIsReadOnlyAndLegacyRollbackAddsDefault(t *testing.T) {
	service, database := runtimeTestService(t)
	ctx := context.Background()
	legacy := config.DefaultRuntimeValues(service.bootstrap)["account_policy"]
	delete(legacy, "max_active_sessions")
	raw, _ := json.Marshal(legacy)
	hash := sha256.Sum256(raw)
	for _, table := range []string{"site_config_current", "site_config_history"} {
		if err := database.Exec("UPDATE "+table+" SET values_json=?,values_sha256=? WHERE namespace='account_policy' AND revision=1", string(raw), hash[:]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if config.Runtime().AccountPolicy.MaxActiveSessions != 5 {
		t.Fatal("legacy database startup did not receive default website session limit")
	}
	view, err := service.Get(ctx, "account_policy")
	if err != nil || view.Values["max_active_sessions"] != int64(5) {
		t.Fatalf("legacy settings view omitted the default: %v %v", view, err)
	}
	var stored model.SiteConfigCurrent
	if err := database.Table("site_config_current").Where("namespace='account_policy'").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || stored.ValuesJSON != string(raw) || !bytes.Equal(stored.ValuesSHA256, hash[:]) {
		t.Fatal("reading legacy settings wrote defaults into the database")
	}
	candidate := config.DefaultRuntimeValues(service.bootstrap)["account_policy"]
	candidate["max_active_sessions"] = 3
	published, err := service.Publish(ctx, "account_policy", 1, 1, PublishRequest{RequestID: "website-session-limit", Reason: "synthetic limit update", Values: candidate}, allowConfigTest)
	if err != nil || published.Revision != 2 || config.Runtime().AccountPolicy.MaxActiveSessions != 3 {
		t.Fatalf("new website session policy was not applied: %v %v", published, err)
	}
	rolledBack, err := service.Rollback(ctx, "account_policy", 1, 2, RollbackRequest{TargetRevision: 1, RequestID: "website-session-limit-rollback", Reason: "synthetic legacy rollback"}, allowConfigTest)
	if err != nil || rolledBack.Revision != 3 || rolledBack.Values["max_active_sessions"] != int64(5) || config.Runtime().AccountPolicy.MaxActiveSessions != 5 {
		t.Fatalf("legacy rollback did not publish a complete document with limit 5: %v %v", rolledBack, err)
	}
	var historical model.SiteConfigHistory
	if err := database.Table("site_config_history").Where("namespace='account_policy' AND revision=1").Take(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if historical.ValuesJSON != string(raw) || !bytes.Equal(historical.ValuesSHA256, hash[:]) {
		t.Fatal("legacy rollback rewrote historical JSON or checksum")
	}
}

func TestStoredLegacyPasswordPolicyKeepsOriginalDocument(t *testing.T) {
	service := New(nil, config.Config{})
	values := config.DefaultRuntimeValues(config.Config{})["account_policy"]
	delete(values, "min_share_password_length")
	delete(values, "share_code_length")
	raw, _ := json.Marshal(values)
	hash := sha256.Sum256(raw)
	row := model.SiteConfigCurrent{Namespace: "account_policy", Revision: 1, SchemaVersion: 1, ValuesJSON: string(raw), ValuesSHA256: hash[:]}
	actual, err := service.checkedValues(row)
	if err != nil || actual["min_share_password_length"] != int64(8) || actual["share_code_length"] != int64(6) {
		t.Fatalf("legacy defaults: %v %v", actual, err)
	}
	if row.ValuesJSON != string(raw) || !bytes.Equal(row.ValuesSHA256, hash[:]) {
		t.Fatal("legacy stored policy changed")
	}
	for _, key := range []string{"min_share_password_length", "share_code_length"} {
		values[key] = 3
		raw, _ = json.Marshal(values)
		hash = sha256.Sum256(raw)
		row.ValuesJSON, row.ValuesSHA256 = string(raw), hash[:]
		if _, err := service.checkedValues(row); err == nil {
			t.Fatalf("invalid stored %s accepted", key)
		}
		delete(values, key)
	}
}
