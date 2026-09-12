package auth

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLegacyWebOriginRegistersCommonAndCompatibilitySessionEndpoints(t *testing.T) {
	originalConfig, originalRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(originalConfig); data.RouterMap = originalRoutes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	conf := config.Config{}
	conf.WebProjects.Enabled = true
	conf.WebProjects.SiteOrigin = "https://home.example"
	config.SetGlobalConfig(conf)
	require.NoError(t, InitRouter())
	require.Contains(t, data.RouterMap, "/api/auth/browser-login")
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
