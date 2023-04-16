package passport

import (
	"net/http"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	// 获得 rsa 公钥
	data.AddRoute(http.MethodGet, "/passport/rsa", QueryLoginRsa)
	// login 接口
	data.AddRoute(http.MethodPost, "/passport/login", Login)
	data.AddRoute(http.MethodPost, "/passport/logout", middleware.ValidateLogin(), Logout)
	return nil
}
