package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/api/accounts"
	"github.com/mcoder2014/home_server/app/acceleration"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/domain/service"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/mcoder2014/home_server/utils/routine"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/route"
	"github.com/sirupsen/logrus"
)

// main 初始化配置、数据库和业务服务，成功加载运行时配置后启动 HTTP 服务及后台维护。
func main() {
	if err := log.Init(); err != nil {
		panic(fmt.Errorf("log init  error: %w", err))
	}

	cliConfig := GetConfig()
	if err := config.InitGlobalConfig(cliConfig.ConfigPath); err != nil {
		panic(fmt.Errorf("load Global config from:%v failed, err:%w", cliConfig.ConfigPath, err))
	}

	routine.Init()

	// 监听 ctrl c 信号
	exitChan := make(chan os.Signal)
	signal.Notify(exitChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	go exitHandle(exitChan)

	// 链接数据库
	if err := db.InitDatabase(config.Global().Mysql.MasterDB); err != nil {
		panic(fmt.Errorf("connect to mysql failed: %w", err))
	}

	port := cliConfig.Port
	if port == -1 {
		port = config.Global().Server.Port
	}
	address := net.JoinHostPort(cliConfig.Host, strconv.Itoa(port))
	logrus.Infof("will bind http server on %s", address)

	// init service
	if err := service.Init(config.ConfigPtr(config.Global())); err != nil {
		panic(err)
	}
	runtimeService := siteconfig.New(db.MasterDB(), config.Global())
	if err := runtimeService.Initialize(context.Background()); err != nil {
		panic(fmt.Errorf("runtime configuration unavailable: %w", err))
	}
	accounts.Configure(runtimeService)
	if _, err := acceleration.Initialize(config.Global(), db.MasterDB()); err != nil {
		panic(fmt.Errorf("Redis/analytics initialization: %w", err))
	}
	r := route.InitRoute()
	runtimeService.Start(context.Background())
	webprojects.StartMaintenance(config.Runtime().WebProjects)

	if err := r.Run(address); err != nil {
		panic(err)
	}
	defer routine.Wait()
}

type CliArgsConfig struct {
	// 默认监听全部接口；隔离验证时可指定 127.0.0.1。
	Host string
	// 配置端口号
	Port int
	// 配置文件路径
	ConfigPath string
}

// GetConfig 读取命令行配置
func GetConfig() *CliArgsConfig {
	conf := &CliArgsConfig{}

	flag.IntVar(&conf.Port, "port", -1, "http server 端口号,默认为空")
	flag.StringVar(&conf.Host, "host", "", "http server 监听地址，默认为全部接口")
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
			if runtime := acceleration.Current.Load(); runtime != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := runtime.Close(ctx); err != nil {
					logrus.WithError(err).Warn("analytics shutdown incomplete")
				}
				cancel()
			}
			time.Sleep(1 * time.Second)
			os.Exit(1) //如果ctrl+c 关不掉程序，使用os.Exit强行关掉
		}
	}
}
