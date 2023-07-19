// Package webdav 使用 golang.org/x/net/webdav 实现的 webdav 服务，包装了一层身份认证。
// 并非裸奔，实际会通过 nginx 反向代理，nginx 会有 https 证书，所以这里不需要 https 证书。
package webdav

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/app/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/utils/log"
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
		Logger:     Logger,
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
	// 用比较黑客的方式规避 Nginx 代理后 URL 与 Host 不一致的问题
	logID := c.GetString(log.LogIDKey)
	c.Request.Header.Set(log.LogIDKey, logID)
	c.Writer.Header().Set(log.LogIDKey, logID)

	destination := c.Request.Header.Get("Destination")
	if len(destination) > 0 {
		idx := strings.Index(destination, "/webdav/")
		if idx >= 0 {
			destination = "http://" + c.Request.Host + destination[idx:]
		}
		c.Request.Header.Set("Destination", destination)
	}
	rawHandler.ServeHTTP(c.Writer, c.Request)
}

func Logger(req *http.Request, err error) {
	url := req.URL.Path
	headers := req.Header
	logID := headers.Get(log.LogIDKey)
	ctx := context.WithValue(context.Background(), log.LogIDKey, logID)

	if err != nil {
		var sb strings.Builder
		sb.WriteString("webdav error: ")
		sb.WriteString(err.Error())
		sb.WriteString("\n")
		sb.WriteString("Host: ")
		sb.WriteString(req.Host)
		sb.WriteString("\n")
		sb.WriteString("url: ")
		sb.WriteString(url)
		sb.WriteString("\n")
		sb.WriteString("headers: ")
		for k, v := range headers {
			sb.WriteString("\t")
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v[0])
			sb.WriteString("\n")
		}
		log.Ctx(ctx).Errorf("[WebDAV] [%v] %v", req.Method, sb.String())
	} else {
		log.Ctx(ctx).Infof("[WebDAV] [%v] %v", req.Method, url)
	}
}
