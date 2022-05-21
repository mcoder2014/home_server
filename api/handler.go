package api

import (
	"sync"

	"github.com/mcoder2014/home_server/api/library"
)

var routeInit sync.Once

// InitRouter 初始化路由， 仅执行一次
func InitRouter() {
	routeInit.Do(func() {
		// DDNS 相关接口
		InitDDNSRouter()

		// 图书相关接口
		library.InitRouter()
	})
}
