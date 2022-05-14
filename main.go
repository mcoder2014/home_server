package main

import (
	"flag"
	"strconv"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/route"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

	cliConfig := GetConfig()
	err := config.InitGlobalConfig(cliConfig.ConfigPath)
	if err != nil {
		logrus.WithError(err).Errorf("load Global config from :%v failed.", cliConfig.ConfigPath)
	}
	logrus.Infof("Load Config: %+v", config.GlobalConfig())

	r := route.InitRoute()

	port := cliConfig.Port
	if port == -1 {
		port = config.GlobalConfig().Server.Port
	}
	logrus.Infof("will bind http server port on %v", port)

	if err = r.Run(":" + strconv.Itoa(port)); err != nil {
		panic(err)
	}
}

type CliArgsConfig struct {
	// 配置端口号
	Port int
	// 配置文件路径
	ConfigPath string
}

// GetConfig 读取命令行配置
func GetConfig() *CliArgsConfig {
	config := &CliArgsConfig{}

	flag.IntVar(&config.Port, "port", -1, "http server 端口号,默认为空")
	flag.StringVar(&config.ConfigPath, "config", "/etc/home_server/config.yaml", "配置文件路径")

	// 从arguments中解析注册的flag。必须在所有flag都注册好而未访问其值时执行。未注册却使用flag -help时，会返回ErrHelp。
	flag.Parse()

	logrus.Infof("Got Cli Args: %v", *config)
	return config
}
