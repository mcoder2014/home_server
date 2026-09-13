package accounts

import (
	"encoding/json"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
)

// TestAccountViewIncludesCurrentApplicationPolicy 校验本人资料响应携带当前应用数量和凭证期限策略，避免管理表单继续使用固定旧值。
func TestAccountViewIncludesCurrentApplicationPolicy(t *testing.T) {
	old := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(old) })
	conf := config.Config{}
	conf.Auth.DefaultCredentialTTLDays = 11
	conf.Auth.MaxCredentialTTLDays = 29
	conf.Auth.MaxApplicationsPerUser = 7
	config.SetGlobalConfig(conf)
	raw, err := json.Marshal(view(&model.UserAccount{ID: 1}, "synthetic-session"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	policy, ok := result["application_policy"].(map[string]interface{})
	if !ok {
		t.Fatal("account response omitted application_policy")
	}
	for key, want := range map[string]float64{"default_credential_ttl_days": 11, "max_credential_ttl_days": 29, "max_applications_per_user": 7} {
		if policy[key] != want {
			t.Errorf("%s=%v, want %v", key, policy[key], want)
		}
	}
}
