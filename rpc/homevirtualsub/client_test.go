package homevirtualsub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/smartystreets/goconvey/convey"
)

const fixtureRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const fixtureToken = "synthetic-home-virtual-sub-service-token"

func fixtureClient(t *testing.T, handler http.HandlerFunc, timeout int) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tokenFile := filepath.Join(t.TempDir(), "service.token")
	if err := os.WriteFile(tokenFile, []byte(fixtureToken), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config.HomeVirtualSubConfig{Enabled: true, BaseURL: server.URL, TokenFile: tokenFile, TimeoutMS: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientFixedRoutesAndSafeResponses(t *testing.T) {
	convey.Convey("固定服务凭证、版本和查询编码，白名单过滤服务响应", t, func() {
		client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
				t.Error("missing service credential")
			}
			switch r.URL.Path {
			case "/management/v1/status":
				json.NewEncoder(w).Encode(map[string]interface{}{"api_version": 1, "revision": fixtureRevision, "core_type": "xray", "total": 1, "private_metadata": "hidden", "workers": []map[string]interface{}{{"name": "测试/订阅", "state": "running", "running": true, "last_error": "https://secret.example/sub?token=private"}}})
			case "/management/v1/subscriptions/detail":
				if r.URL.Query().Get("name") != "测试/订阅" {
					t.Error("subscription query was not preserved")
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"revision": fixtureRevision, "subscription": map[string]interface{}{"name": "测试/订阅", "urls": []string{"https://example.com/sub"}, "http_proxy_chain": map[string]string{"password": "hidden"}}})
			case "/management/v1/subscriptions/refresh":
				if r.Header.Get("If-Match") != `"`+fixtureRevision+`"` {
					t.Error("missing configuration revision")
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"saved": false, "revision": fixtureRevision, "apply_state": "pending", "message": "unsafe https://secret.example"})
			default:
				t.Errorf("unexpected service path: %s", r.URL.Path)
			}
		}, 5000)
		status, err := client.Status(context.Background())
		convey.So(err, convey.ShouldBeNil)
		convey.So(status.Total, convey.ShouldEqual, 1)
		raw, _ := json.Marshal(status)
		convey.So(string(raw), convey.ShouldNotContainSubstring, "secret.example")
		convey.So(string(raw), convey.ShouldNotContainSubstring, "private_metadata")
		detail, err := client.Detail(context.Background(), "测试/订阅")
		convey.So(err, convey.ShouldBeNil)
		convey.So(detail.Subscription.URLs[0], convey.ShouldEqual, "https://example.com/sub")
		raw, _ = json.Marshal(detail)
		convey.So(string(raw), convey.ShouldNotContainSubstring, "http_proxy_chain")
		result, err := client.Mutate(context.Background(), "refresh", "测试/订阅", fixtureRevision, SubscriptionInput{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.ApplyState, convey.ShouldEqual, "pending")
		convey.So(result.Message, convey.ShouldNotContainSubstring, "secret.example")
		_, err = client.Mutate(context.Background(), "../../other", "demo", fixtureRevision, SubscriptionInput{})
		convey.So(errors.Is(err, apperrors.ErrInvalid), convey.ShouldBeTrue)
	})
}

func TestClientDoesNotFollowRedirectOrRetry(t *testing.T) {
	convey.Convey("重定向不泄露服务凭证，超时写入不自动重试", t, func() {
		var redirected int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&redirected, 1) }))
		defer target.Close()
		client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		}, 5000)
		_, err := client.Status(context.Background())
		convey.So(errors.Is(err, apperrors.ErrDependency), convey.ShouldBeTrue)
		convey.So(atomic.LoadInt32(&redirected), convey.ShouldEqual, 0)

		var attempts int32
		slow := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			time.Sleep(200 * time.Millisecond)
		}, 100)
		_, err = slow.Mutate(context.Background(), "delete", "demo", fixtureRevision, SubscriptionInput{})
		convey.So(errors.Is(err, apperrors.ErrDependency), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "核对")
		convey.So(atomic.LoadInt32(&attempts), convey.ShouldEqual, 1)
	})
}

func TestClientRejectsUnsafeErrorsAndTokenFiles(t *testing.T) {
	convey.Convey("服务认证失败不会使管理员登出，也不转发原始错误", t, func() {
		client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":"unauthorized","message":"private-service https://secret.example token=password"}}`))
		}, 5000)
		_, err := client.Status(context.Background())
		convey.So(errors.Is(err, apperrors.ErrDependency), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldNotContainSubstring, "secret.example")
		convey.So(strings.Contains(err.Error(), "password"), convey.ShouldBeFalse)

		path := filepath.Join(t.TempDir(), "unsafe.token")
		convey.So(os.WriteFile(path, []byte(fixtureToken), 0644), convey.ShouldBeNil)
		_, err = NewClient(config.HomeVirtualSubConfig{Enabled: true, BaseURL: "http://127.0.0.1:18190", TokenFile: path})
		convey.So(err, convey.ShouldNotBeNil)
	})
}
