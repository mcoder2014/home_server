// Package webdav 使用 golang.org/x/net/webdav 实现的 webdav 服务，包装了一层身份认证。
// 并非裸奔，实际会通过 nginx 反向代理，nginx 会有 https 证书，所以这里不需要 https 证书。
package webdav

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"golang.org/x/net/webdav"
)

var rawHandler *webdav.Handler

func InitRouter() error {
	sharePath := config.Global().WebDAV.SharePath
	if len(sharePath) == 0 {
		return fmt.Errorf("module webdav, share path is empty")
	}

	rawHandler = &webdav.Handler{
		Prefix:     "/webdav/",
		FileSystem: webdav.Dir(sharePath),
		LockSystem: webdav.NewMemLS(),
	}

	webDAVPath := "/webdav/*path"
	data.AddRoute("OPTIONS", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("GET", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("HEAD", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("POST", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("DELETE", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("PUT", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("MKCOL", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("COPY", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("MOVE", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("LOCK", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("UNLOCK", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("PROPFIND", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRoute("PROPPATCH", webDAVPath, middleware.ValidateBasicAuth(), WebDAV)
	return nil
}

func WebDAV(c *gin.Context) {
	rawHandler.ServeHTTP(c.Writer, c.Request)
}
