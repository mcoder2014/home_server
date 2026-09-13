package service

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/applications"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/webdav"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
)

func Init(conf *config.Config) error {
	if err := passport.Init(conf); err != nil {
		return err
	}
	if err := webprojects.Init(&conf.WebProjects); err != nil {
		return err
	}
	if conf.WebProjects.Enabled {
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
	webprojects.StartMaintenance(conf.WebProjects)
	return nil
}
