package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// HomeVirtualSubConfig is a deployment-only connection. It is never part of
// browser bootstrap or the editable runtime configuration registry.
type HomeVirtualSubConfig struct {
	Enabled   bool   `yaml:"enabled"`
	BaseURL   string `yaml:"base_url"`
	TokenFile string `yaml:"token_file"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

func NormalizeHomeVirtualSub(c *HomeVirtualSubConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.BaseURL == "" {
		c.BaseURL = "http://127.0.0.1:18190"
	}
	if c.TimeoutMS == 0 {
		c.TimeoutMS = 5000
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("home virtual sub requires a loopback HTTP address without credentials or a path")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("home virtual sub requires a literal loopback address")
	}
	if u.Port() != "" {
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid home virtual sub port")
		}
	}
	if !filepath.IsAbs(c.TokenFile) || c.TimeoutMS < 100 || c.TimeoutMS > 30000 {
		return fmt.Errorf("home virtual sub requires an absolute token_file and timeout_ms between 100 and 30000")
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	return nil
}
