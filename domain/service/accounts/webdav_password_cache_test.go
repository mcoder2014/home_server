package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
)

// resetWebDAVPasswordState isolates the success cache and all failure budgets;
// restoring them keeps these tests independent of the existing account suite.
func resetWebDAVPasswordState(t *testing.T) {
	t.Helper()
	webDAVPasswordCacheLock.Lock()
	previousCache := webDAVPasswordSuccesses
	webDAVPasswordSuccesses = map[[32]byte]time.Time{}
	webDAVPasswordCacheLock.Unlock()
	passwordFailureLock.Lock()
	previousWebDAV, previousLogin, previousAuthenticated := webDAVPasswordFailures, passwordFailures, authenticatedPasswordFailures
	webDAVPasswordFailures = map[[32]byte]passwordFailure{}
	passwordFailures = map[[32]byte]passwordFailure{}
	authenticatedPasswordFailures = map[[32]byte]passwordFailure{}
	passwordFailureLock.Unlock()
	t.Cleanup(func() {
		webDAVPasswordCacheLock.Lock()
		webDAVPasswordSuccesses = previousCache
		webDAVPasswordCacheLock.Unlock()
		passwordFailureLock.Lock()
		webDAVPasswordFailures, passwordFailures, authenticatedPasswordFailures = previousWebDAV, previousLogin, previousAuthenticated
		passwordFailureLock.Unlock()
	})
}

func TestWebDAVPasswordCacheBindsCurrentCredentials(t *testing.T) {
	resetWebDAVPasswordState(t)
	password := "SyntheticDAVCachePassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), PasswordBudgetScopeKey, PasswordBudgetWebDAV)
	for _, scope := range []string{"", PasswordBudgetAuthenticated} {
		other := context.WithValue(ctx, PasswordBudgetScopeKey, scope)
		if err := VerifyPassword(other, 9201, "cache-user", string(hash), password); err != nil {
			t.Fatal(err)
		}
		if len(webDAVPasswordSuccesses) != 0 {
			t.Fatal("non-WebDAV password confirmation populated the DAV cache")
		}
	}
	if err := VerifyPassword(ctx, 9201, "cache-user", string(hash), "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal("wrong password was accepted")
	}
	if len(webDAVPasswordSuccesses) != 0 {
		t.Fatal("failed credentials populated the success cache")
	}
	for _, login := range []string{"cache-user", "alias@example.com", "cache-user"} {
		if err := VerifyPassword(ctx, 9201, login, string(hash), password); err != nil {
			t.Fatal(err)
		}
	}
	if len(webDAVPasswordSuccesses) != 1 {
		t.Fatalf("aliases of the same current credentials should share one entry, got %d", len(webDAVPasswordSuccesses))
	}
	for _, scope := range []string{"", PasswordBudgetAuthenticated} {
		other := context.WithValue(ctx, PasswordBudgetScopeKey, scope)
		if err := VerifyPassword(other, 9201, "cache-user", string(hash), password); err != nil {
			t.Fatal(err)
		}
		if err := VerifyPassword(other, 9201, "cache-user", string(hash), "wrong"); !errors.Is(err, ErrCredentials) {
			t.Fatal("warm DAV credentials bypassed another password-confirmation scope")
		}
	}
	if err := VerifyPassword(ctx, 9202, "other-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	if len(webDAVPasswordSuccesses) != 2 {
		t.Fatal("different accounts shared a success-cache entry")
	}
	nextPassword := "SyntheticChangedDAVPassword456"
	nextHash, err := bcrypt.GenerateFromPassword([]byte(nextPassword), 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(ctx, 9201, "cache-user", string(nextHash), password); !errors.Is(err, ErrCredentials) {
		t.Fatal("old cached password survived a changed account hash")
	}
	if err := VerifyPassword(ctx, 9201, "cache-user", string(hash), nextPassword); !errors.Is(err, ErrCredentials) {
		t.Fatal("a different password reused a cached successful check")
	}
	if err := VerifyPassword(ctx, 9201, "cache-user", string(nextHash), nextPassword); err != nil {
		t.Fatal(err)
	}
}

func TestWebDAVPasswordCacheHasFixedExpiry(t *testing.T) {
	resetWebDAVPasswordState(t)
	password := "SyntheticDAVExpiryPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), PasswordBudgetScopeKey, PasswordBudgetWebDAV)
	if err := VerifyPassword(ctx, 9203, "expiry-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	var expires time.Time
	for k, e := range webDAVPasswordSuccesses {
		key, expires = k, e
	}
	if expires.IsZero() || time.Until(expires) > webDAVPasswordCacheTTL || time.Until(expires) < webDAVPasswordCacheTTL-time.Second {
		t.Fatal("successful check did not establish the fixed cache lifetime")
	}
	if err := VerifyPassword(ctx, 9203, "expiry-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	if !webDAVPasswordSuccesses[key].Equal(expires) {
		t.Fatal("a cache hit extended the password verification lifetime")
	}
	webDAVPasswordSuccesses[key] = time.Now().Add(-time.Second)
	if err := VerifyPassword(ctx, 9203, "expiry-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	if !webDAVPasswordSuccesses[key].After(expires) {
		t.Fatal("expired credentials were not verified and refreshed")
	}
}

func TestWebDAVPasswordCacheCapacityPreservesAuthentication(t *testing.T) {
	resetWebDAVPasswordState(t)
	password := "SyntheticDAVCapacityPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxWebDAVPasswordCacheEntries; i++ {
		key := sha256.Sum256([]byte(fmt.Sprintf("synthetic-entry-%d", i)))
		webDAVPasswordSuccesses[key] = time.Now().Add(time.Minute)
	}
	ctx := context.WithValue(context.Background(), PasswordBudgetScopeKey, PasswordBudgetWebDAV)
	if err := VerifyPassword(ctx, 9204, "capacity-user", string(hash), password); err != nil {
		t.Fatalf("full success cache rejected otherwise valid credentials: %v", err)
	}
	if len(webDAVPasswordSuccesses) != maxWebDAVPasswordCacheEntries {
		t.Fatal("success cache exceeded its memory bound")
	}
	for key := range webDAVPasswordSuccesses {
		webDAVPasswordSuccesses[key] = time.Now().Add(-time.Second)
	}
	if err := VerifyPassword(ctx, 9204, "capacity-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	if len(webDAVPasswordSuccesses) != 1 {
		t.Fatal("expired full cache did not reclaim entries")
	}
}

func TestWebDAVWarmPasswordCachePreservesFailureBudgets(t *testing.T) {
	resetWebDAVPasswordState(t)
	password := "SyntheticDAVBudgetPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), PasswordSourceIPKey, "198.51.100.205")
	dav := context.WithValue(ctx, PasswordBudgetScopeKey, PasswordBudgetWebDAV)
	if err := VerifyPassword(dav, 9205, "budget-user", string(hash), password); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := VerifyPassword(ctx, 9205, "budget-user", string(hash), "wrong"); !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	if err := VerifyPassword(dav, 9205, "budget-user", string(hash), password); err != nil {
		t.Fatalf("website failures blocked cached DAV traffic: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := VerifyPassword(dav, 9205, "budget-user", string(hash), "wrong"); !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	if err := VerifyPassword(dav, 9205, "alias@example.com", string(hash), password); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("warm password cache bypassed exhausted DAV budget: %v", err)
	}
}

func TestWebDAVPasswordCacheConcurrentChecks(t *testing.T) {
	resetWebDAVPasswordState(t)
	password := "SyntheticDAVConcurrentPassword123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), PasswordBudgetScopeKey, PasswordBudgetWebDAV)
	var group sync.WaitGroup
	for i := 0; i < 24; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := VerifyPassword(ctx, 9206, "concurrent-user", string(hash), password); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if len(webDAVPasswordSuccesses) != 1 {
		t.Fatal("concurrent checks did not converge on one bounded success entry")
	}
}
