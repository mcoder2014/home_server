package config

import (
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
)

type Config struct {
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
}

// WebProjectsConfig controls the isolated storage and hard safety limits for hosted web projects.
type WebProjectsConfig struct {
	Enabled                     bool   `json:"enabled" yaml:"enabled"`
	StorageRoot                 string `json:"storage_root" yaml:"storage_root"`
	SiteOrigin                  string `json:"site_origin" yaml:"site_origin"`
	MaxUploadBytes              int64  `json:"max_upload_bytes" yaml:"max_upload_bytes"`
	MaxExpandedBytes            int64  `json:"max_expanded_bytes" yaml:"max_expanded_bytes"`
	MaxFileBytes                int64  `json:"max_file_bytes" yaml:"max_file_bytes"`
	MaxFileCount                int    `json:"max_file_count" yaml:"max_file_count"`
	MaxDirectoryDepth           int    `json:"max_directory_depth" yaml:"max_directory_depth"`
	MaxProjectBytes             int64  `json:"max_project_bytes" yaml:"max_project_bytes"`
	MaxReleases                 int    `json:"max_releases" yaml:"max_releases"`
	MaxConcurrentUploadsPerUser int    `json:"max_concurrent_uploads_per_user" yaml:"max_concurrent_uploads_per_user"`
	MaxConcurrentExtracts       int    `json:"max_concurrent_extracts" yaml:"max_concurrent_extracts"`
	DeleteRetentionDays         int    `json:"delete_retention_days" yaml:"delete_retention_days"`
}

// 全局配置
var globalConfig = Config{}

func Global() Config {
	return globalConfig
}

func SetGlobalConfig(c Config) {
	globalConfig = c
}

// InitGlobalConfig 从指定配置文件中读取配置信息
func InitGlobalConfig(filepath string) error {
	err := utils.BindConfig(filepath, &globalConfig)
	if err != nil {
		return err
	}
	logrus.Infof("InitGlobalConfig config file path: %v", filepath)
	return nil
}

func ConfigPtr(c Config) *Config {
	return &c
}
