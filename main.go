package main

import (
	"flag"
	"github.com/mcoder2014/home_server/route"
	"github.com/sirupsen/logrus"
	"strconv"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

	config := GetConfig()

	r := route.InitRoute()
	// listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
	if err :=r.Run(":"+strconv.FormatInt(config.Port, 10)); err!=nil {
		panic(err)
	}
}

type CliArgsConfig struct {
	Port int64
}

func GetConfig() *CliArgsConfig {
    config := &CliArgsConfig{}

	// StringVar用指定的名称、控制台参数项目、默认值、使用信息注册一个string类型flag，并将flag的值保存到p指向的变量
	flag.Int64Var(&config.Port, "P", 8080, "http server 端口号,默认为空")

	// 从arguments中解析注册的flag。必须在所有flag都注册好而未访问其值时执行。未注册却使用flag -help时，会返回ErrHelp。
	flag.Parse()

	logrus.Infof("Got Cli Args: %v", *config)
	return config
}