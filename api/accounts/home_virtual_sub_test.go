package accounts_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	virtualapi "github.com/mcoder2014/home_server/api/homevirtualsub"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
)

const virtualRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const virtualPrefix = "/api/admin/home-virtual-sub"

func installVirtualRoutes(t *testing.T, f *httpFixture, address string) {
	t.Helper()
	if address != "" {
		path := filepath.Join(t.TempDir(), "service.token")
		if err := os.WriteFile(path, []byte("synthetic-home-virtual-sub-service-token"), 0600); err != nil {
			t.Fatal(err)
		}
		f.conf.HomeVirtualSub = config.HomeVirtualSubConfig{Enabled: true, BaseURL: address, TokenFile: path}
	}
	// Adding a bootstrap connection must retain the already loaded DB snapshot.
	snapshot := config.Runtime()
	config.SetGlobalConfig(f.conf)
	if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := virtualapi.InitRouter(); err != nil {
		t.Fatal(err)
	}
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		if strings.HasPrefix(path, virtualPrefix) {
			f.router.Handle(method, path, handlers...)
		}
	})
}

func TestHTTPHomeVirtualSubAuthorizationAndSafeAudit(t *testing.T) {
	f := newHTTPFixture(t)
	var calls int32
	var unauthorized int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Authorization") != "Bearer synthetic-home-virtual-sub-service-token" {
			t.Error("service credential missing")
		}
		if atomic.LoadInt32(&unauthorized) == 1 {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"unauthorized","message":"secret-provider credential"}}`))
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["current_password"] != nil || body["reason"] != nil {
			t.Error("confirmation leaked to service")
		}
		if r.Header.Get("If-Match") != `"`+virtualRevision+`"` {
			t.Error("revision not forwarded")
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"saved": true, "revision": virtualRevision, "apply_state": "pending", "message": "https://secret.example/sub?token=hidden"})
	}))
	defer upstream.Close()
	installVirtualRoutes(t, f, upstream.URL)
	owner := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	application := f.applicationToken(owner, []string{"web-projects:read"})
	input := map[string]interface{}{"name": "测试/订阅", "urls": []string{"https://secret.example/sub?token=hidden"}, "http_port": 18301, "socket_port": 18302, "reason": "验证管理权限", "current_password": integrationPassword}
	headers := map[string]string{"If-Match": virtualRevision}
	for _, session := range []*browserSession{nil, member} {
		requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", session, input, headers))
		requireDenied(t, f.request("GET", virtualPrefix+"/status", session, nil, nil))
	}
	requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", owner, input, map[string]string{"If-Match": virtualRevision, "Authorization": "Bearer " + application}))
	requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", owner, input, map[string]string{"If-Match": virtualRevision, "X-CSRF-Token": ""}))
	requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", owner, input, map[string]string{"If-Match": virtualRevision, "Origin": "https://foreign.example.com"}))
	requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", owner, input, nil))
	input["current_password"] = "wrong-password"
	requireDenied(t, f.request("POST", virtualPrefix+"/subscriptions", owner, input, headers))
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("denied request reached service")
	}
	input["current_password"] = integrationPassword
	for _, invalid := range []map[string]interface{}{{"urls": []string{}}, {"urls": nil}, {"http_port": nil}, {"bind_ip": nil}, {"outbound_server_count": nil}} {
		invalid["reason"], invalid["current_password"] = "验证空字段", integrationPassword
		requireDenied(t, f.request("PATCH", virtualPrefix+"/subscriptions?name=demo", owner, invalid, headers))
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("explicit empty values reached service")
	}
	response := f.request("POST", virtualPrefix+"/subscriptions", owner, input, headers)
	result := requireSuccess(t, response)
	if result["saved"] != true || result["apply_state"] != "pending" || result["audit_recorded"] != true {
		t.Fatalf("unexpected mutation result: %s", response.Raw)
	}
	if strings.Contains(string(response.Raw), "secret.example") || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("mutation leaked service text or was cacheable")
	}
	var summary, reason, outcome string
	if err := f.database.QueryRow("SELECT after_summary, reason, result FROM admin_audit_log WHERE target_type = 'home_virtual_sub'").Scan(&summary, &reason, &outcome); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "secret.example") || strings.Contains(summary, integrationPassword) || reason != "验证管理权限" || outcome != "pending" {
		t.Fatal("audit must retain only safe metadata and actual outcome")
	}
	atomic.StoreInt32(&unauthorized, 1)
	response = f.request("POST", virtualPrefix+"/subscriptions", owner, input, headers)
	if response.Status != 503 || strings.Contains(string(response.Raw), "credential") {
		t.Fatalf("unsafe dependency failure: %s", response.Raw)
	}
	// A dependency credential failure must not revoke the browser session.
	requireSuccess(t, f.request("GET", "/api/auth/me", owner, nil, nil))
	// An unavailable intent log must block the external write.
	previous := atomic.LoadInt32(&calls)
	if _, err := f.database.Exec("DROP TABLE admin_audit_log"); err != nil {
		t.Fatal(err)
	}
	response = f.request("POST", virtualPrefix+"/subscriptions", owner, input, headers)
	if response.Status != 503 || atomic.LoadInt32(&calls) != previous {
		t.Fatal("unrecorded external write was allowed")
	}
}

func TestHTTPHomeVirtualSubDisabledByDefault(t *testing.T) {
	f := newHTTPFixture(t)
	installVirtualRoutes(t, f, "")
	owner := f.login(f.owner, integrationPassword)
	status := requireSuccess(t, f.request("GET", virtualPrefix+"/status", owner, nil, nil))
	if status["enabled"] != false {
		t.Fatal("legacy config unexpectedly enabled management")
	}
	requireDenied(t, f.request("GET", virtualPrefix+"/subscriptions", owner, nil, nil))
}

func TestHTTPHomeVirtualSubAuditCompletionFailureKeepsServiceResult(t *testing.T) {
	f := newHTTPFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate an intent disappearing after the external operation succeeded.
		if _, err := f.database.Exec("DELETE FROM admin_audit_log WHERE target_type = 'home_virtual_sub'"); err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"saved": true, "revision": virtualRevision, "apply_state": "applied"})
	}))
	defer upstream.Close()
	installVirtualRoutes(t, f, upstream.URL)
	owner := f.login(f.owner, integrationPassword)
	input := map[string]interface{}{"current_password": integrationPassword, "reason": "验证审计结果异常"}
	response := f.request("DELETE", virtualPrefix+"/subscriptions?name=demo", owner, input, map[string]string{"If-Match": virtualRevision})
	result := requireSuccess(t, response)
	if result["saved"] != true || result["apply_state"] != "applied" || result["audit_recorded"] != false {
		t.Fatalf("external operation was misreported as rollback: %s", response.Raw)
	}
}

func TestHTTPHomeVirtualSubRechecksRevokedActingSession(t *testing.T) {
	f := newHTTPFixture(t)
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{"saved": false, "revision": virtualRevision, "apply_state": "applied"})
	}))
	defer upstream.Close()
	installVirtualRoutes(t, f, upstream.URL)
	acting := f.login(f.owner, integrationPassword)
	surviving := f.login(f.owner, integrationPassword)
	page := requireSuccess(t, f.request(http.MethodGet, "/api/account/sessions", acting, nil, nil))
	actingID := object(page["current_session"])["id"].(string)
	payload, err := json.Marshal(map[string]interface{}{"reason": "验证写入前会话复核", "current_password": integrationPassword})
	if err != nil {
		t.Fatal(err)
	}
	// Pause after authentication, then revoke only the session performing the write.
	blocked, done := f.beginPausedAccountRequest(virtualPrefix+"/subscriptions/start?name=demo", acting, payload, map[string]string{"If-Match": virtualRevision})
	result := requireSuccess(t, f.request(http.MethodPost, "/api/account/sessions/revoke", surviving, map[string]interface{}{"session_ids": []string{actingID}}, nil))
	if number(result["revoked_count"]) != 1 {
		t.Fatal("acting session was not selectively revoked")
	}
	response := f.finishPausedRequest(blocked, done)
	var audits int64
	if err := f.database.QueryRow("SELECT COUNT(*) FROM admin_audit_log WHERE target_type = 'home_virtual_sub'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusUnauthorized || atomic.LoadInt32(&calls) != 0 || audits != 0 {
		t.Fatalf("revoked acting session reached the external write: status=%d calls=%d audits=%d", response.Status, atomic.LoadInt32(&calls), audits)
	}
	f.me(surviving)
}

// This opt-in acceptance test connects the real administrator handlers to an
// isolated management service with a synthetic feed and core. CI without that
// independent service still runs the authorization and RPC contract tests.
func TestHTTPHomeVirtualSubConnectedService(t *testing.T) {
	address, tokenFile, feed := os.Getenv("HOME_VIRTUAL_SUB_TEST_URL"), os.Getenv("HOME_VIRTUAL_SUB_TEST_TOKEN_FILE"), os.Getenv("HOME_VIRTUAL_SUB_TEST_FEED_URL")
	if address == "" || tokenFile == "" || feed == "" {
		t.Skip("requires an isolated home virtual sub service and synthetic feed")
	}
	if address != "http://127.0.0.1:19190" || !strings.HasPrefix(tokenFile, "/tmp/home-virtual-sub-validation-") || feed != "http://127.0.0.1:19191/subscription" {
		t.Fatal("unsafe connected-service fixture")
	}
	f := newHTTPFixture(t)
	f.conf.HomeVirtualSub = config.HomeVirtualSubConfig{Enabled: true, BaseURL: address, TokenFile: tokenFile}
	installVirtualRoutes(t, f, "")
	owner := f.login(f.owner, integrationPassword)
	status := requireSuccess(t, f.request("GET", virtualPrefix+"/status", owner, nil, nil))
	if number(status["total"]) != 0 {
		t.Fatal("connected service must start with an empty synthetic config")
	}
	getRevision := func() string {
		return requireSuccess(t, f.request("GET", virtualPrefix+"/subscriptions", owner, nil, nil))["revision"].(string)
	}
	call := func(method, path string, values map[string]interface{}) map[string]interface{} {
		values["current_password"], values["reason"] = integrationPassword, "独立双服务联调"
		return requireSuccess(t, f.request(method, virtualPrefix+path, owner, values, map[string]string{"If-Match": getRevision()}))
	}
	nameQuery := "?name=%E8%81%94%E8%B0%83%2F%E8%AE%A2%E9%98%85"
	created := call("POST", "/subscriptions", map[string]interface{}{"name": "联调/订阅", "urls": []string{feed}, "bind_ip": "127.0.0.1", "http_port": 19192, "socket_port": 19193, "outbound_server_count": 1})
	if created["saved"] != true {
		t.Fatal("create did not persist configuration")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		status = requireSuccess(t, f.request("GET", virtualPrefix+"/status", owner, nil, nil))
		if number(status["running"]) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("synthetic core did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	detail := requireSuccess(t, f.request("GET", virtualPrefix+"/subscriptions/detail"+nameQuery, owner, nil, nil))
	if object(detail["subscription"])["name"] != "联调/订阅" {
		t.Fatal("query name did not survive the gateway")
	}
	call("PATCH", "/subscriptions"+nameQuery, map[string]interface{}{"http_port": 19194, "socket_port": 19195})
	call("POST", "/subscriptions/stop"+nameQuery, map[string]interface{}{})
	status = requireSuccess(t, f.request("GET", virtualPrefix+"/status", owner, nil, nil))
	if number(status["running"]) != 0 {
		t.Fatal("stop did not wait for the core to exit")
	}
	call("POST", "/subscriptions/start"+nameQuery, map[string]interface{}{})
	call("POST", "/subscriptions/refresh"+nameQuery, map[string]interface{}{})
	deleted := call("DELETE", "/subscriptions"+nameQuery, map[string]interface{}{})
	if deleted["saved"] != true {
		t.Fatal("delete did not persist configuration")
	}
	status = requireSuccess(t, f.request("GET", virtualPrefix+"/status", owner, nil, nil))
	if number(status["total"]) != 0 || number(status["running"]) != 0 {
		t.Fatal("last delete retained a worker")
	}
	logs := requireSuccess(t, f.request("GET", "/api/admin/audit-logs?target_type=home_virtual_sub", owner, nil, nil))
	encoded, _ := json.Marshal(logs)
	if strings.Contains(string(encoded), feed) || strings.Contains(string(encoded), integrationPassword) {
		t.Fatal("connected-service audit exposed sensitive input")
	}
}
