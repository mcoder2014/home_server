package service

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
)

func Init(conf *config.Config) error {
	return passport.Init(conf)
}
