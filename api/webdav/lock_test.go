package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

// TestWebDAVAliasesShareLocks 验证两个共享文件根的入口遵守同一排他锁；仅使用临时文件，不启动网络或数据库。
func TestWebDAVAliasesShareLocks(t *testing.T) {
	oldConfig := config.Global()
	oldRoutes := data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(oldConfig); data.RouterMap = oldRoutes })
	conf := config.Config{}
	conf.WebDAV.SharePath = t.TempDir()
	config.SetGlobalConfig(conf)
	if err := InitRouter(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf.WebDAV.SharePath, "review.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	lock := httptest.NewRequest("LOCK", "/webdav/review.txt", strings.NewReader(`<?xml version="1.0"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner>synthetic-review</D:owner></D:lockinfo>`))
	lock.Header.Set("Depth", "0")
	lock.Header.Set("Timeout", "Second-60")
	locked := httptest.NewRecorder()
	rawHandler.ServeHTTP(locked, lock)
	if locked.Code != http.StatusOK || locked.Header().Get("Lock-Token") == "" {
		t.Fatalf("LOCK setup status=%d", locked.Code)
	}
	same := httptest.NewRecorder()
	rawHandler.ServeHTTP(same, httptest.NewRequest(http.MethodPut, "/webdav/review.txt", strings.NewReader("same-route")))
	other := httptest.NewRecorder()
	rawHandlerDev.ServeHTTP(other, httptest.NewRequest(http.MethodPut, "/webdav_dev/review.txt", strings.NewReader("other-route")))
	content, err := os.ReadFile(filepath.Join(conf.WebDAV.SharePath, "review.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if same.Code != http.StatusLocked || other.Code != http.StatusLocked || string(content) != "original" {
		t.Fatalf("same_alias_status=%d other_alias_status=%d changed=%t", same.Code, other.Code, string(content) == "other-route")
	}
	t.Logf("exclusive_lock_status=%d same_alias_without_token=%d alternate_alias_without_token=%d file_changed=false", locked.Code, same.Code, other.Code)
}
