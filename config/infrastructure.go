package config

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

type RedisConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Address          string `yaml:"address"`
	DB               int    `yaml:"db"`
	Username         string `yaml:"username"`
	PasswordFile     string `yaml:"password_file"`
	KeyPrefix        string `yaml:"key_prefix"`
	DialTimeoutMS    int    `yaml:"dial_timeout_ms"`
	CommandTimeoutMS int    `yaml:"command_timeout_ms"`
	PoolSize         int    `yaml:"pool_size"`
}

type CacheConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Namespaces []string `yaml:"namespaces"`
}

type AnalyticsConfig struct {
	Enabled              bool   `yaml:"enabled"`
	Timezone             string `yaml:"timezone"`
	RetentionDays        int    `yaml:"retention_days"`
	HotDays              int    `yaml:"hot_days"`
	FlushIntervalSeconds int    `yaml:"flush_interval_seconds"`
	QueueSize            int    `yaml:"queue_size"`
	VisitorHMACKeyFile   string `yaml:"visitor_hmac_key_file"`
}

// NormalizeInfrastructure validates only the new opt-in infrastructure settings.
// It supplies disabled defaults for old installations and never reads a secret
// or changes Redis/MySQL state. Call before creating clients or serving traffic.
func NormalizeInfrastructure(conf *Config) error {
	r, a := &conf.Redis, &conf.Analytics
	if r.Address == "" {
		r.Address = "127.0.0.1:6379"
	}
	if r.KeyPrefix == "" {
		r.KeyPrefix = "hs:prod"
	}
	if r.DialTimeoutMS == 0 {
		r.DialTimeoutMS = 200
	}
	if r.CommandTimeoutMS == 0 {
		r.CommandTimeoutMS = 20
	}
	if r.PoolSize == 0 {
		r.PoolSize = 8
	}
	host, port, err := net.SplitHostPort(r.Address)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host == "" || number < 1 || number > 65535 {
		return fmt.Errorf("redis.address must be a host:port")
	}
	if r.DB < 0 || r.DB > 15 || r.DialTimeoutMS < 1 || r.DialTimeoutMS > 5000 || r.CommandTimeoutMS < 1 || r.CommandTimeoutMS > 1000 || r.PoolSize < 1 || r.PoolSize > 64 {
		return fmt.Errorf("invalid Redis database, timeout or pool size")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_.-]{0,79}$`).MatchString(r.KeyPrefix) {
		return fmt.Errorf("invalid Redis key prefix")
	}
	if r.PasswordFile != "" && !filepath.IsAbs(r.PasswordFile) {
		return fmt.Errorf("redis.password_file must be absolute")
	}
	if (conf.Cache.Enabled || a.Enabled) && !r.Enabled {
		return fmt.Errorf("cache and analytics require redis.enabled")
	}
	known := map[string]bool{"book": true, "book_address": true, "web_release": true, "app_token": true}
	if conf.Cache.Namespaces == nil {
		conf.Cache.Namespaces = []string{"book", "book_address", "web_release", "app_token"}
	}
	seen := make(map[string]bool)
	for _, name := range conf.Cache.Namespaces {
		if !known[name] || seen[name] {
			return fmt.Errorf("unknown or duplicate cache namespace")
		}
		seen[name] = true
	}
	if a.Timezone == "" {
		a.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(a.Timezone); err != nil || a.Timezone != "Asia/Shanghai" {
		return fmt.Errorf("invalid analytics timezone")
	}
	if a.RetentionDays == 0 {
		a.RetentionDays = 90
	}
	if a.HotDays == 0 {
		a.HotDays = 3
	}
	if a.FlushIntervalSeconds == 0 {
		a.FlushIntervalSeconds = 60
	}
	if a.QueueSize == 0 {
		a.QueueSize = 4096
	}
	if a.RetentionDays != 90 || a.HotDays < 1 || a.HotDays > a.RetentionDays || a.FlushIntervalSeconds < 1 || a.FlushIntervalSeconds > 60 || a.QueueSize < 1 || a.QueueSize > 65536 {
		return fmt.Errorf("invalid analytics retention, hot window, flush interval or queue size")
	}
	if a.Enabled && (a.VisitorHMACKeyFile == "" || !filepath.IsAbs(a.VisitorHMACKeyFile)) {
		return fmt.Errorf("analytics.visitor_hmac_key_file must be an absolute private file")
	}
	return nil
}
