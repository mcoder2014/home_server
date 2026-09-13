// Package webdav 使用 golang.org/x/net/webdav 实现的 webdav 服务，包装了一层身份认证。
// 并非裸奔，实际会通过 nginx 反向代理，nginx 会有 https 证书，所以这里不需要 https 证书。
package webdav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/domain/model"
	webdavService "github.com/mcoder2014/home_server/domain/service/webdav"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
	"golang.org/x/net/webdav"
)

var rawHandler *webdav.Handler
var rawHandlerDev *webdav.Handler

// InitRouter 为共享根目录创建两个 WebDAV 处理器，并为两套路径的文件、目录和锁方法挂载 Basic/Bearer 认证。
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
	rawHandlerDev = &webdav.Handler{
		Prefix:     "/webdav_dev/",
		FileSystem: webdav.Dir(sharePath),
		LockSystem: webdav.NewMemLS(),
		Logger:     Logger,
	}

	// 计划加入 log 信息，备份一份路由
	methods := []string{
		"OPTIONS", "GET", "HEAD", "POST", "DELETE", "PUT", "MKCOL",
		"COPY", "MOVE", "LOCK", "UNLOCK", "PROPFIND", "PROPPATCH",
	}
	data.AddRouteV2(methods, []string{"/webdav/*path"}, middleware.ValidateBasicAuth(), WebDAV)
	data.AddRouteV2(methods, []string{"/webdav_dev/*path"}, middleware.ValidateBasicAuth(), WebDAVDev)
	return nil
}

// WebDAV 处理 /webdav/*path 的 WebDAV 方法：修正反向代理后的 Destination，并把文件与锁操作交给标准 WebDAV 处理器。
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

// WebDAVDev 处理 /webdav_dev/*path 的兼容 WebDAV 方法：在文件操作前记录路径摘要及用户操作事件，并使用同一共享目录。
func WebDAVDev(c *gin.Context) {
	// 用比较黑客的方式规避 Nginx 代理后 URL 与 Host 不一致的问题
	logID := c.GetString(log.LogIDKey)
	c.Request.Header.Set(log.LogIDKey, logID)
	c.Writer.Header().Set(log.LogIDKey, logID)

	destination := c.Request.Header.Get("Destination")
	if len(destination) > 0 {
		idx := strings.Index(destination, "/webdav_dev/")
		if idx >= 0 {
			destination = "http://" + c.Request.Host + destination[idx:]
		}
		c.Request.Header.Set("Destination", destination)
	}
	filepath := strings.TrimPrefix(c.Request.URL.Path, "/webdav_dev/")
	h := sha256.New()
	h.Write([]byte(filepath))

	logEntity := &model.WebDAVLogEntity{
		ID:       utils.GenInt64ID(),
		Method:   c.Request.Method,
		FilePath: filepath,
		Hash:     hex.EncodeToString(h.Sum(nil)),
		UserID:   utils.GetUserIDFromCtx(c),
		Agent:    c.Request.UserAgent(),
		LogID:    logID,
	}
	if err := webdavService.SendLogEvent(logEntity); err != nil {
		log.Ctx(ginfmt.RPCContext(c)).WithError(err).Errorf("logRoutine failed")
	}
	rawHandlerDev.ServeHTTP(c.Writer, c.Request)
}

// Logger 记录 WebDAV 请求结果；发生错误时补充主机、路径和脱敏请求头，不将认证头原文写入日志。
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
		for k, v := range log.RedactedHeaders(headers) {
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
