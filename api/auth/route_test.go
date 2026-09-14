package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/stretchr/testify/require"
)

func TestBrowserOriginAllowsLegacyAndAdditionalConfiguredOrigins(t *testing.T) {
	conf := config.AuthConfig{
		SiteOrigin:  "https://home.example.com",
		SiteOrigins: []string{"https://home.internal.example.com", "https://home.internal.example.com:8443"},
	}

	for _, origin := range []string{"https://home.example.com", "https://home.internal.example.com", "https://home.internal.example.com:8443"} {
		request := httptest.NewRequest("POST", "/api/auth/browser-login", nil)
		request.Header.Set("Origin", origin)
		require.True(t, browserOriginAllowed(request, conf), origin)
	}
}

// TestBrowserOriginRequiresOneExactConfiguredOrigin 验证 Cookie 转换仅接受一个精确配置的 Origin，拒绝缺失、重复及相似域名。
func TestBrowserOriginRequiresOneExactConfiguredOrigin(t *testing.T) {
	conf := config.AuthConfig{
		SiteOrigin:  "https://home.example.com",
		SiteOrigins: []string{"https://home.internal.example.com", "https://home.internal.example.com:8443"},
	}
	tests := []struct {
		name    string
		origins []string
	}{
		{name: "missing"},
		{name: "empty", origins: []string{""}},
		{name: "null", origins: []string{"null"}},
		{name: "unknown", origins: []string{"https://unknown.example.com"}},
		{name: "port mismatch", origins: []string{"https://home.internal.example.com:9443"}},
		{name: "protocol mismatch", origins: []string{"http://home.example.com"}},
		{name: "path mismatch", origins: []string{"https://home.example.com/"}},
		{name: "duplicate header", origins: []string{"https://home.example.com", "https://home.example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/auth/browser-login", nil)
			for _, origin := range tt.origins {
				request.Header.Add("Origin", origin)
			}
			require.False(t, browserOriginAllowed(request, conf))
		})
	}
}

func TestConfiguredWebShareOriginRegistersCommonAndCompatibilitySessionEndpoints(t *testing.T) {
	originalConfig, originalRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(originalConfig); data.RouterMap = originalRoutes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	conf := config.Config{}
	conf.WebProjects.Enabled = true
	conf.WebProjects.SiteOrigin = "https://home.example"
	config.SetGlobalConfig(conf)
	require.NoError(t, InitRouter())
	require.Contains(t, data.RouterMap, "/api/auth/browser-login")
	require.Contains(t, data.RouterMap, "/api/web-share/browser-login")
	require.Contains(t, data.RouterMap, "/api/web-projects/browser-login")
}

func TestDisabledWebModuleDoesNotEnableSessionFromUnusedOrigin(t *testing.T) {
	originalConfig, originalRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(originalConfig); data.RouterMap = originalRoutes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	conf := config.Config{}
	conf.WebProjects.SiteOrigin = "https://unused.example"
	config.SetGlobalConfig(conf)
	require.NoError(t, InitRouter())
	require.NotContains(t, data.RouterMap, "/api/auth/browser-login")
}

func TestOriginListAloneRegistersBrowserLogin(t *testing.T) {
	originalConfig, originalRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(originalConfig); data.RouterMap = originalRoutes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	conf := config.Config{}
	conf.Auth.SiteOrigins = []string{"https://home.internal.example.com"}
	config.SetGlobalConfig(conf)
	require.NoError(t, InitRouter())
	require.Contains(t, data.RouterMap, "/api/auth/browser-login")
	require.NotContains(t, data.RouterMap, "/api/web-share/browser-login")
	require.NotContains(t, data.RouterMap, "/api/web-projects/browser-login")
}
