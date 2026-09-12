package api

import (
	"sync"

	"github.com/mcoder2014/home_server/api/applications"
	"github.com/mcoder2014/home_server/api/auth"
	"github.com/mcoder2014/home_server/api/library"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/api/passport"
	"github.com/mcoder2014/home_server/api/webdav"
	"github.com/mcoder2014/home_server/api/webprojects"
	"github.com/mcoder2014/home_server/config"
)

var routeInit sync.Once

// InitRouter 初始化路由， 仅执行一次
func InitRouter() error {
	var err error
	routeInit.Do(func() {
		if err = middleware.ConfigureAuthentication(config.Global().Auth); err != nil {
			return
		}

		for _, initFunc := range []func() error{
			// DDNS 相关接口
			InitDDNSRouter,
			// 图书相关接口
			library.InitRouter,
			// 登录退出相关接口
			passport.InitRouter,
			// webDAV 相关接口
			webdav.InitRouter,
			// 静态网页项目相关接口
			webprojects.InitRouter,
			auth.InitRouter,
			applications.InitRouter,
		} {
			err = initFunc()
			if err != nil {
				return
			}
		}
	})
	return err
}
