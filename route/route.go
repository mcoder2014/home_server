package route

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/sirupsen/logrus"
)

func InitRoute() *gin.Engine {
	// 先初始化路由
	api.InitRouter()

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// 批量注册回调
	for path, route := range data.RouterMap {
		r.Handle(route.Method, path, route.Handler)
		logrus.Infof("Gin Register Method: %v, path: %v", route.Method, path)
	}

	// 加入中间件
	r.Use(middleware.AddLogID)

	return r
}
