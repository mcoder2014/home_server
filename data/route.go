package data

import "github.com/gin-gonic/gin"

var (
	// RouterMap 用于初始化 http 路由的 map
	RouterMap = map[string]HttpRoute{}
)

// HttpRoute 记录路由
type HttpRoute struct {
	// HTTP 方法
	Method string
	// HTTP 路径
	Path string
	// Handler 处理函数
	Handler gin.HandlerFunc
}