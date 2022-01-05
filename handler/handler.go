package handler

import "github.com/gin-gonic/gin"

func Hi(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Hi",
	})
}