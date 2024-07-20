package service

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/webdav"
)

func Init(conf *config.Config) error {
	if err := passport.Init(conf); err != nil {
		return err
	}
	return webdav.InitLogRoutine()
}
