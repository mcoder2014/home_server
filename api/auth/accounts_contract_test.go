package auth

import (
	"net/http"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

// TestBrowserAccountEndpointsRemainAvailable 验证浏览器账号路由不依赖应用凭证开关，避免关闭机器认证时连带移除网站登录。
func TestBrowserAccountEndpointsRemainAvailable(t *testing.T) {
	oldConfig, oldRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(oldConfig); data.RouterMap = oldRoutes })
	data.RouterMap = make(map[string]map[string]data.HttpRoute)
	conf := config.Config{}
	conf.Auth.SiteOrigin = "https://home.example.com"
	config.SetGlobalConfig(conf)
	if err := InitRouter(); err != nil {
		t.Fatal(err)
	}
	for path, method := range map[string]string{
		"/api/auth/login":           http.MethodPost,
		"/api/auth/me":              http.MethodGet,
		"/api/auth/register":        http.MethodPost,
		"/api/auth/change-password": http.MethodPost,
		"/api/auth/logout":          http.MethodPost,
	} {
		if _, ok := data.RouterMap[path][method]; !ok {
			t.Errorf("browser account endpoint %s %s is unavailable", method, path)
		}
	}
}
