package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
)

// TestVerifyPassword 验证成功校验不占失败预算，以及并发失败、过期窗口和计数容量上限的行为。
func TestVerifyPassword(t *testing.T) {
	password := "SyntheticPasswordForLimiter123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"successful traffic", "concurrent failures", "expired failures", "capacity"} {
		// 重置网站登录的失败计数，分别核对成功、并发失败、过期恢复及容量耗尽场景。
		t.Run(scenario, func(t *testing.T) {
			passwordFailureLock.Lock()
			passwordFailures = map[[32]byte]passwordFailure{}
			passwordFailureLock.Unlock()
			ctx := context.WithValue(context.Background(), PasswordSourceIPKey, "198.51.100.101")
			switch scenario {
			case "successful traffic":
				for i := 0; i < 80; i++ {
					if err := VerifyPassword(ctx, 101, "name", string(hash), password); err != nil {
						t.Fatalf("success %d consumed failure budget: %v", i, err)
					}
				}
				if len(passwordFailures) != 0 {
					t.Fatal("successful passwords allocated failure counters")
				}
			case "concurrent failures":
				var group sync.WaitGroup
				for i := 0; i < 80; i++ {
					group.Add(1)
					go func() {
						defer group.Done()
						err := VerifyPassword(ctx, 101, "name", string(hash), "wrong")
						if !errors.Is(err, ErrCredentials) && !errors.Is(err, apperrors.ErrRateLimited) {
							t.Errorf("unexpected error: %v", err)
						}
					}()
				}
				group.Wait()
				if err := VerifyPassword(ctx, 101, "alias", string(hash), password); !errors.Is(err, apperrors.ErrRateLimited) {
					t.Fatalf("concurrent failures did not block next request: %v", err)
				}
			case "expired failures":
				key := sha256.Sum256([]byte("account:101"))
				passwordFailures[key] = passwordFailure{Count: 10, Expires: time.Now().Add(-time.Second)}
				if err := VerifyPassword(ctx, 101, "name", string(hash), password); err != nil {
					t.Fatalf("expired budget blocked valid password: %v", err)
				}
				if err := VerifyPassword(ctx, 101, "name", string(hash), "wrong"); !errors.Is(err, ErrCredentials) {
					t.Fatal(err)
				}
				if entry := passwordFailures[key]; entry.Count != 1 || time.Until(entry.Expires) > time.Minute || time.Until(entry.Expires) < 59*time.Second {
					t.Fatal("expired counter did not restart a one-minute failure window")
				}
			case "capacity":
				for i := 0; i < maxPasswordFailureKeys+100; i++ {
					ctx := context.WithValue(context.Background(), PasswordSourceIPKey, fmt.Sprintf("10.0.%d.%d", i/250, i%250+1))
					err := VerifyPassword(ctx, 0, fmt.Sprintf("missing-%d", i), "", "wrong")
					if !errors.Is(err, ErrCredentials) && !errors.Is(err, apperrors.ErrRateLimited) {
						t.Fatal(err)
					}
				}
				if len(passwordFailures) > maxPasswordFailureKeys {
					t.Fatal("failure memory exceeded configured bound")
				}
				if err := VerifyPassword(ctx, 101, "name", string(hash), password); !errors.Is(err, apperrors.ErrRateLimited) {
					t.Fatalf("full table did not fail closed: %v", err)
				}
				for key, entry := range passwordFailures {
					entry.Expires = time.Now().Add(-time.Second)
					passwordFailures[key] = entry
				}
				if err := VerifyPassword(ctx, 101, "name", string(hash), password); err != nil {
					t.Fatalf("expired full table failed to recover: %v", err)
				}
			}
		})
	}
}

// TestRandomPasswordAndPolicyMessage 核对随机密码满足配置下限和 bcrypt 字节上限，错误文案显示实际下限。
func TestRandomPasswordAndPolicyMessage(t *testing.T) {
	for _, minimum := range []int{15, 21, 32, 64, 72} {
		password, err := randomPassword(minimum)
		if err != nil {
			t.Fatal(err)
		}
		if len(password) > 72 || len(password) < minimum {
			t.Fatalf("minimum %d generated %d bytes", minimum, len(password))
		}
		if err := ValidatePassword(password, password, minimum); err != nil {
			t.Fatal(err)
		}
		if err := ValidatePassword("short", "short", minimum); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("至少%d个字符", minimum)) {
			t.Fatalf("policy error did not state actual minimum %d: %v", minimum, err)
		}
	}
	if _, err := randomPassword(73); !errors.Is(err, apperrors.ErrInvalid) {
		t.Fatalf("unsupported bcrypt minimum accepted: %v", err)
	}
}
