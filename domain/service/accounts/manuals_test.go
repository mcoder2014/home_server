package accounts

import (
	"context"
	"testing"

	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestManualsModuleUsesStaticConfigurationWithoutDatabaseNamespace(t *testing.T) {
	before := config.Global()
	t.Cleanup(func() {
		config.SetGlobalConfig(before)
	})
	config.SetGlobalConfig(config.Config{ConfigSource: "database", Manuals: config.ManualsConfig{Enabled: true}})

	enabled, err := ModuleEnabled(context.Background(), "manuals")
	require.NoError(t, err)
	require.True(t, enabled)
	enabled, err = EnabledTx(nil, "manuals", "enabled", true)
	require.NoError(t, err)
	require.True(t, enabled)
	_, err = EnabledTx(nil, "manuals", "unknown", false)
	require.ErrorIs(t, err, apperrors.ErrDependency)
}
