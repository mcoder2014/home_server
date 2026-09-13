package webprojects

import (
	"net/http"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"github.com/stretchr/testify/require"
)

func TestInitRouterRegistersPrimaryAndCompatibilityWebShareAPIs(t *testing.T) {
	originalConfig, originalRoutes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(originalConfig); data.RouterMap = originalRoutes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	conf := config.Config{}
	conf.WebProjects.Enabled = true
	config.SetGlobalConfig(conf)

	require.NoError(t, InitRouter())
	for _, route := range []string{"/api/web-share", "/api/web-share/:id", "/api/web-projects", "/api/web-projects/:id"} {
		require.Contains(t, data.RouterMap, route)
	}
	require.Contains(t, data.RouterMap["/api/web-share"], http.MethodGet)
	require.Contains(t, data.RouterMap["/api/web-share"], http.MethodPost)
}

func TestParseIfMatch(t *testing.T) {
	for _, value := range []string{"7", `"7"`} {
		revision, err := parseIfMatch(value)
		require.NoError(t, err)
		require.Equal(t, int64(7), revision)
	}
	for _, value := range []string{"", "0", "-1", "W/\"7\"", `"7`, "abc"} {
		_, err := parseIfMatch(value)
		require.Error(t, err, value)
	}
}

func TestValidateProjectTarget(t *testing.T) {
	for _, target := range []string{"/p/report/", "/p/report/guide.html", "/p/report/#/history"} {
		require.NoError(t, validateProjectTarget(target), target)
	}
	for _, target := range []string{"", "/p/", "/login", "https://evil.example/p/report/", "//evil.example/p/report/", `/p/report/\\evil`, "/p/report/\nheader", "/p/report/../../login", "/p/report/%2e%2e/%2e%2e/login", "/p/report/%5cevil", "/p/report/%00evil"} {
		require.Error(t, validateProjectTarget(target), target)
	}
}

func TestIsDocumentNavigation(t *testing.T) {
	require.True(t, isDocumentNavigation("GET", "/p/report/", "text/html,application/xhtml+xml"))
	require.True(t, isDocumentNavigation("GET", "/p/report/guide.html", "text/html"))
	require.False(t, isDocumentNavigation("HEAD", "/p/report/", "text/html"))
	require.False(t, isDocumentNavigation("GET", "/p/report/assets/app.js", "text/html,*/*"))
	require.False(t, isDocumentNavigation("GET", "/p/report/data.json", "text/html,*/*"))
}
