package applications

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/applications"
	"github.com/mcoder2014/home_server/domain/service/passport"
	appErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
)

const (
	ScopeWebProjectsRead  = "web-projects:read"
	ScopeWebProjectsWrite = "web-projects:write"
	ScopeLibraryRead      = "library:read"
	ScopeLibraryWrite     = "library:write"
	ScopeWebDAVRead       = "webdav:read"
	ScopeWebDAVWrite      = "webdav:write"

	StatusEnabledName  = "enabled"
	StatusDisabledName = "disabled"
	StatusRevokedName  = "revoked"

	defaultListLimit    = 20
	maximumListLimit    = 100
	cleanupBatchLimit   = 100
	maxNameRunes        = 128
	maxDescriptionRunes = 2000
)

var orderedScopes = []string{
	ScopeWebProjectsRead,
	ScopeWebProjectsWrite,
	ScopeLibraryRead,
	ScopeLibraryWrite,
	ScopeWebDAVRead,
	ScopeWebDAVWrite,
}

var readForWrite = map[string]string{
	ScopeWebProjectsWrite: ScopeWebProjectsRead,
	ScopeLibraryWrite:     ScopeLibraryRead,
	ScopeWebDAVWrite:      ScopeWebDAVRead,
}

type Repository interface {
	Create(ctx context.Context, application *model.Application, maxActive int) error
	ListOwned(ctx context.Context, ownerUserID, cursor int64, limit int) ([]*model.Application, error)
	GetOwned(ctx context.Context, ownerUserID, applicationID int64) (*model.Application, error)
	GetByAccessKey(ctx context.Context, accessKey string) (*model.Application, error)
	GetByID(ctx context.Context, applicationID int64) (*model.Application, error)
	UpdateOwned(ctx context.Context, application *model.Application, expectedRevision int64) (bool, error)
	StoreIssuedToken(ctx context.Context, application *model.Application, token *model.ApplicationAccessToken, now time.Time) error
	GetTokenByDigest(ctx context.Context, digest []byte) (*model.ApplicationAccessToken, error)
	CleanupExpiredTokens(ctx context.Context, before time.Time, limit int) error
}

type Options struct {
	TokenTTL               time.Duration
	DefaultCredentialTTL   time.Duration
	MaxCredentialTTL       time.Duration
	MaxApplicationsPerUser int
}

type CreateInput struct {
	Name          string
	Description   string
	Scopes        []string
	ExpiresInDays *int
}

type UpdateInput struct {
	Name          *string
	Description   *string
	Scopes        *[]string
	ExpiresInDays *int
	Status        *string
}

type IssuedToken struct {
	AccessToken string
	ExpiresIn   int
	Scopes      []string
}

type Service struct {
	repository Repository
	options    Options
	now        func() time.Time
	random     io.Reader
	userExists func(context.Context, int64) bool
}

func NewService(repo Repository, options Options) *Service {
	service := &Service{
		repository: repo,
		options:    options,
		now:        time.Now,
		random:     rand.Reader,
		userExists: func(_ context.Context, userID int64) bool {
			user, err := passport.GetMockData().GetByID(userID)
			return err == nil && user != nil
		},
	}
	return service
}

func (s *Service) Create(ctx context.Context, ownerUserID int64, input CreateInput) (*model.Application, string, error) {
	if ownerUserID <= 0 {
		return nil, "", appErrors.ErrUnauthorized
	}
	name, description, scopes, ttl, err := s.validateCreate(input)
	if err != nil {
		return nil, "", err
	}
	accessKey, err := s.generateCredential("ak_cq_", 16)
	if err != nil {
		return nil, "", fmt.Errorf("generate access key: %w", appErrors.ErrDependency)
	}
	secretKey, err := s.generateCredential("sk_cq_", 32)
	if err != nil {
		return nil, "", fmt.Errorf("generate secret key: %w", appErrors.ErrDependency)
	}
	now := s.now().UTC()
	digest := sha256.Sum256([]byte(secretKey))
	application := &model.Application{
		OwnerUserID: ownerUserID, Name: name, Description: description, AccessKey: accessKey,
		SecretDigest: digest[:], Scopes: scopes, Status: model.ApplicationStatusEnabled,
		Revision: 1, SecretVersion: 1, ExpiresAt: now.Add(ttl), CreateTime: now, UpdateTime: now,
	}
	if err := s.repository.Create(ctx, application, s.options.MaxApplicationsPerUser); err != nil {
		return nil, "", normalizeRepositoryError(err)
	}
	logApplicationChange(ownerUserID, application.ID, "create")
	return application, secretKey, nil
}

func (s *Service) List(ctx context.Context, ownerUserID, cursor int64, limit int) ([]*model.Application, bool, error) {
	if ownerUserID <= 0 || cursor < 0 {
		return nil, false, appErrors.ErrInvalid
	}
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit < 1 || limit > maximumListLimit {
		return nil, false, appErrors.ErrInvalid
	}
	items, err := s.repository.ListOwned(ctx, ownerUserID, cursor, limit+1)
	if err != nil {
		return nil, false, normalizeRepositoryError(err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func (s *Service) Get(ctx context.Context, ownerUserID, applicationID int64) (*model.Application, error) {
	if ownerUserID <= 0 || applicationID <= 0 {
		return nil, appErrors.ErrInvalid
	}
	application, err := s.repository.GetOwned(ctx, ownerUserID, applicationID)
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if application == nil {
		return nil, appErrors.ErrNotFound
	}
	return application, nil
}

func (s *Service) Update(ctx context.Context, ownerUserID, applicationID, revision int64, input UpdateInput) (*model.Application, error) {
	application, err := s.Get(ctx, ownerUserID, applicationID)
	if err != nil {
		return nil, err
	}
	if application.Status == model.ApplicationStatusRevoked {
		return nil, appErrors.ErrConflict
	}
	if revision <= 0 || application.Revision != revision {
		return nil, appErrors.ErrConflict
	}
	if err := s.applyUpdate(application, input); err != nil {
		return nil, err
	}
	application.Revision++
	application.UpdateTime = s.now().UTC()
	updated, err := s.repository.UpdateOwned(ctx, application, revision)
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if !updated {
		return nil, appErrors.ErrConflict
	}
	logApplicationChange(ownerUserID, application.ID, "update")
	return application, nil
}

func (s *Service) Rotate(ctx context.Context, ownerUserID, applicationID, revision int64) (*model.Application, string, error) {
	application, err := s.Get(ctx, ownerUserID, applicationID)
	if err != nil {
		return nil, "", err
	}
	if application.Status == model.ApplicationStatusRevoked || revision <= 0 || application.Revision != revision {
		return nil, "", appErrors.ErrConflict
	}
	secretKey, err := s.generateCredential("sk_cq_", 32)
	if err != nil {
		return nil, "", fmt.Errorf("generate secret key: %w", appErrors.ErrDependency)
	}
	digest := sha256.Sum256([]byte(secretKey))
	application.SecretDigest = digest[:]
	application.SecretVersion++
	application.Revision++
	application.UpdateTime = s.now().UTC()
	updated, err := s.repository.UpdateOwned(ctx, application, revision)
	if err != nil {
		return nil, "", normalizeRepositoryError(err)
	}
	if !updated {
		return nil, "", appErrors.ErrConflict
	}
	logApplicationChange(ownerUserID, application.ID, "rotate")
	return application, secretKey, nil
}

func (s *Service) Revoke(ctx context.Context, ownerUserID, applicationID, revision int64) (*model.Application, error) {
	application, err := s.Get(ctx, ownerUserID, applicationID)
	if err != nil {
		return nil, err
	}
	if application.Status == model.ApplicationStatusRevoked || revision <= 0 || application.Revision != revision {
		return nil, appErrors.ErrConflict
	}
	application.Status = model.ApplicationStatusRevoked
	application.ActiveSlot = nil
	application.Revision++
	application.UpdateTime = s.now().UTC()
	updated, err := s.repository.UpdateOwned(ctx, application, revision)
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if !updated {
		return nil, appErrors.ErrConflict
	}
	logApplicationChange(ownerUserID, application.ID, "revoke")
	return application, nil
}

func (s *Service) IssueToken(ctx context.Context, accessKey, secretKey string) (*IssuedToken, error) {
	if !validCredential(accessKey, "ak_cq_", 16) || !validCredential(secretKey, "sk_cq_", 32) {
		return nil, appErrors.ErrUnauthorized
	}
	application, err := s.repository.GetByAccessKey(ctx, accessKey)
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if application == nil {
		return nil, appErrors.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(secretKey))
	if subtle.ConstantTimeCompare(application.SecretDigest, digest[:]) != 1 || !s.applicationCanAuthenticate(ctx, application) {
		return nil, appErrors.ErrUnauthorized
	}
	rawToken, err := s.generateCredential("at_cq_", 32)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", appErrors.ErrDependency)
	}
	now := s.now().UTC()
	tokenDigest := sha256.Sum256([]byte(rawToken))
	token := &model.ApplicationAccessToken{
		ApplicationID: application.ID, TokenDigest: tokenDigest[:], SecretVersion: application.SecretVersion,
		ApplicationRevision: application.Revision, ScopeSnapshot: append([]string(nil), application.Scopes...),
		ExpiredAt: now.Add(s.options.TokenTTL), CreateTime: now,
	}
	if err := s.repository.StoreIssuedToken(ctx, application, token, now); err != nil {
		if errors.Is(err, appErrors.ErrUnauthorized) {
			return nil, appErrors.ErrUnauthorized
		}
		return nil, normalizeRepositoryError(err)
	}
	_ = s.repository.CleanupExpiredTokens(ctx, now, cleanupBatchLimit)
	return &IssuedToken{AccessToken: rawToken, ExpiresIn: int(s.options.TokenTTL / time.Second), Scopes: append([]string(nil), application.Scopes...)}, nil
}

func (s *Service) AuthenticateToken(ctx context.Context, rawToken string) (*utils.Principal, error) {
	if !validCredential(rawToken, "at_cq_", 32) {
		return nil, appErrors.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(rawToken))
	token, err := s.repository.GetTokenByDigest(ctx, digest[:])
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if token == nil || !token.ExpiredAt.After(s.now().UTC()) {
		return nil, appErrors.ErrUnauthorized
	}
	application, err := s.repository.GetByID(ctx, token.ApplicationID)
	if err != nil {
		return nil, normalizeRepositoryError(err)
	}
	if application == nil || token.SecretVersion != application.SecretVersion || token.ApplicationRevision != application.Revision || !sameScopes(token.ScopeSnapshot, application.Scopes) || !s.applicationCanAuthenticate(ctx, application) {
		return nil, appErrors.ErrUnauthorized
	}
	return &utils.Principal{Kind: "application", UserID: application.OwnerUserID, ApplicationID: application.ID, Scopes: append([]string(nil), token.ScopeSnapshot...)}, nil
}

func (s *Service) validateCreate(input CreateInput) (string, string, []string, time.Duration, error) {
	name, description, err := validateText(input.Name, input.Description)
	if err != nil {
		return "", "", nil, 0, err
	}
	scopes, err := normalizeScopes(input.Scopes, true)
	if err != nil {
		return "", "", nil, 0, err
	}
	ttl := s.options.DefaultCredentialTTL
	if input.ExpiresInDays != nil {
		maxDays := int(s.options.MaxCredentialTTL / (24 * time.Hour))
		if *input.ExpiresInDays < 1 || *input.ExpiresInDays > maxDays {
			return "", "", nil, 0, appErrors.ErrInvalid
		}
		ttl = time.Duration(*input.ExpiresInDays) * 24 * time.Hour
	}
	if ttl <= 0 || ttl > s.options.MaxCredentialTTL {
		return "", "", nil, 0, appErrors.ErrInvalid
	}
	return name, description, scopes, ttl, nil
}

func (s *Service) applyUpdate(application *model.Application, input UpdateInput) error {
	if input.Name == nil && input.Description == nil && input.Scopes == nil && input.ExpiresInDays == nil && input.Status == nil {
		return appErrors.ErrInvalid
	}
	name, description := application.Name, application.Description
	if input.Name != nil {
		name = *input.Name
	}
	if input.Description != nil {
		description = *input.Description
	}
	var err error
	application.Name, application.Description, err = validateText(name, description)
	if err != nil {
		return err
	}
	if input.Scopes != nil {
		application.Scopes, err = normalizeScopes(*input.Scopes, false)
		if err != nil {
			return err
		}
	}
	if input.ExpiresInDays != nil {
		maxDays := int(s.options.MaxCredentialTTL / (24 * time.Hour))
		if *input.ExpiresInDays < 1 || *input.ExpiresInDays > maxDays {
			return appErrors.ErrInvalid
		}
		ttl := time.Duration(*input.ExpiresInDays) * 24 * time.Hour
		application.ExpiresAt = s.now().UTC().Add(ttl)
	}
	if input.Status != nil {
		switch *input.Status {
		case StatusEnabledName:
			application.Status = model.ApplicationStatusEnabled
		case StatusDisabledName:
			application.Status = model.ApplicationStatusDisabled
		default:
			return appErrors.ErrInvalid
		}
	}
	return nil
}

func (s *Service) applicationCanAuthenticate(ctx context.Context, application *model.Application) bool {
	now := s.now().UTC()
	return application.Status == model.ApplicationStatusEnabled && application.ExpiresAt.After(now) && s.userExists(ctx, application.OwnerUserID)
}

func (s *Service) generateCredential(prefix string, byteCount int) (string, error) {
	random := make([]byte, byteCount)
	if _, err := io.ReadFull(s.random, random); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func validCredential(value, prefix string, byteCount int) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == byteCount && len(value) == len(prefix)+base64.RawURLEncoding.EncodedLen(byteCount)
}

func validateText(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes || !utf8.ValidString(description) || utf8.RuneCountInString(description) > maxDescriptionRunes {
		return "", "", appErrors.ErrInvalid
	}
	return name, description, nil
}

func normalizeScopes(scopes []string, useDefault bool) ([]string, error) {
	if scopes == nil && useDefault {
		return []string{ScopeWebProjectsRead}, nil
	}
	allowed := make(map[string]bool, len(orderedScopes))
	for _, scope := range orderedScopes {
		allowed[scope] = true
	}
	selected := make(map[string]bool, len(scopes)+3)
	for _, scope := range scopes {
		if !allowed[scope] {
			return nil, appErrors.ErrInvalid
		}
		selected[scope] = true
		if readScope := readForWrite[scope]; readScope != "" {
			selected[readScope] = true
		}
	}
	result := make([]string, 0, len(selected))
	for _, scope := range orderedScopes {
		if selected[scope] {
			result = append(result, scope)
		}
	}
	return result, nil
}

func sameScopes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func normalizeRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, appErrors.ErrRateLimited) || errors.Is(err, appErrors.ErrConflict) {
		return err
	}
	return fmt.Errorf("application repository: %w", appErrors.ErrDependency)
}

func logApplicationChange(ownerUserID, applicationID int64, action string) {
	logrus.WithFields(logrus.Fields{
		"event": "application_credential_change", "owner_user_id": ownerUserID,
		"application_id": applicationID, "action": action, "result": "success",
	}).Info("application credential changed")
}

var (
	defaultService *Service
	defaultLock    sync.RWMutex
)

func Init(conf config.AuthConfig) error {
	if conf.TokenTTLSeconds < 0 || conf.DefaultCredentialTTLDays < 0 || conf.MaxCredentialTTLDays < 0 || conf.MaxApplicationsPerUser < 0 {
		return fmt.Errorf("invalid auth application limits")
	}
	if conf.TokenTTLSeconds == 0 {
		conf.TokenTTLSeconds = 900
	}
	if conf.DefaultCredentialTTLDays == 0 {
		conf.DefaultCredentialTTLDays = 90
	}
	if conf.MaxCredentialTTLDays == 0 {
		conf.MaxCredentialTTLDays = 365
	}
	if conf.MaxApplicationsPerUser == 0 {
		conf.MaxApplicationsPerUser = 20
	}
	if conf.TokenTTLSeconds > 3600 || conf.DefaultCredentialTTLDays > 3650 || conf.MaxCredentialTTLDays > 3650 || conf.MaxApplicationsPerUser > 100 {
		return fmt.Errorf("invalid auth application limits")
	}
	options := Options{
		TokenTTL:               time.Duration(conf.TokenTTLSeconds) * time.Second,
		DefaultCredentialTTL:   time.Duration(conf.DefaultCredentialTTLDays) * 24 * time.Hour,
		MaxCredentialTTL:       time.Duration(conf.MaxCredentialTTLDays) * 24 * time.Hour,
		MaxApplicationsPerUser: conf.MaxApplicationsPerUser,
	}
	if options.TokenTTL <= 0 || options.DefaultCredentialTTL <= 0 || options.MaxCredentialTTL <= 0 || options.DefaultCredentialTTL > options.MaxCredentialTTL || options.MaxApplicationsPerUser <= 0 {
		return fmt.Errorf("invalid auth application limits")
	}
	defaultLock.Lock()
	defaultService = NewService(&repository.Repository{}, options)
	defaultLock.Unlock()
	return nil
}

func Default() *Service {
	defaultLock.RLock()
	service := defaultService
	defaultLock.RUnlock()
	return service
}

func AuthenticateToken(ctx context.Context, rawToken string) (*utils.Principal, error) {
	service := Default()
	if service == nil {
		return nil, appErrors.ErrUnauthorized
	}
	return service.AuthenticateToken(ctx, rawToken)
}
