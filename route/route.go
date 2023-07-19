package route

import (
	"fmt"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/app/api"
	middleware2 "github.com/mcoder2014/home_server/app/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

func InitRoute() *gin.Engine {
	// 先初始化路由
	if err := api.InitRouter(); err != nil {
		panic(fmt.Errorf("init gin router error:%w", err))
	}

	engine := gin.New()
	engine.Use(gin.LoggerWithWriter(log.GetDefaultOutput()), gin.RecoveryWithWriter(log.GetDefaultOutput()))
	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	// session
	store := cookie.NewStore([]byte("secret11111"))
	engine.Use(sessions.Sessions("home_server", store))
	// 加入中间件
	engine.Use(middleware2.AddLogID, middleware2.CORS())

	// 批量注册 http 接口
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		engine.Handle(method, path, handlers...)
		logrus.Infof("Gin Register Method: %v, path: %v", method, path)
	})

	return engine
}
