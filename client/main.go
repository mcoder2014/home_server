package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/client/biz/ddns"
	"github.com/mcoder2014/home_server/client/config"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors:   false,
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000000",
	})

	cliConfig := GetConfig()
	err := config.InitGlobalConfig(cliConfig.ConfigPath)
	if err != nil {
		logrus.WithError(err).Errorf("config.InitGlobalConfig failed")
		os.Exit(1)
	}

	logrus.Infof("load config:%+v", config.Global())

	// 监听 ctrl c 信号
	exitChan := make(chan os.Signal)
	signal.Notify(exitChan, os.Interrupt, os.Kill, syscall.SIGTERM)

	// 业务 routine
	go ddns.StartDDNSRoutine()

	// 阻塞主线程
	exitHandle(exitChan)
	logrus.Infof("program end.")
}

func exitHandle(exitChan chan os.Signal) {
	for {
		select {
		case sig := <-exitChan:
			logrus.Infof("Get Signal: %v from sys, stop program", sig)
			time.Sleep(3 * time.Second)
			logrus.Infof("Force Exit")
			os.Exit(1) //如果ctrl+c 关不掉程序，使用os.Exit强行关掉
		}
	}
}

type CliArgsConfig struct {

	// 配置文件路径
	ConfigPath string
}

// GetConfig 读取命令行配置
func GetConfig() *CliArgsConfig {
	conf := &CliArgsConfig{}

	flag.StringVar(&conf.ConfigPath, "conf", "/etc/home_server/conf.yaml", "配置文件路径")

	// 从arguments中解析注册的flag。必须在所有flag都注册好而未访问其值时执行。未注册却使用flag -help时，会返回ErrHelp。
	flag.Parse()

	logrus.Infof("Got Cli Args: %v", *conf)
	return conf
}
