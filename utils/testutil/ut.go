package testutil

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/database"
)

func Init() error {
	configPath := "config/config.yaml"

	// 读取 config 信息
	err := config.InitGlobalConfig(configPath)
	if err != nil {
		return err
	}

	// 链接数据库
	err = database.InitDatabase(config.Global().Mysql.MasterDB)
	if err != nil {
		return err
	}
	return nil
}
