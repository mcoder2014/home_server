package config

import (
	"fmt"

	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
)

type ClientConfig struct {
	DDNSConfig []*DomainConfig   `json:"ddns_config" yaml:"ddns_config"`
	Cloudflare *CloudflareConfig `json:"cloudflare" yaml:"cloudflare"`
}

// CloudflareConfig 相关的配置，用于配置域名
type CloudflareConfig struct {
	APIToken string `json:"api_token" yaml:"api_token"`
	Zone     string `json:"zone" yaml:"zone"`
	Debug    bool   `json:"debug" yaml:"debug"`
}

func (c *CloudflareConfig) String() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%+v", *c)
}

type DomainConfig struct {
	// 对应的 DDNS 域名
	Domain    string    `json:"domain" yaml:"domain"`
	IPVersion IpVersion `json:"ip_version" yaml:"ip_version"`
}

func (d *DomainConfig) String() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%+v", *d)
}

type IpVersion string

const (
	IPV4 IpVersion = "ipv4"
	IPV6           = "ipv6"
)

// 全局配置
var globalConfig = ClientConfig{}

func Global() ClientConfig {
	return globalConfig
}

func SetGlobalConfig(c ClientConfig) {
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
