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

const passwordFailureWindow = time.Minute
const maxPasswordFailureKeys = 4096

type passwordFailure struct {
	Count   int
	Expires time.Time
}

var passwordFailureLock sync.Mutex
var passwordFailures = map[[32]byte]passwordFailure{}

// VerifyPassword shares failed-password budgets across login, Basic and password
// confirmation. Existing aliases use the same account ID. Successful requests do
// not consume or reset a failure budget; in-flight checks can finish, but their
// failed results are counted atomically before any later request is admitted.
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
	if len(passwordFailures) >= maxPasswordFailureKeys {
		for key, entry := range passwordFailures {
			if !entry.Expires.After(now) {
				delete(passwordFailures, key)
			}
		}
	}
	for i, key := range keys {
		entry, exists := passwordFailures[key]
		if (entry.Expires.After(now) && entry.Count >= limits[i]) || (!exists && len(passwordFailures) >= maxPasswordFailureKeys) {
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
		entry, exists := passwordFailures[key]
		if !exists && len(passwordFailures) >= maxPasswordFailureKeys {
			continue
		}
		if !entry.Expires.After(now) {
			entry = passwordFailure{Expires: now.Add(passwordFailureWindow)}
		}
		if entry.Count < limits[i] {
			entry.Count++
		}
		passwordFailures[key] = entry
	}
	passwordFailureLock.Unlock()
	return ErrCredentials
}
