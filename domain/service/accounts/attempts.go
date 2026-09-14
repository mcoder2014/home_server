package accounts

import (
	"context"
	"crypto/sha256"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
)

// PasswordSourceIPKey is set only after the HTTP boundary resolves the trusted
// proxy chain. Missing sources share a bounded fallback budget.
const PasswordSourceIPKey = "home_server.password_source_ip"

// PasswordBudgetScopeKey is set only by trusted authentication middleware, never
// from client input. Only the fixed DAV and verified-session scopes are distinct.
const PasswordBudgetScopeKey = "home_server.password_budget_scope"
const PasswordBudgetWebDAV = "webdav"
const PasswordBudgetAuthenticated = "authenticated"

const passwordFailureWindow = time.Minute
const maxPasswordFailureKeys = 4096

type passwordFailure struct {
	Count   int
	Expires time.Time
}

var passwordFailureLock sync.Mutex
var passwordFailures = map[[32]byte]passwordFailure{}
var webDAVPasswordFailures = map[[32]byte]passwordFailure{}
var authenticatedPasswordFailures = map[[32]byte]passwordFailure{}

// VerifyPassword keeps anonymous login, DAV Basic and verified-session password
// confirmation in independent account, IP and capacity budgets. Anonymous
// failures cannot block security actions in an existing authenticated session.
// Aliases share an account budget within each scope; successes do not consume or
// reset it. Failed checks update counters under the mutex; already admitted
// requests can finish concurrently.
func VerifyPassword(ctx context.Context, userID int64, loginKey, hash, password string) error {
	accountKey := "login:" + strings.ToLower(strings.TrimSpace(loginKey))
	if userID > 0 {
		accountKey = "account:" + strconv.FormatInt(userID, 10)
	}
	source, _ := ctx.Value(PasswordSourceIPKey).(string)
	if ip := net.ParseIP(source); ip != nil {
		source = ip.String()
	} else {
		source = "unknown"
	}
	keys := [2][32]byte{sha256.Sum256([]byte(accountKey)), sha256.Sum256([]byte("ip:" + source))}
	limits := [2]int{10, 30}
	now := time.Now()
	passwordFailureLock.Lock()
	failures := passwordFailures
	switch ctx.Value(PasswordBudgetScopeKey) {
	case PasswordBudgetWebDAV:
		failures = webDAVPasswordFailures
	case PasswordBudgetAuthenticated:
		failures = authenticatedPasswordFailures
	}
	if len(failures) >= maxPasswordFailureKeys {
		for key, entry := range failures {
			if !entry.Expires.After(now) {
				delete(failures, key)
			}
		}
	}
	for i, key := range keys {
		entry, exists := failures[key]
		if (entry.Expires.After(now) && entry.Count >= limits[i]) || (!exists && len(failures) >= maxPasswordFailureKeys) {
			passwordFailureLock.Unlock()
			return apperrors.ErrRateLimited
		}
	}
	passwordFailureLock.Unlock()
	if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
		return nil
	}
	passwordFailureLock.Lock()
	now = time.Now()
	for i, key := range keys {
		entry, exists := failures[key]
		if !exists && len(failures) >= maxPasswordFailureKeys {
			continue
		}
		if !entry.Expires.After(now) {
			entry = passwordFailure{Expires: now.Add(passwordFailureWindow)}
		}
		if entry.Count < limits[i] {
			entry.Count++
		}
		failures[key] = entry
	}
	passwordFailureLock.Unlock()
	return ErrCredentials
}
