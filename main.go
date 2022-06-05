package main

import (
	"flag"
	"os"
	"strconv"

	"github.com/mcoder2014/home_server/utils/routine"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
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
		os.Exit(1)
	}
	logrus.Infof("Load Config: %+v", config.Global())

	routine.Init()

	// 链接数据库
	err = db.InitDatabase(config.Global().Mysql.MasterDB)
	if err != nil {
		logrus.WithError(err).Errorf("connect to mysql failed. dsn is %v ", config.Global().Mysql.MasterDB)
		os.Exit(1)
	}

	r := route.InitRoute()

	port := cliConfig.Port
	if port == -1 {
		port = config.Global().Server.Port
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
	conf := &CliArgsConfig{}

	flag.IntVar(&conf.Port, "port", -1, "http server 端口号,默认为空")
	flag.StringVar(&conf.ConfigPath, "conf", "/etc/home_server/conf.yaml", "配置文件路径")

	// 从arguments中解析注册的flag。必须在所有flag都注册好而未访问其值时执行。未注册却使用flag -help时，会返回ErrHelp。
	flag.Parse()

	logrus.Infof("Got Cli Args: %v", *conf)
	return conf
}
