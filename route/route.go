package route

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/handler"
)

func InitRoute() *gin.Engine {
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.GET("/hi", handler.Hi)
	return r
}