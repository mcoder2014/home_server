package webprojects

import (
	"bytes"
	_ "embed"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/model"
	xhtml "golang.org/x/net/html"
)

//go:embed container/runtime.js
var containerJS []byte

func containerScript(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", containerJS)
}

func injectContainerScript(raw, script []byte) ([]byte, error) {
	tokenizer := xhtml.NewTokenizer(bytes.NewReader(raw))
	body := make([]byte, 0, len(raw)+len(script))
	injected := false
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return nil, tokenizer.Err()
			}
			if !injected {
				body = append(body, script...)
			}
			return body, nil
		}
		if !injected && tokenType == xhtml.EndTagToken {
			name, _ := tokenizer.TagName()
			if bytes.EqualFold(name, []byte("head")) || bytes.EqualFold(name, []byte("body")) {
				body = append(body, script...)
				injected = true
			}
		}
		body = append(body, tokenizer.Raw()...)
	}
}

// Serve only a transformed response; immutable release bytes and downloads stay
// unchanged. The asset URL is absolute to avoid uploaded <base> changing it.
func serveEnhancedHTML(c *gin.Context, file *os.File, info os.FileInfo, project *model.WebProject, release *model.WebProjectRelease) error {
	raw, err := io.ReadAll(io.LimitReader(file, 50<<20+1))
	if err != nil || len(raw) > 50<<20 {
		return fmt.Errorf("read enhanced HTML")
	}
	scheme := "https"
	if c.Request.TLS == nil && c.GetHeader("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	source := stdhtml.EscapeString(scheme + "://" + c.Request.Host + "/api/web-share/container.js")
	script := fmt.Sprintf(`<script defer src="%s" data-project-id="%d" data-release-id="%d" data-entry-file="%s"></script>`, source, project.ID, release.ID, stdhtml.EscapeString(release.EntryFile))
	body, err := injectContainerScript(raw, []byte(script))
	if err != nil {
		return fmt.Errorf("inject enhanced HTML: %w", err)
	}
	c.Header("ETag", "")
	// A conditional request must not reuse an older raw/enhanced representation.
	c.Request.Header.Del("If-Modified-Since")
	c.Request.Header.Del("If-None-Match")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), bytes.NewReader(body))
	return nil
}

func contentNotFound(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusNotFound)
		return
	}
	if strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Header("Referrer-Policy", "no-referrer")
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>404</title><script>const url=new URL(location.href);if(url.searchParams.has('code')){url.searchParams.delete('code');history.replaceState(history.state,'',url.pathname+url.search+url.hash)}</script><body><h1>404</h1><p>页面不存在或不可见。</p><a href="/">系统主页</a></body></html>`))
	} else {
		c.Status(http.StatusNotFound)
	}
}
