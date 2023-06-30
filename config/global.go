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
	Feishu struct {
		AppID             string `json:"app_id" yaml:"app_id"`
		AppSecret         string `json:"app_secret" yaml:"app_secret"`
		EncryptKey        string `json:"encrypt_key" yaml:"encrypt_key"`
		VerificationToken string `json:"verification_token" yaml:"verification_token"`
	} `json:"feishu" yaml:"feishu"`
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
