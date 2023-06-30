package api

import (
	"sync"

	"github.com/mcoder2014/home_server/api/feishu"
	"github.com/mcoder2014/home_server/api/library"
	"github.com/mcoder2014/home_server/api/passport"
	"github.com/mcoder2014/home_server/api/webdav"
)

var routeInit sync.Once

// InitRouter 初始化路由， 仅执行一次
func InitRouter() error {
	var err error
	routeInit.Do(func() {

		for _, initFunc := range []func() error{
			// DDNS 相关接口
			InitDDNSRouter,
			// 图书相关接口
			library.InitRouter,
			// 登录退出相关接口
			passport.InitRouter,
			// webDAV 相关接口
			webdav.InitRouter,
			// 飞书开放平台相关接口
			feishu.InitRouter,
		} {
			err = initFunc()
			if err != nil {
				return
			}
		}
	})
	return err
}
