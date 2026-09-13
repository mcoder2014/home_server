package config

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

type AccountPolicyConfig struct {
	SessionTTLSeconds        int `json:"session_ttl_seconds"`
	TemporaryPasswordTTLDays int `json:"temporary_password_ttl_days"`
	MinPasswordLength        int `json:"min_password_length"`
	BcryptCost               int `json:"bcrypt_cost"`
}

// RuntimeConfig is copied on load and publish. Storage and trust boundaries are
// always taken from bootstrap configuration, never from administrator values.
type RuntimeConfig struct {
	Auth                                                               AuthConfig
	WebProjects                                                        WebProjectsConfig
	RegistrationEnabled, LibraryEnabled, WebDAVEnabled, CleanupEnabled bool
	SiteTitle, SiteNotice                                              string
	AccountPolicy                                                      AccountPolicyConfig
	Generation                                                         int64
	Revisions                                                          map[string]int64
}

var runtimeSnapshot atomic.Pointer[RuntimeConfig]
var runtimeLock sync.Mutex

func BuildRuntimeSnapshot(conf Config, generation int64, revisions map[string]int64, values map[string]map[string]interface{}) (RuntimeConfig, error) {
	if len(values) != len(Registry(conf)) || generation < 0 {
		return RuntimeConfig{}, fmt.Errorf("runtime configuration is incomplete")
	}
	validated := make(map[string]map[string]interface{}, len(values))
	for _, schema := range Registry(conf) {
		if generation > 0 && revisions[schema.Namespace] <= 0 {
			return RuntimeConfig{}, fmt.Errorf("missing revision for %s", schema.Namespace)
		}
		v, err := ValidateValues(conf, schema.Namespace, values[schema.Namespace])
		if err != nil {
			return RuntimeConfig{}, err
		}
		validated[schema.Namespace] = v
	}
	result := RuntimeConfig{Auth: conf.Auth, WebProjects: conf.WebProjects, Generation: generation, Revisions: revisions}
	for namespace, target := range map[string]interface{}{"auth": &result.Auth, "web_projects": &result.WebProjects, "account_policy": &result.AccountPolicy} {
		data, err := json.Marshal(validated[namespace])
		if err != nil {
			return RuntimeConfig{}, err
		}
		if err := json.Unmarshal(data, target); err != nil {
			return RuntimeConfig{}, err
		}
	}
	result.RegistrationEnabled = validated["registration"]["enabled"].(bool)
	result.LibraryEnabled = validated["library"]["enabled"].(bool)
	result.WebDAVEnabled = validated["webdav"]["enabled"].(bool)
	result.CleanupEnabled = result.WebProjects.CleanupEnabled
	result.SiteTitle = validated["site"]["title"].(string)
	result.SiteNotice = validated["site"]["notice"].(string)
	return cloneRuntime(result), nil
}

func StoreRuntimeSnapshot(snapshot RuntimeConfig) error {
	runtimeLock.Lock()
	defer runtimeLock.Unlock()
	current := runtimeSnapshot.Load()
	if current != nil && current.Generation > snapshot.Generation {
		return fmt.Errorf("refusing runtime generation regression")
	}
	copy := cloneRuntime(snapshot)
	runtimeSnapshot.Store(&copy)
	return nil
}

func Runtime() RuntimeConfig {
	if snapshot := runtimeSnapshot.Load(); snapshot != nil {
		return cloneRuntime(*snapshot)
	}
	conf := Global()
	if conf.ConfigSource == "database" {
		return RuntimeConfig{} // Startup must load database configuration before serving.
	}
	result, err := BuildRuntimeSnapshot(conf, 0, nil, DefaultRuntimeValues(conf))
	if err != nil {
		return RuntimeConfig{} // Invalid limits must not silently enable a module.
	}
	return result
}

func cloneRuntime(value RuntimeConfig) RuntimeConfig {
	value.Auth.SiteOrigins = append([]string(nil), value.Auth.SiteOrigins...)
	value.Auth.TrustedProxyCIDRs = append([]string(nil), value.Auth.TrustedProxyCIDRs...)
	revisions := make(map[string]int64, len(value.Revisions))
	for name, revision := range value.Revisions {
		revisions[name] = revision
	}
	value.Revisions = revisions
	return value
}

// DefaultRuntimeValues is an explicit migration input. Calling it never writes
// or installs defaults, and database startup never uses it as fallback data.
func DefaultRuntimeValues(conf Config) map[string]map[string]interface{} {
	values := make(map[string]map[string]interface{})
	for _, schema := range Registry(conf) {
		values[schema.Namespace] = make(map[string]interface{})
		for _, field := range schema.Fields {
			if !field.ReadOnly {
				values[schema.Namespace][field.Key] = field.DefaultValue
			}
		}
	}
	auth, _ := json.Marshal(conf.Auth)
	web, _ := json.Marshal(conf.WebProjects)
	for namespace, raw := range map[string][]byte{"auth": auth, "web_projects": web} {
		var input map[string]json.RawMessage
		_ = json.Unmarshal(raw, &input)
		for key, def := range values[namespace] {
			if key == "cleanup_enabled" {
				continue
			}
			if _, ok := def.(bool); ok {
				var value bool
				if json.Unmarshal(input[key], &value) == nil {
					values[namespace][key] = value
				}
				continue
			}
			var value int64
			if json.Unmarshal(input[key], &value) != nil || value == 0 {
				continue
			}
			switch def.(type) {
			case int:
				values[namespace][key] = int(value)
			case int64:
				values[namespace][key] = value
			}
		}
	}
	return values
}
