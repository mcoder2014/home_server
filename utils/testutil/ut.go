package testutil

import (
	"github.com/mcoder2014/home_server/config"
)

func Init() error {
	configPath := "config/config.yaml"

	// 读取 config 信息
	err := config.InitGlobalConfig(configPath)
	if err != nil {
		return err
	}
	return nil
}
