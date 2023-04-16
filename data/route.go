package data

import (
	"github.com/gin-gonic/gin"
)

var (
	// RouterMap 用于初始化 http 路由的 map
	RouterMap = map[string]map[string]HttpRoute{}
)

// HttpRoute 记录路由
type HttpRoute struct {
	// HTTP 方法
	Method string
	// HTTP 路径
	Path string
	// Handler 处理函数
	Handlers []gin.HandlerFunc
}

func AddRoute(method string, path string, handlers ...gin.HandlerFunc) {
	if _, ok := RouterMap[path]; !ok {
		RouterMap[path] = make(map[string]HttpRoute)
	}
	RouterMap[path][method] = HttpRoute{
		Method:   method,
		Path:     path,
		Handlers: handlers,
	}
}

func ForRange(f func(method, path string, handlers ...gin.HandlerFunc)) {
	for path, route := range RouterMap {
		for method, r := range route {
			f(method, path, r.Handlers...)
		}
	}
}
