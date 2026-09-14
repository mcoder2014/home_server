package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/utils/log"
)

// AddLogID 在上下文尚无 LogID 时生成并保存一个，使后续日志能关联到同一次请求。
func AddLogID(ctx *gin.Context) {
	if _, exist := ctx.Get(log.LogIDKey); exist {
		return
	}
	ctx.Set(log.LogIDKey, log.GenLogID())
}
