package config

import (
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
	"sync"
)

type Config struct {
	Redis                RedisConfig     `json:"-" yaml:"redis"`
	Cache                CacheConfig     `json:"-" yaml:"cache"`
	Analytics            AnalyticsConfig `json:"-" yaml:"analytics"`
	IdentitySource       string          `json:"identity_source" yaml:"identity_source"`
	ConfigSource         string          `json:"config_source" yaml:"config_source"`
	UploadHardLimitBytes int64           `json:"upload_hard_limit_bytes" yaml:"upload_hard_limit_bytes"`
	// 服务相关配置
	Server struct {
		// http 服务端口号
		Port int `json:"port" yaml:"port"`
	} `json:"server" yaml:"server"`

	// 依赖的服务的相关配置
	RPC struct {
		// 聚美数智的 api 接口秘钥
		// https://market.aliyun.com/products/57126001/cmapi00053669.html?spm=5176.730005.result.6.70ff35249MqvXk&innerSource=search_isbn#sku=yuncode4766900008
		JmIsbn struct {
			// 用于接口验证的 AppCode
			AppCode string `json:"app_code" yaml:"app_code"`
		} `json:"jmisbn" yaml:"jmisbn"`

		// Cloudflare 相关的配置，用于配置域名
		Cloudflare struct {
			APIKey    string `json:"api_key" yaml:"api_key"`
			APIToken  string `json:"api_token" yaml:"api_token"`
			ZoneID    string `json:"zone_id" yaml:"zone_id"`
			AccountID string `json:"account_id" yaml:"account_id"`
		} `json:"cloudflare" yaml:"cloudflare"`
	} `json:"rpc" yaml:"rpc"`

	// 数据库相关配置
	Mysql struct {
		MasterDB string `json:"master_db" yaml:"master_db"`
	} `json:"mysql" yaml:"mysql"`

	Passport struct {
		MockData          string `json:"mock_data" yaml:"mock_data"`
		RedirectLoginPath string `json:"redirect_login_path" yaml:"redirect_login_path"`
	} `json:"passport" yaml:"passport"`
	WebDAV struct {
		SharePath string `json:"share_path" yaml:"share_path"`
	} `json:"webdav" yaml:"webdav"`
	WebProjects WebProjectsConfig `json:"web_projects" yaml:"web_projects"`
	Auth        AuthConfig        `json:"auth" yaml:"auth"`
}

// AuthConfig controls machine credentials and the common browser session origin.
// Application auth is opt-in; existing user-token and Basic login remain available.
type AuthConfig struct {
	ApplicationsEnabled      bool     `json:"applications_enabled" yaml:"applications_enabled"`
	SiteOrigin               string   `json:"site_origin" yaml:"site_origin"`
	SiteOrigins              []string `json:"site_origins" yaml:"site_origins"`
	TokenTTLSeconds          int      `json:"token_ttl_seconds" yaml:"token_ttl_seconds"`
	DefaultCredentialTTLDays int      `json:"default_credential_ttl_days" yaml:"default_credential_ttl_days"`
	MaxCredentialTTLDays     int      `json:"max_credential_ttl_days" yaml:"max_credential_ttl_days"`
	MaxApplicationsPerUser   int      `json:"max_applications_per_user" yaml:"max_applications_per_user"`
	TrustedProxyCIDRs        []string `json:"trusted_proxy_cidrs" yaml:"trusted_proxy_cidrs"`
}

// WebProjectsConfig controls the isolated storage and hard safety limits for hosted web projects.
type WebProjectsConfig struct {
	CleanupEnabled              bool   `json:"cleanup_enabled" yaml:"-"`
	Enabled                     bool   `json:"enabled" yaml:"enabled"`
	StorageRoot                 string `json:"storage_root" yaml:"storage_root"`
	SiteOrigin                  string `json:"site_origin" yaml:"site_origin"`
	MaxUploadBytes              int64  `json:"max_upload_bytes" yaml:"max_upload_bytes"`
	MaxExpandedBytes            int64  `json:"max_expanded_bytes" yaml:"max_expanded_bytes"`
	MaxFileBytes                int64  `json:"max_file_bytes" yaml:"max_file_bytes"`
	MaxFileCount                int    `json:"max_file_count" yaml:"max_file_count"`
	MaxDirectoryDepth           int    `json:"max_directory_depth" yaml:"max_directory_depth"`
	MaxProjectBytes             int64  `json:"max_project_bytes" yaml:"max_project_bytes"`
	MaxProjectsPerUser          int    `json:"max_projects_per_user" yaml:"-"`
	MaxUserBytes                int64  `json:"max_user_bytes" yaml:"-"`
	MinFreeDiskBytes            int64  `json:"min_free_disk_bytes" yaml:"-"`
	MaxReleases                 int    `json:"max_releases" yaml:"max_releases"`
	MaxConcurrentUploadsPerUser int    `json:"max_concurrent_uploads_per_user" yaml:"max_concurrent_uploads_per_user"`
	MaxConcurrentExtracts       int    `json:"max_concurrent_extracts" yaml:"max_concurrent_extracts"`
	DeleteRetentionDays         int    `json:"delete_retention_days" yaml:"delete_retention_days"`
}

// 全局配置
var globalConfig = Config{}
var globalConfigLock sync.RWMutex

func Global() Config {
	globalConfigLock.RLock()
	defer globalConfigLock.RUnlock()
	result := globalConfig
	if result.Cache.Namespaces != nil {
		result.Cache.Namespaces = append([]string{}, result.Cache.Namespaces...)
	}
	result.Auth.SiteOrigins = append([]string(nil), result.Auth.SiteOrigins...)
	result.Auth.TrustedProxyCIDRs = append([]string(nil), result.Auth.TrustedProxyCIDRs...)
	return result
}

// Normalize shared configuration before any router or service consumes it.
// An unused/disabled module origin must not unexpectedly enable browser sessions.
func SetGlobalConfig(c Config) {
	if c.Cache.Namespaces != nil {
		c.Cache.Namespaces = append([]string{}, c.Cache.Namespaces...)
	}
	if c.Auth.SiteOrigin == "" && c.WebProjects.Enabled {
		c.Auth.SiteOrigin = c.WebProjects.SiteOrigin
	}
	c.Auth.SiteOrigins = append([]string(nil), c.Auth.SiteOrigins...)
	c.Auth.TrustedProxyCIDRs = append([]string(nil), c.Auth.TrustedProxyCIDRs...)
	globalConfigLock.Lock()
	globalConfig = c
	globalConfigLock.Unlock()
	runtimeLock.Lock()
	runtimeSnapshot.Store(nil)
	runtimeLock.Unlock()
}

// InitGlobalConfig 从指定配置文件中读取配置信息
func InitGlobalConfig(filepath string) error {
	var loaded Config
	if err := utils.BindConfig(filepath, &loaded); err != nil {
		return err
	}
	if err := NormalizeInfrastructure(&loaded); err != nil {
		return err
	}
	SetGlobalConfig(loaded)
	logrus.Infof("InitGlobalConfig config file path: %v", filepath)
	return nil
}

func ConfigPtr(c Config) *Config {
	return &c
}
