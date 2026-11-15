package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const webDAVPasswordCacheTTL = 5 * time.Minute
const maxWebDAVPasswordCacheEntries = 1024

var webDAVPasswordCacheSecret [32]byte
var webDAVPasswordCacheLock sync.Mutex
var webDAVPasswordSuccesses = map[[32]byte]time.Time{}

func init() {
	if _, err := rand.Read(webDAVPasswordCacheSecret[:]); err != nil {
		panic(fmt.Errorf("initialize WebDAV password cache key: %w", err))
	}
}

// verifyPasswordHash only reuses successful DAV password work. Callers still
// enforce failure budgets before this function and current account state and
// permissions afterwards. Binding the current hash makes password changes miss
// immediately; only the keyed digest and a fixed expiry stay in process memory.
func verifyPasswordHash(ctx context.Context, accountKey, hash, password string) bool {
	if ctx.Value(PasswordBudgetScopeKey) != PasswordBudgetWebDAV {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	mac := hmac.New(sha256.New, webDAVPasswordCacheSecret[:])
	mac.Write([]byte(accountKey + "\x00" + hash + "\x00" + password))
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	now := time.Now()
	webDAVPasswordCacheLock.Lock()
	expires, found := webDAVPasswordSuccesses[key]
	if found && expires.After(now) {
		webDAVPasswordCacheLock.Unlock()
		return true
	}
	delete(webDAVPasswordSuccesses, key)
	webDAVPasswordCacheLock.Unlock()

	// Never hold the shared cache mutex during an expensive password check.
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return false
	}
	now = time.Now()
	webDAVPasswordCacheLock.Lock()
	if len(webDAVPasswordSuccesses) >= maxWebDAVPasswordCacheEntries {
		for key, expires := range webDAVPasswordSuccesses {
			if !expires.After(now) {
				delete(webDAVPasswordSuccesses, key)
			}
		}
	}
	if len(webDAVPasswordSuccesses) < maxWebDAVPasswordCacheEntries {
		webDAVPasswordSuccesses[key] = now.Add(webDAVPasswordCacheTTL)
	}
	webDAVPasswordCacheLock.Unlock()
	return true
}
