package service

import (
	"fmt"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/applications"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/webdav"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
)

// Init 校验身份与配置来源配套，初始化认证、网页存储和应用凭据服务，最后启动 WebDAV 日志消费。
func Init(conf *config.Config) error {
	if (conf.IdentitySource == "database") != (conf.ConfigSource == "database") {
		return fmt.Errorf("database identity and configuration must be enabled together")
	}
	if err := passport.Init(conf); err != nil {
		return err
	}
	if err := webprojects.Init(&conf.WebProjects); err != nil {
		return err
	}
	// Database configuration can enable web projects after startup. Storage
	// boundaries must already be valid even when the bootstrap switch is off.
	if conf.WebProjects.Enabled || conf.ConfigSource == "database" {
		if err := webprojects.ValidateStorageIsolation(conf.WebProjects.StorageRoot, conf.WebDAV.SharePath); err != nil {
			return err
		}
	}
	if err := applications.Init(conf.Auth); err != nil {
		return err
	}
	config.SetGlobalConfig(*conf)
	if err := webdav.InitLogRoutine(); err != nil {
		return err
	}
	return nil
}
