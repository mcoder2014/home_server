package applications

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	appErrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

// TestCreateGeneratesOpaqueCredentialsAndStoresOnlyDigest 用固定时间和随机源核对凭据格式、默认范围与有效期，以及 SK 摘要存储。
func TestCreateGeneratesOpaqueCredentialsAndStoresOnlyDigest(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := NewService(repo, Options{TokenTTL: 15 * time.Minute, DefaultCredentialTTL: 90 * 24 * time.Hour, MaxCredentialTTL: 365 * 24 * time.Hour, MaxApplicationsPerUser: 20})
	service.now = func() time.Time { return now }
	service.random = bytes.NewReader(bytes.Repeat([]byte{7}, 256))

	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "deploy bot", Scopes: nil})
	require.NoError(t, err)
	require.Regexp(t, `^ak_cq_[A-Za-z0-9_-]{22}$`, application.AccessKey)
	require.Regexp(t, `^sk_cq_[A-Za-z0-9_-]{43}$`, secret)
	require.NotEqual(t, []byte(secret), application.SecretDigest)
	require.Len(t, application.SecretDigest, 32)
	require.Equal(t, []string{ScopeWebProjectsRead}, application.Scopes)
	require.Equal(t, now.Add(90*24*time.Hour), application.ExpiresAt)

	stored := repo.applications[application.ID]
	require.NotContains(t, string(stored.SecretDigest), secret)
	require.NotContains(t, stored.Name, secret)
	require.Empty(t, stored.LastIssuedAt)
}

func TestManualsWriteScopeIncludesReadScope(t *testing.T) {
	scopes, err := normalizeScopes([]string{ScopeManualsWrite}, false)
	require.NoError(t, err)
	require.Equal(t, []string{ScopeManualsRead, ScopeManualsWrite}, scopes)
}

func TestOwnerIsolationAndRevisionProtection(t *testing.T) {
	service, repo, now := testService()
	application, _, err := service.Create(context.Background(), 101, CreateInput{Name: "owner app", Scopes: []string{ScopeLibraryRead}})
	require.NoError(t, err)

	_, err = service.Get(context.Background(), 202, application.ID)
	require.ErrorIs(t, err, appErrors.ErrNotFound)
	_, err = service.Update(context.Background(), 202, application.ID, application.Revision, UpdateInput{Name: stringPointer("stolen")})
	require.ErrorIs(t, err, appErrors.ErrNotFound)
	_, err = service.Update(context.Background(), 101, application.ID, application.Revision+1, UpdateInput{Name: stringPointer("stale")})
	require.ErrorIs(t, err, appErrors.ErrConflict)

	updated, err := service.Update(context.Background(), 101, application.ID, application.Revision, UpdateInput{Status: stringPointer(StatusDisabledName)})
	require.NoError(t, err)
	require.Equal(t, model.ApplicationStatusDisabled, updated.Status)
	require.Equal(t, application.Revision+1, updated.Revision)
	require.Equal(t, now, repo.applications[application.ID].UpdateTime)
}

// TestIssueAndAuthenticateTokenRejectsExpiredRevokedAndStaleVersions 核对正常令牌身份，并验证修订变更、撤销或凭据过期会拒绝认证。
func TestIssueAndAuthenticateTokenRejectsExpiredRevokedAndStaleVersions(t *testing.T) {
	service, repo, now := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "reader", Scopes: []string{ScopeWebProjectsWrite}})
	require.NoError(t, err)

	issued, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	require.Regexp(t, `^at_cq_[A-Za-z0-9_-]{43}$`, issued.AccessToken)
	require.Equal(t, 900, issued.ExpiresIn)
	require.Equal(t, []string{ScopeWebProjectsRead, ScopeWebProjectsWrite}, issued.Scopes)
	require.Empty(t, repo.plaintextTokens)

	principal, err := service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "application", principal.Kind)
	require.Equal(t, int64(101), principal.UserID)
	require.Equal(t, application.ID, principal.ApplicationID)

	stored := repo.applications[application.ID]
	stored.Revision++
	repo.applications[application.ID] = stored
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)

	issued = issueFreshToken(t, service, repo, application.ID, secret)
	stored = repo.applications[application.ID]
	stored.Status = model.ApplicationStatusRevoked
	repo.applications[application.ID] = stored
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)

	stored.Status = model.ApplicationStatusEnabled
	stored.ExpiresAt = now.Add(-time.Second)
	repo.applications[application.ID] = stored
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
}

func TestRotateImmediatelyInvalidatesOldSecretAndToken(t *testing.T) {
	service, _, _ := testService()
	application, oldSecret, err := service.Create(context.Background(), 101, CreateInput{Name: "rotating", Scopes: []string{ScopeWebDAVRead}})
	require.NoError(t, err)
	oldToken, err := service.IssueToken(context.Background(), application.AccessKey, oldSecret)
	require.NoError(t, err)

	rotated, newSecret, err := service.Rotate(context.Background(), 101, application.ID, application.Revision)
	require.NoError(t, err)
	require.NotEqual(t, oldSecret, newSecret)
	require.Equal(t, application.SecretVersion+1, rotated.SecretVersion)
	_, err = service.IssueToken(context.Background(), application.AccessKey, oldSecret)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
	_, err = service.AuthenticateToken(context.Background(), oldToken.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
}

// TestExpiredTokenAndDisableEnableCycleCannotReviveToken 确认到期令牌被拒绝，应用停用再启用也不能复活旧令牌。
func TestExpiredTokenAndDisableEnableCycleCannotReviveToken(t *testing.T) {
	service, repo, now := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "lifecycle"})
	require.NoError(t, err)
	issued, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	for _, token := range repo.tokens {
		token.ExpiredAt = now
	}
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)

	issued, err = service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	disabled, err := service.Update(context.Background(), 101, application.ID, application.Revision, UpdateInput{Status: stringPointer(StatusDisabledName)})
	require.NoError(t, err)
	enabled, err := service.Update(context.Background(), 101, application.ID, disabled.Revision, UpdateInput{Status: stringPointer(StatusEnabledName)})
	require.NoError(t, err)
	require.Equal(t, model.ApplicationStatusEnabled, enabled.Status)
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
}

func TestChangingScopesInvalidatesExistingToken(t *testing.T) {
	service, _, _ := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "scoped", Scopes: []string{ScopeWebProjectsRead}})
	require.NoError(t, err)
	issued, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	newScopes := []string{ScopeLibraryRead}
	updated, err := service.Update(context.Background(), 101, application.ID, application.Revision, UpdateInput{Scopes: &newScopes})
	require.NoError(t, err)
	require.Equal(t, newScopes, updated.Scopes)
	_, err = service.AuthenticateToken(context.Background(), issued.AccessToken)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
}

func TestScopeValidationAndWriteImpliesRead(t *testing.T) {
	service, _, _ := testService()
	application, _, err := service.Create(context.Background(), 101, CreateInput{Name: "writer", Scopes: []string{ScopeLibraryWrite, ScopeLibraryWrite}})
	require.NoError(t, err)
	require.Equal(t, []string{ScopeLibraryRead, ScopeLibraryWrite}, application.Scopes)
	commentApplication, _, err := service.Create(context.Background(), 101, CreateInput{Name: "commenter", Scopes: []string{ScopeWebCommentsWrite}})
	require.NoError(t, err)
	require.Equal(t, []string{ScopeWebCommentsRead, ScopeWebCommentsWrite}, commentApplication.Scopes)

	_, _, err = service.Create(context.Background(), 101, CreateInput{Name: "bad", Scopes: []string{"admin:*"}})
	require.ErrorIs(t, err, appErrors.ErrInvalid)
	_, _, err = service.Create(context.Background(), 0, CreateInput{Name: "no owner"})
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
	zeroDays := 0
	_, _, err = service.Create(context.Background(), 101, CreateInput{Name: "no ttl", ExpiresInDays: &zeroDays})
	require.ErrorIs(t, err, appErrors.ErrInvalid)
}

func TestIssuedTokenCannotOutliveApplication(t *testing.T) {
	service, repo, now := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "short-lived"})
	require.NoError(t, err)
	stored := repo.applications[application.ID]
	stored.ExpiresAt = now.Add(2 * time.Minute)
	repo.applications[application.ID] = stored

	issued, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	require.Equal(t, 120, issued.ExpiresIn)
	for _, token := range repo.tokens {
		require.Equal(t, stored.ExpiresAt, token.ExpiredAt)
	}
}

func TestIssueTokenValidatesUserAndParameters(t *testing.T) {
	service, _, _ := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "client"})
	require.NoError(t, err)

	_, err = service.IssueToken(context.Background(), application.AccessKey, "sk_cq_short")
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
	_, err = service.IssueToken(context.Background(), "ak_cq_short", secret)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)

	service.userExists = func(context.Context, int64) bool { return false }
	_, err = service.IssueToken(context.Background(), application.AccessKey, secret)
	require.ErrorIs(t, err, appErrors.ErrUnauthorized)
}

func TestApplicationLimitExcludesRevoked(t *testing.T) {
	service, _, _ := testService()
	service.options.MaxApplicationsPerUser = 1
	application, _, err := service.Create(context.Background(), 101, CreateInput{Name: "first"})
	require.NoError(t, err)
	_, _, err = service.Create(context.Background(), 101, CreateInput{Name: "second"})
	require.ErrorIs(t, err, appErrors.ErrRateLimited)
	_, err = service.Revoke(context.Background(), 101, application.ID, application.Revision)
	require.NoError(t, err)
	_, _, err = service.Create(context.Background(), 101, CreateInput{Name: "replacement"})
	require.NoError(t, err)
}

func TestInitAppliesSafeDefaultsAndRejectsUnsafeLimits(t *testing.T) {
	require.NoError(t, Init(config.AuthConfig{}))
	configured := Default()
	require.Equal(t, 15*time.Minute, configured.options.TokenTTL)
	require.Equal(t, 90*24*time.Hour, configured.options.DefaultCredentialTTL)
	require.Equal(t, 365*24*time.Hour, configured.options.MaxCredentialTTL)
	require.Equal(t, 20, configured.options.MaxApplicationsPerUser)

	require.Error(t, Init(config.AuthConfig{TokenTTLSeconds: -1}))
	require.Error(t, Init(config.AuthConfig{TokenTTLSeconds: 3601}))
	require.Error(t, Init(config.AuthConfig{DefaultCredentialTTLDays: 366, MaxCredentialTTLDays: 365}))
	require.Error(t, Init(config.AuthConfig{MaxApplicationsPerUser: 101}))
}

type memoryRepository struct {
	applications    map[int64]*model.Application
	tokens          map[string]*model.ApplicationAccessToken
	nextID          int64
	plaintextTokens []string
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{applications: make(map[int64]*model.Application), tokens: make(map[string]*model.ApplicationAccessToken), nextID: 1000}
}

func (r *memoryRepository) Create(_ context.Context, application *model.Application, maxActive int) error {
	active := 0
	for _, existing := range r.applications {
		if existing.OwnerUserID == application.OwnerUserID && existing.Status != model.ApplicationStatusRevoked {
			active++
		}
	}
	if active >= maxActive {
		return appErrors.ErrRateLimited
	}
	r.nextID++
	copy := cloneApplication(application)
	copy.ID = r.nextID
	application.ID = copy.ID
	r.applications[copy.ID] = copy
	return nil
}

func (r *memoryRepository) ListOwned(_ context.Context, ownerUserID, cursor int64, limit int) ([]*model.Application, error) {
	result := make([]*model.Application, 0, limit)
	for id := r.nextID; id > 0 && len(result) < limit; id-- {
		application := r.applications[id]
		if application != nil && application.OwnerUserID == ownerUserID && (cursor == 0 || id < cursor) {
			result = append(result, cloneApplication(application))
		}
	}
	return result, nil
}

func (r *memoryRepository) GetOwned(_ context.Context, ownerUserID, applicationID int64) (*model.Application, error) {
	application := r.applications[applicationID]
	if application == nil || application.OwnerUserID != ownerUserID {
		return nil, nil
	}
	return cloneApplication(application), nil
}

func (r *memoryRepository) GetByAccessKey(_ context.Context, accessKey string) (*model.Application, error) {
	for _, application := range r.applications {
		if application.AccessKey == accessKey {
			return cloneApplication(application), nil
		}
	}
	return nil, nil
}

func (r *memoryRepository) GetByID(_ context.Context, applicationID int64) (*model.Application, error) {
	if r.applications[applicationID] == nil {
		return nil, nil
	}
	return cloneApplication(r.applications[applicationID]), nil
}

func (r *memoryRepository) UpdateOwned(_ context.Context, application *model.Application, expectedRevision int64) (bool, error) {
	stored := r.applications[application.ID]
	if stored == nil || stored.OwnerUserID != application.OwnerUserID {
		return false, nil
	}
	if stored.Revision != expectedRevision {
		return false, appErrors.ErrConflict
	}
	r.applications[application.ID] = cloneApplication(application)
	return true, nil
}

func (r *memoryRepository) StoreIssuedToken(_ context.Context, application *model.Application, token *model.ApplicationAccessToken, now time.Time) error {
	stored := r.applications[application.ID]
	if stored == nil || stored.Revision != application.Revision || stored.SecretVersion != application.SecretVersion || stored.Status != model.ApplicationStatusEnabled {
		return appErrors.ErrUnauthorized
	}
	stored.LastIssuedAt = &now
	r.tokens[string(token.TokenDigest)] = token
	return nil
}

func (r *memoryRepository) GetTokenByDigest(_ context.Context, digest []byte) (*model.ApplicationAccessToken, error) {
	token := r.tokens[string(digest)]
	if token == nil {
		return nil, nil
	}
	copy := *token
	return &copy, nil
}

func (r *memoryRepository) CleanupExpiredTokens(context.Context, time.Time, int) error { return nil }

func testService() (*Service, *memoryRepository, time.Time) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	random := &incrementReader{}
	service := NewService(repo, Options{TokenTTL: 15 * time.Minute, DefaultCredentialTTL: 90 * 24 * time.Hour, MaxCredentialTTL: 365 * 24 * time.Hour, MaxApplicationsPerUser: 20})
	service.now = func() time.Time { return now }
	service.random = random
	service.userExists = func(_ context.Context, userID int64) bool { return userID == 101 }
	return service, repo, now
}

func issueFreshToken(t *testing.T, service *Service, repo *memoryRepository, applicationID int64, secret string) *IssuedToken {
	t.Helper()
	application := repo.applications[applicationID]
	issued, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	return issued
}

func cloneApplication(application *model.Application) *model.Application {
	copy := *application
	copy.SecretDigest = append([]byte(nil), application.SecretDigest...)
	copy.Scopes = append([]string(nil), application.Scopes...)
	return &copy
}

func stringPointer(value string) *string { return &value }

var _ Repository = (*memoryRepository)(nil)

type incrementReader struct{ next byte }

func (r *incrementReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		r.next++
		buffer[index] = r.next
	}
	return len(buffer), nil
}

func TestAuthenticateTokenCarriesOriginalWriteAuthorizationSnapshot(t *testing.T) {
	service, _, now := testService()
	application, secret, err := service.Create(context.Background(), 101, CreateInput{Name: "snapshot", Scopes: []string{ScopeWebProjectsWrite}})
	require.NoError(t, err)
	token, err := service.IssueToken(context.Background(), application.AccessKey, secret)
	require.NoError(t, err)
	principal, err := service.AuthenticateToken(context.Background(), token.AccessToken)
	require.NoError(t, err)
	require.Equal(t, application.Revision, principal.ApplicationRevision)
	require.Equal(t, application.SecretVersion, principal.SecretVersion)
	require.Equal(t, now.Add(15*time.Minute), principal.TokenExpiresAt)
}
