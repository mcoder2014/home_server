package homevirtualsub

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	virtual "github.com/mcoder2014/home_server/rpc/homevirtualsub"
)

const prefix = "/api/admin/home-virtual-sub"

func InitRouter() error {
	client, err := virtual.NewClient(config.Global().HomeVirtualSub)
	if err != nil {
		return err
	}
	admin, write := middleware.RequireAccount(true, false), middleware.BrowserWrite()
	for _, view := range []string{"status", "subscriptions", "subscriptions/detail"} {
		current := view
		data.AddRoute(http.MethodGet, prefix+"/"+current, admin, func(c *gin.Context) { read(c, client, current) })
	}
	for _, route := range []struct{ method, path, action string }{
		{http.MethodPost, "/subscriptions", "create"},
		{http.MethodPatch, "/subscriptions", "update"},
		{http.MethodDelete, "/subscriptions", "delete"},
		{http.MethodPost, "/subscriptions/refresh", "refresh"},
		{http.MethodPost, "/subscriptions/start", "start"},
		{http.MethodPost, "/subscriptions/stop", "stop"},
	} {
		action := route.action
		data.AddRoute(route.method, prefix+route.path, admin, write, func(c *gin.Context) { mutate(c, client, action) })
	}
	return nil
}
