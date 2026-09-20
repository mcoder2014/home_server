package resourcepasswords

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateManagedPasswordUsesUTF8Bytes(t *testing.T) {
	require.NoError(t, ValidateManagedPassword("12345678"))
	require.NoError(t, ValidateManagedPassword(strings.Repeat("界", 24)))
	require.ErrorIs(t, ValidateManagedPassword("1234567"), apperrors.ErrInvalid)
	require.ErrorIs(t, ValidateManagedPassword(strings.Repeat("界", 25)), apperrors.ErrInvalid)
	require.ErrorIs(t, ValidateManagedPassword(string([]byte{0xff, 0xfe})), apperrors.ErrInvalid)
	require.NoError(t, ValidateManagedPassword(""), "empty password clears protection")
}

func TestGrantBindsResourceVersionAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 2, 3, 0, time.UTC)
	service := newTestService([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now })
	record := &model.ResourcePassword{ResourceType: string(ResourceWebProject), ResourceID: 101, PasswordHash: "$2a$04$0d3NQ5z/0G4xP8xPtp0seux64uXXToQxVrYVSlGBXkMZ2HfW0Z4oS", Version: 7}
	grant, err := service.issueGrant(ResourceWebProject, 101, 7)
	require.NoError(t, err)
	require.NoError(t, service.authorizeRecord(record, false, grant.Token))
	require.ErrorIs(t, service.authorizeRecord(record, false, ""), ErrPasswordRequired)

	changed := *record
	changed.Version++
	require.ErrorIs(t, service.authorizeRecord(&changed, false, grant.Token), ErrPasswordRequired)
	require.NoError(t, service.authorizeRecord(record, true, ""), "owner bypasses only the password layer")

	now = now.Add(GrantTTL + time.Second)
	require.ErrorIs(t, service.authorizeRecord(record, false, grant.Token), ErrPasswordRequired)
}

func TestGrantCookieIsHostOnlySecureAndHttpOnly(t *testing.T) {
	expires := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	cookie := GrantCookie(ResourceManual, 22, Grant{Token: "signed", ExpiresAt: expires})
	require.Equal(t, "__Host-cq_read_manual_22", cookie.Name)
	require.Equal(t, "/", cookie.Path)
	require.Empty(t, cookie.Domain)
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Equal(t, int(GrantTTL/time.Second), cookie.MaxAge)
	require.Equal(t, expires, cookie.Expires)
}

func TestAttemptLimiterCapsKeysAndConcurrentChecks(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(2, 1, 2, time.Minute, func() time.Time { return now })
	release, err := limiter.acquire("first")
	require.NoError(t, err)
	_, err = limiter.acquire("second")
	require.ErrorIs(t, err, apperrors.ErrRateLimited, "bcrypt concurrency is checked before work starts")
	release()

	release, err = limiter.acquire("second")
	require.NoError(t, err)
	release()
	_, err = limiter.acquire("third")
	require.ErrorIs(t, err, apperrors.ErrRateLimited, "tracked keys stay bounded")

	now = now.Add(2 * time.Minute)
	release, err = limiter.acquire("third")
	require.NoError(t, err, "expired keys are pruned before rejecting new clients")
	release()
	limiter.success("third")
	require.LessOrEqual(t, limiter.size(), 1)
}

func TestAuthorizeUnprotectedAndOwnerDoNotNeedGrant(t *testing.T) {
	service := newTestService([]byte("0123456789abcdef0123456789abcdef"), time.Now)
	require.NoError(t, service.authorizeRecord(nil, false, ""))
	require.NoError(t, service.authorizeRecord(&model.ResourcePassword{PasswordHash: "", Version: 3}, false, ""))
	require.NoError(t, service.authorizeRecord(&model.ResourcePassword{PasswordHash: "protected", Version: 3}, true, ""))
	require.True(t, errors.Is(service.authorizeRecord(&model.ResourcePassword{PasswordHash: "protected", Version: 3}, false, "bad"), ErrPasswordRequired))
}
