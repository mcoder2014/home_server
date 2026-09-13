package route

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

// InitRoute 初始化业务路由后构建 Gin 引擎，安装访问日志、恢复、LogID 与 CORS，再挂载注册表中的方法和路径。
// 健康检查单独注册，不执行后续业务中间件。
func InitRoute() *gin.Engine {
	// 先初始化路由
	if err := api.InitRouter(); err != nil {
		panic(fmt.Errorf("init gin router error:%w", err))
	}

	engine := gin.New()
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{Output: log.GetDefaultOutput(), Formatter: middleware.AccessLogFormatter}), gin.RecoveryWithWriter(log.GetDefaultOutput()))
	// GET /ping 返回进程存活信号，不执行数据库或业务依赖检查。
	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	// 加入中间件
	engine.Use(middleware.AddLogID, middleware.CORS())

	// 批量注册 http 接口
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		engine.Handle(method, path, handlers...)
		logrus.Infof("Gin Register Method: %v, path: %v", method, path)
	})

	return engine
}
