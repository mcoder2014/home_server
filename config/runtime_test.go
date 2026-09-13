package config

import (
	"testing"
)

func TestRuntimeRejectsInvalidAndUndeclaredValues(t *testing.T) {
	conf := Config{}
	values := DefaultRuntimeValues(conf)
	if len(values) != 7 {
		t.Fatalf("expected seven registered namespaces, got %d", len(values))
	}
	for _, change := range []struct {
		namespace, key string
		value          interface{}
	}{
		{"registration", "enabled", "true"}, {"registration", "monthly_limit", 4},
		{"auth", "max_applications_per_user", 101}, {"auth", "site_origin", "https://attacker.example"},
		{"web_projects", "max_upload_bytes", int64(51 << 20)}, {"account_policy", "bcrypt_cost", 20},
	} {
		copyValues := DefaultRuntimeValues(conf)[change.namespace]
		copyValues[change.key] = change.value
		if _, err := ValidateValues(conf, change.namespace, copyValues); err == nil {
			t.Errorf("accepted invalid %s.%s", change.namespace, change.key)
		}
	}
	values["auth"]["default_credential_ttl_days"] = 366
	if _, err := ValidateValues(conf, "auth", values["auth"]); err == nil {
		t.Fatal("accepted credential default larger than maximum")
	}
}

func TestRuntimeSnapshotIsCompleteImmutableAndMonotonic(t *testing.T) {
	conf := Config{}
	conf.Auth.SiteOrigins = []string{"https://home.example.com"}
	conf.WebProjects.StorageRoot = "/private/example"
	values := DefaultRuntimeValues(conf)
	revisions := map[string]int64{}
	for name := range values {
		revisions[name] = 1
	}
	snapshot, err := BuildRuntimeSnapshot(conf, 10, revisions, values)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Auth.SiteOrigins[0] = "https://attacker.example"
	snapshot.Revisions["site"] = 99
	loaded := Runtime()
	if loaded.Auth.SiteOrigins[0] != "https://home.example.com" || loaded.Revisions["site"] != 1 || loaded.WebProjects.StorageRoot != "/private/example" {
		t.Fatal("snapshot retained mutable data or discarded bootstrap path")
	}
	loaded.Auth.SiteOrigins[0] = "https://mutated.example"
	if Runtime().Auth.SiteOrigins[0] != "https://home.example.com" {
		t.Fatal("Runtime returned shared slices")
	}
	snapshot.Generation = 9
	if err := StoreRuntimeSnapshot(snapshot); err == nil {
		t.Fatal("accepted an older generation")
	}
	delete(values, "registration")
	if _, err := BuildRuntimeSnapshot(conf, 11, revisions, values); err == nil {
		t.Fatal("accepted an incomplete runtime snapshot")
	}
}

func TestDefaultRuntimeValuesPreserveImportedLimitsAndHideSecrets(t *testing.T) {
	conf := Config{}
	conf.Auth.TokenTTLSeconds = 1800
	conf.Auth.SiteOrigin = "https://home.example.com"
	conf.WebProjects.MaxUploadBytes = 8 << 20
	conf.WebProjects.MaxFileBytes = 8 << 20
	conf.WebProjects.StorageRoot = "/private/storage"
	values := DefaultRuntimeValues(conf)
	if values["auth"]["token_ttl_seconds"] != 1800 || values["web_projects"]["max_upload_bytes"] != int64(8<<20) {
		t.Fatal("discarded imported limits")
	}
	if _, ok := values["auth"]["site_origin"]; ok {
		t.Fatal("exported bootstrap origin")
	}
	if _, ok := values["web_projects"]["storage_root"]; ok {
		t.Fatal("exported storage path")
	}
	if values["registration"]["enabled"] != false {
		t.Fatal("enabled registration while importing")
	}
}

func TestBootstrapChangeInvalidatesPreviousSnapshot(t *testing.T) {
	original := Global()
	t.Cleanup(func() { SetGlobalConfig(original) })
	conf := Config{}
	SetGlobalConfig(conf)
	values := DefaultRuntimeValues(conf)
	revisions := map[string]int64{}
	for namespace := range values {
		revisions[namespace] = 1
	}
	values["site"]["title"] = "Previous database title"
	snapshot, err := BuildRuntimeSnapshot(conf, 7, revisions, values)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	conf.Auth.SiteOrigin = "https://replacement.example.com"
	SetGlobalConfig(conf)
	current := Runtime()
	if current.SiteTitle != "CQ Home Server" || current.Auth.SiteOrigin != "https://replacement.example.com" || current.Generation != 0 {
		t.Fatal("bootstrap replacement retained previous database snapshot")
	}
}

func TestResourceQuotaSchemaDefaultsAndRelationships(t *testing.T) {
	conf := Config{}
	values := DefaultRuntimeValues(conf)["web_projects"]
	if values["max_projects_per_user"] != 10 || values["max_user_bytes"] != int64(10<<30) || values["min_free_disk_bytes"] != int64(20<<30) {
		t.Fatal("resource quota defaults are missing")
	}
	values["max_user_bytes"] = int64(1)
	if _, err := ValidateValues(conf, "web_projects", values); err == nil {
		t.Fatal("per-user quota accepted below per-project quota")
	}
}
