package resourcepasswords

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type ResourceType string

const (
	ResourceWebProject ResourceType = "web_project"
	ResourceManual     ResourceType = "manual"
	GrantTTL                        = time.Hour
	grantFormatVersion byte         = 1
)

var ErrPasswordRequired = apperrors.WithMessage(apperrors.ErrForbidden, "resource password required")

type State struct {
	PasswordProtected bool  `json:"password_protected"`
	Version           int64 `json:"version"`
}

type UpdateInput struct {
	Password string `json:"password"`
	Version  int64  `json:"version"`
}

type UnlockInput struct {
	Password string `json:"password"`
}

type Grant struct {
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	secret   []byte
	now      func() time.Time
	random   io.Reader
	attempts *attemptLimiter
}

var Default = mustDefaultService()

func mustDefaultService() *Service {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		panic("initialize resource password signer: " + err.Error())
	}
	return newService(secret, time.Now, rand.Reader)
}

func New(secret []byte) (*Service, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("resource password signing secret must contain at least 32 bytes")
	}
	return newService(secret, time.Now, rand.Reader), nil
}

func newTestService(secret []byte, now func() time.Time) *Service {
	return newService(secret, now, strings.NewReader(strings.Repeat("x", 128)))
}

func newService(secret []byte, now func() time.Time, random io.Reader) *Service {
	copySecret := append([]byte(nil), secret...)
	return &Service{
		secret: copySecret, now: now, random: random,
		attempts: newAttemptLimiter(4096, 8, 8, time.Minute, now),
	}
}

func ValidateManagedPassword(password string) error {
	if password == "" {
		return nil
	}
	if !utf8.ValidString(password) || len([]byte(password)) < config.Runtime().AccountPolicy.MinSharePasswordLength || len([]byte(password)) > 72 {
		return apperrors.ErrInvalid
	}
	return nil
}

func PrepareManagedPassword(password string) (string, error) {
	if err := ValidateManagedPassword(password); err != nil {
		return "", err
	}
	if password == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", apperrors.ErrDependency
	}
	return string(hash), nil
}

func (service *Service) State(ctx context.Context, resourceType ResourceType, resourceID int64) (*State, error) {
	if !validResource(resourceType, resourceID) || db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	record, err := dal.FindResourcePassword(db.MasterDB().WithContext(ctx), string(resourceType), resourceID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	return stateOf(record), nil
}

func (service *Service) States(ctx context.Context, resourceType ResourceType, resourceIDs []int64) (map[int64]State, error) {
	result := make(map[int64]State, len(resourceIDs))
	if !validType(resourceType) || db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	for _, resourceID := range resourceIDs {
		if resourceID <= 0 {
			return nil, apperrors.ErrInvalid
		}
		result[resourceID] = State{}
	}
	records, err := dal.ListResourcePasswords(db.MasterDB().WithContext(ctx), string(resourceType), resourceIDs)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	for _, record := range records {
		result[record.ResourceID] = *stateOf(record)
	}
	return result, nil
}

func SetTx(tx *gorm.DB, resourceType ResourceType, resourceID, expectedVersion int64, passwordHash string) (*State, error) {
	if tx == nil || !validResource(resourceType, resourceID) || expectedVersion < 0 || (passwordHash != "" && len(passwordHash) > 100) {
		return nil, apperrors.ErrInvalid
	}
	record, err := dal.FindResourcePassword(tx, string(resourceType), resourceID, true)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	now := time.Now()
	if record == nil {
		if expectedVersion != 0 {
			return nil, apperrors.ErrConflict
		}
		record = &model.ResourcePassword{ResourceType: string(resourceType), ResourceID: resourceID, PasswordHash: passwordHash, Version: 1, UpdateTime: now}
		if err := dal.InsertResourcePassword(tx, record); err != nil {
			return nil, apperrors.ErrDependency
		}
		return stateOf(record), nil
	}
	if record.Version != expectedVersion {
		return nil, apperrors.ErrConflict
	}
	updated, err := dal.UpdateResourcePassword(tx, string(resourceType), resourceID, expectedVersion, passwordHash, now)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if !updated {
		return nil, apperrors.ErrConflict
	}
	record.PasswordHash = passwordHash
	record.Version++
	return stateOf(record), nil
}

func (service *Service) Authorize(ctx context.Context, resourceType ResourceType, resourceID int64, owner bool, token string) (*State, error) {
	if !validResource(resourceType, resourceID) || db.MasterDB() == nil {
		return nil, apperrors.ErrDependency
	}
	record, err := dal.FindResourcePassword(db.MasterDB().WithContext(ctx), string(resourceType), resourceID, false)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if err := service.authorizeRecord(record, owner, token); err != nil {
		return nil, err
	}
	return stateOf(record), nil
}

func (service *Service) Unlock(ctx context.Context, resourceType ResourceType, resourceID int64, password, clientKey string) (*State, Grant, error) {
	if !validResource(resourceType, resourceID) || db.MasterDB() == nil {
		return nil, Grant{}, apperrors.ErrDependency
	}
	record, err := dal.FindResourcePassword(db.MasterDB().WithContext(ctx), string(resourceType), resourceID, false)
	if err != nil {
		return nil, Grant{}, apperrors.ErrDependency
	}
	state := stateOf(record)
	if !state.PasswordProtected {
		return state, Grant{}, nil
	}
	key := string(resourceType) + ":" + strconv.FormatInt(resourceID, 10) + ":" + clientKey
	release, err := service.attempts.acquire(key)
	if err != nil {
		return nil, Grant{}, err
	}
	defer release()
	if !utf8.ValidString(password) || len(password) < 1 || len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		return nil, Grant{}, ErrPasswordRequired
	}
	service.attempts.success(key)
	grant, err := service.issueGrant(resourceType, resourceID, record.Version)
	if err != nil {
		return nil, Grant{}, apperrors.ErrDependency
	}
	return state, grant, nil
}

func (service *Service) authorizeRecord(record *model.ResourcePassword, owner bool, token string) error {
	if record == nil || record.PasswordHash == "" || owner {
		return nil
	}
	if !service.validGrant(token, ResourceType(record.ResourceType), record.ResourceID, record.Version) {
		return ErrPasswordRequired
	}
	return nil
}

func stateOf(record *model.ResourcePassword) *State {
	if record == nil {
		return &State{}
	}
	return &State{PasswordProtected: record.PasswordHash != "", Version: record.Version}
}

func validResource(resourceType ResourceType, resourceID int64) bool {
	return resourceID > 0 && validType(resourceType)
}

func validType(resourceType ResourceType) bool {
	return resourceType == ResourceWebProject || resourceType == ResourceManual
}

func (service *Service) issueGrant(resourceType ResourceType, resourceID, version int64) (Grant, error) {
	if !validResource(resourceType, resourceID) || version <= 0 {
		return Grant{}, apperrors.ErrInvalid
	}
	expires := service.now().Add(GrantTTL)
	payload := make([]byte, 34)
	payload[0] = grantFormatVersion
	payload[1] = resourceTypeByte(resourceType)
	binary.BigEndian.PutUint64(payload[2:10], uint64(resourceID))
	binary.BigEndian.PutUint64(payload[10:18], uint64(version))
	binary.BigEndian.PutUint64(payload[18:26], uint64(expires.Unix()))
	if _, err := io.ReadFull(service.random, payload[26:34]); err != nil {
		return Grant{}, err
	}
	signature := service.sign(payload)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature)
	return Grant{Token: token, ExpiresAt: expires}, nil
}

func (service *Service) validGrant(token string, resourceType ResourceType, resourceID, version int64) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) != 34 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size || subtle.ConstantTimeCompare(signature, service.sign(payload)) != 1 {
		return false
	}
	expires := int64(binary.BigEndian.Uint64(payload[18:26]))
	return payload[0] == grantFormatVersion && payload[1] == resourceTypeByte(resourceType) &&
		int64(binary.BigEndian.Uint64(payload[2:10])) == resourceID &&
		int64(binary.BigEndian.Uint64(payload[10:18])) == version &&
		expires > service.now().Unix()
}

func (service *Service) sign(payload []byte) []byte {
	digest := hmac.New(sha256.New, service.secret)
	_, _ = digest.Write(payload)
	return digest.Sum(nil)
}

func resourceTypeByte(resourceType ResourceType) byte {
	if resourceType == ResourceWebProject {
		return 1
	}
	if resourceType == ResourceManual {
		return 2
	}
	return 0
}

func CookieName(resourceType ResourceType, resourceID int64) string {
	label := "invalid"
	if resourceType == ResourceWebProject {
		label = "web"
	} else if resourceType == ResourceManual {
		label = "manual"
	}
	return "__Host-cq_read_" + label + "_" + strconv.FormatInt(resourceID, 10)
}

func GrantToken(request *http.Request, resourceType ResourceType, resourceID int64) string {
	if request == nil {
		return ""
	}
	cookie, err := request.Cookie(CookieName(resourceType, resourceID))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func GrantCookie(resourceType ResourceType, resourceID int64, grant Grant) *http.Cookie {
	return &http.Cookie{Name: CookieName(resourceType, resourceID), Value: grant.Token, Path: "/", MaxAge: int(GrantTTL / time.Second), Expires: grant.ExpiresAt, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func ExpiredGrantCookie(resourceType ResourceType, resourceID int64) *http.Cookie {
	return &http.Cookie{Name: CookieName(resourceType, resourceID), Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

type attemptLimiter struct {
	sync.Mutex
	entries     map[string]*attemptEntry
	semaphore   chan struct{}
	maxKeys     int
	maxAttempts int
	window      time.Duration
	now         func() time.Time
}

type attemptEntry struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

func newAttemptLimiter(maxKeys, maxConcurrent, maxAttempts int, window time.Duration, now func() time.Time) *attemptLimiter {
	return &attemptLimiter{entries: make(map[string]*attemptEntry), semaphore: make(chan struct{}, maxConcurrent), maxKeys: maxKeys, maxAttempts: maxAttempts, window: window, now: now}
}

func (limiter *attemptLimiter) acquire(key string) (func(), error) {
	now := limiter.now()
	limiter.Lock()
	for candidate, entry := range limiter.entries {
		if now.Sub(entry.lastSeen) >= limiter.window {
			delete(limiter.entries, candidate)
		}
	}
	entry := limiter.entries[key]
	if entry == nil {
		if len(limiter.entries) >= limiter.maxKeys {
			limiter.Unlock()
			return nil, apperrors.ErrRateLimited
		}
		entry = &attemptEntry{windowStart: now}
		limiter.entries[key] = entry
	} else if now.Sub(entry.windowStart) >= limiter.window {
		entry.count = 0
		entry.windowStart = now
	}
	entry.lastSeen = now
	if entry.count >= limiter.maxAttempts {
		limiter.Unlock()
		return nil, apperrors.ErrRateLimited
	}
	entry.count++
	limiter.Unlock()
	select {
	case limiter.semaphore <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-limiter.semaphore }) }, nil
	default:
		return nil, apperrors.ErrRateLimited
	}
}

func (limiter *attemptLimiter) success(key string) {
	limiter.Lock()
	delete(limiter.entries, key)
	limiter.Unlock()
}

func (limiter *attemptLimiter) size() int {
	limiter.Lock()
	defer limiter.Unlock()
	return len(limiter.entries)
}
