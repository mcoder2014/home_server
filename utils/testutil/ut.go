package testutil

import (
	"os"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/utils/routine"

	"github.com/mcoder2014/home_server/config"
)

// Init 读取测试配置路径，初始化协程管理和真实数据库连接；配置或数据库初始化失败时返回错误。
func Init() error {
	configPath := "config/config.yaml"

	if path, ok := os.LookupEnv("TEST_CONFIG_PATH"); ok {
		configPath = path
	}

	// go
	routine.Init()

	// 读取 config 信息
	err := config.InitGlobalConfig(configPath)
	if err != nil {
		return err
	}

	// 链接数据库
	err = db.InitDatabase(config.Global().Mysql.MasterDB)
	if err != nil {
		return err
	}
	return nil

}
