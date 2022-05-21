package config

import (
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Server struct {
		Port int `json:"port" yaml:"port"`
	} `json:"server" yaml:"server"`

	RPC struct {
		// 聚美数智的 api 接口秘钥
		// https://market.aliyun.com/products/57126001/cmapi00053669.html?spm=5176.730005.result.6.70ff35249MqvXk&innerSource=search_isbn#sku=yuncode4766900008
		JmIsbn struct {
			AppCode string `json:"app_code" yaml:"app_code"`
		} `json:"jmisbn" yaml:"jmisbn"`

		Cloudflare struct {
			APIKey    string `json:"api_key" yaml:"api_key"`
			ZoneID    string `json:"zone_id" yaml:"zone_id"`
			AccountID string `json:"account_id" yaml:"account_id"`
		} `json:"cloudflare" yaml:"cloudflare"`
	} `json:"rpc" yaml:"rpc"`

	Mysql struct {
		MasterDB string `json:"master_db" yaml:"master_db"`
	} `json:"mysql" yaml:"mysql"`
}

var globalConfig = Config{}

func Global() Config {
	return globalConfig
}

func SetGlobalConfig(c Config) {
	globalConfig = c
}

func InitGlobalConfig(filepath string) error {
	err := utils.BindConfig(filepath, &globalConfig)
	if err != nil {
		return err
	}
	logrus.Infof("InitGlobalConfig config file path: %v", filepath)
	return nil
}
