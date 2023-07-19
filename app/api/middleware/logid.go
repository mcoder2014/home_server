package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/utils/log"
)

func AddLogID(ctx *gin.Context) {
	if _, exist := ctx.Get(log.LogIDKey); exist {
		return
	}
	ctx.Set(log.LogIDKey, log.GenLogID())
}
