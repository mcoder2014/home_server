package manuals

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

func TestManualWritePolicyLegacyIdentityRejectsMissingTokenAndRechecksExpiry(t *testing.T) {
	before := config.Global()
	t.Cleanup(func() {
		config.SetGlobalConfig(before)
		_ = passport.GetMockData().LoadConf("[]")
	})
	config.SetGlobalConfig(config.Config{IdentitySource: "config", Manuals: config.ManualsConfig{Enabled: true}})
	encoded, err := json.Marshal([]*model.UserIdentity{{ID: 101, UserName: "owner"}})
	require.NoError(t, err)
	require.NoError(t, passport.GetMockData().LoadConf(string(encoded)))
	principal := &utils.Principal{Kind: "user", UserID: 101, TokenExpiresAt: time.Now().Add(time.Minute)}

	require.ErrorIs(t, requireWritePolicy(context.Background(), nil, 101, principal), apperrors.ErrUnauthorized)
	wrongTokenType := context.WithValue(context.Background(), utils.CtxKeyLoginToken, 101)
	require.ErrorIs(t, requireWritePolicy(wrongTokenType, nil, 101, principal), apperrors.ErrUnauthorized)
	principal.TokenExpiresAt = time.Now().Add(-time.Second)
	require.ErrorIs(t, requireWritePolicy(context.Background(), nil, 101, principal), apperrors.ErrUnauthorized)
	principal.TokenExpiresAt = time.Now().Add(time.Minute)
	require.ErrorIs(t, requireWritePolicy(context.Background(), nil, 202, principal), apperrors.ErrUnauthorized)
}
