package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/app/schedule"
	"github.com/mcoder2014/home_server/domain/service"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/mcoder2014/home_server/utils/routine"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/route"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := log.Init(); err != nil {
		panic(fmt.Errorf("log init  error: %w", err))
	}

	cliConfig := GetConfig()
	if err := config.InitGlobalConfig(cliConfig.ConfigPath); err != nil {
		panic(fmt.Errorf("load Global config from:%v failed, err:%w", cliConfig.ConfigPath, err))
	}
	logrus.Infof("Load Config: %+v", config.Global())

	routine.Init()

	// 监听 ctrl c 信号
	exitChan := make(chan os.Signal)
	signal.Notify(exitChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	go exitHandle(exitChan)

	// 链接数据库
	if err := db.InitDatabase(config.Global().Mysql.MasterDB); err != nil {
		panic(fmt.Errorf("connect to mysql failed. dsn is %v, err:%w ", config.Global().Mysql.MasterDB, err))
	}

	r := route.InitRoute()
	port := cliConfig.Port
	if port == -1 {
		port = config.Global().Server.Port
	}
	logrus.Infof("will bind http server port on %v", port)

	// init service
	if err := service.Init(config.ConfigPtr(config.Global())); err != nil {
		panic(err)
	}

	if err := schedule.Init(); err != nil {
		panic(err)
	}

	if err := r.Run(":" + strconv.Itoa(port)); err != nil {
		panic(err)
	}
	defer routine.Wait()
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

func exitHandle(exitChan chan os.Signal) {
	for {
		select {
		case sig := <-exitChan:
			logrus.Infof("Get Signal: %v from sys, stop program", sig)
			time.Sleep(1 * time.Second)
			os.Exit(1) //如果ctrl+c 关不掉程序，使用os.Exit强行关掉
		}
	}
}
