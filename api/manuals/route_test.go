package manuals

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/stretchr/testify/require"
)

func TestManualRoutesSetNoStoreBeforeModuleDecision(t *testing.T) {
	before := config.Global()
	mode := gin.Mode()
	t.Cleanup(func() {
		config.SetGlobalConfig(before)
		gin.SetMode(mode)
	})
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		config.SetGlobalConfig(config.Config{Manuals: config.ManualsConfig{Enabled: enabled}})
		router := gin.New()
		router.Use(requireModule)
		router.GET("/api/manuals", func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/manuals", nil)
		router.ServeHTTP(response, request)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		if enabled {
			require.Equal(t, http.StatusNoContent, response.Code)
		} else {
			require.NotEqual(t, http.StatusNoContent, response.Code)
		}
	}
}

func TestManualOptionalReaderRejectsExplicitEmptyCredential(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/api/manuals", nil)
	principal, err := optionalReader(context)
	require.NoError(t, err)
	require.Nil(t, principal)

	context.Request.Header["Authorization"] = []string{""}
	principal, err = optionalReader(context)
	require.Error(t, err)
	require.Nil(t, principal)
}
