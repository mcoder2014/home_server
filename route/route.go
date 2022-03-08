package route

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/handler"
	"github.com/sirupsen/logrus"
)



func InitRoute() *gin.Engine {
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.GET("/hi", handler.Hi)

	// 批量注册回调
	for path, route := range data.RouterMap {
		r.Handle(route.Method, path, route.Handler)
		logrus.Infof("Register Method: %v, path: %v", route.Method, path)
	}

	return r
}
