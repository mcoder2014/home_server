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
	// 加入中间件
	r.Use(gin.Recovery(), middleware.AddLogID, middleware.CORS())

	// 批量注册 http 接口
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		r.Handle(method, path, handlers...)
		logrus.Infof("Gin Register Method: %v, path: %v", method, path)
	})

	return r
}
