package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectInvalidInfrastructureConfig(t *testing.T) {
	before := Global()
	t.Cleanup(func() { SetGlobalConfig(before) })
	cases := map[string]string{
		"invalid Redis address":        "redis:\n  enabled: true\n  address: https://redis.example.com\n",
		"cache requires Redis":         "cache:\n  enabled: true\n",
		"unknown cache namespace":      "cache:\n  namespaces: [passwords]\n",
		"negative timeout":             "redis:\n  command_timeout_ms: -1\n",
		"invalid timezone":             "analytics:\n  timezone: Not/AZone\n",
		"retention exceeds contract":   "analytics:\n  retention_days: 91\n",
		"hot window exceeds retention": "analytics:\n  hot_days: 91\n",
		"analytics requires key":       "redis:\n  enabled: true\n  address: 127.0.0.1:16389\nanalytics:\n  enabled: true\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := InitGlobalConfig(path); err == nil {
				t.Fatal("invalid infrastructure configuration was accepted")
			}
		})
	}
}

func TestExplicitEmptyCacheNamespacesSurviveConfigurationCopies(t *testing.T) {
	before := Global()
	t.Cleanup(func() { SetGlobalConfig(before) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("redis:\n  enabled: true\ncache:\n  enabled: true\n  namespaces: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := InitGlobalConfig(path); err != nil {
		t.Fatal(err)
	}
	conf := Global()
	if err := NormalizeInfrastructure(&conf); err != nil {
		t.Fatal(err)
	}
	if conf.Cache.Namespaces == nil || len(conf.Cache.Namespaces) != 0 {
		t.Fatalf("explicit empty namespaces changed to %v", conf.Cache.Namespaces)
	}
}
