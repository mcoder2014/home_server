package accounts_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api"
	"github.com/mcoder2014/home_server/data"
)

// TestHTTPDDNSRecordsRequireAdministrator 验证旧内存记录维护仅允许管理员，匿名来源和普通成员不能篡改或枚举记录。
func TestHTTPDDNSRecordsRequireAdministrator(t *testing.T) {
	f := newHTTPFixture(t)
	api.Ipv4Map = sync.Map{}
	t.Cleanup(func() { api.Ipv4Map = sync.Map{} })
	if err := api.InitDDNSRouter(); err != nil {
		t.Fatal(err)
	}
	data.ForRange(func(method, path string, handlers ...gin.HandlerFunc) {
		if strings.HasPrefix(path, "/ddns") {
			f.router.Handle(method, path, handlers...)
		}
	})
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	input := map[string]string{"Domain": "synthetic.example.com", "Ipv4": "192.0.2.40"}
	for _, session := range []*browserSession{nil, member} {
		requireDenied(t, f.request("POST", "/ddns/ipv4", session, input, nil))
		requireDenied(t, f.request("GET", "/ddns/all", session, nil, nil))
		requireDenied(t, f.request("GET", "/ddns?domain=synthetic.example.com", session, nil, nil))
	}
	requireSuccess(t, f.request("POST", "/ddns/ipv4", admin, input, nil))
	requireSuccess(t, f.request("POST", "/ddns/ipv4", admin, input, nil))
	response := f.request("GET", "/ddns?domain=synthetic.example.com", admin, nil, nil)
	var record struct {
		IPv4 string `json:"ipv4"`
	}
	if err := json.Unmarshal(response.Raw, &record); err != nil || record.IPv4 != input["Ipv4"] {
		t.Fatal("administrator could not read the recorded address")
	}
	requireDenied(t, f.request("POST", "/ddns/ipv4", admin, map[string]string{"Domain": "", "Ipv4": "not-an-ip"}, nil))
	requireDenied(t, f.request("POST", "/ddns/ipv4", admin, strings.Repeat("x", 17<<10), nil))
	if _, exists := api.Ipv4Map.Load(""); exists {
		t.Fatal("invalid request created an empty-key DDNS record")
	}
	if response := f.request("GET", "/ddns/real_ip", nil, nil, nil); response.Status != 200 {
		t.Fatal("public source-address discovery must remain available")
	}
}
