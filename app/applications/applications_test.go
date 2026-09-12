package applications

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

func TestApplicationCredentialViewNeverContainsCredentialDigestsOrOwner(t *testing.T) {
	now := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)
	lastIssued := now.Add(-time.Minute)
	application := &model.Application{
		ID: 123, OwnerUserID: 999, Name: "build agent", Description: "deploys static sites",
		AccessKey: "ak_cq_public", SecretDigest: []byte("must never leave the server"),
		Scopes: []string{"web-projects:read"}, Status: model.ApplicationStatusEnabled,
		Revision: 4, SecretVersion: 2, CreateTime: now.Add(-time.Hour), UpdateTime: now, ExpiresAt: now.Add(24 * time.Hour), LastIssuedAt: &lastIssued,
	}

	view := buildApplicationCredentialView(application)
	require.Equal(t, "123", view.ID)
	require.Equal(t, "enabled", view.Status)
	require.Equal(t, application.AccessKey, view.AccessKey)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret_digest")
	require.NotContains(t, string(encoded), "secret_key")
	require.NotContains(t, string(encoded), "owner_user_id")
}

func TestApplicationCredentialManagementRequiresUserActor(t *testing.T) {
	ownerID, err := requireApplicationCredentialOwner(&utils.Principal{Kind: "user", UserID: 101})
	require.NoError(t, err)
	require.Equal(t, int64(101), ownerID)
	_, err = requireApplicationCredentialOwner(&utils.Principal{Kind: "application", UserID: 101, ApplicationID: 22})
	require.Error(t, err)
	_, err = requireApplicationCredentialOwner(nil)
	require.Error(t, err)
}

func TestApplicationCredentialStatusNameRejectsUnknownDatabaseValue(t *testing.T) {
	require.Equal(t, "enabled", applicationCredentialStatusName(model.ApplicationStatusEnabled))
	require.Equal(t, "disabled", applicationCredentialStatusName(model.ApplicationStatusDisabled))
	require.Equal(t, "revoked", applicationCredentialStatusName(model.ApplicationStatusRevoked))
	require.Empty(t, applicationCredentialStatusName(99))
}
