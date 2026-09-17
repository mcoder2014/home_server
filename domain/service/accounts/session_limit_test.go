package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

func setSessionLimit(t *testing.T, limit int) {
	t.Helper()
	snapshot := config.Runtime()
	snapshot.AccountPolicy.MaxActiveSessions = limit
	if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
}

// sessionLimitDatabase restores the real registry default after the other
// session tests' explicitly larger fixture limit. Production defaults stay intact.
func sessionLimitDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database := sessionTestDatabase(t)
	defaults, err := config.BuildRuntimeSnapshot(config.Global(), 0, nil, config.DefaultRuntimeValues(config.Global()))
	if err != nil || defaults.AccountPolicy.MaxActiveSessions != 5 {
		t.Fatalf("expected the production default of five sessions: policy=%v error=%v", defaults.AccountPolicy, err)
	}
	setSessionLimit(t, defaults.AccountPolicy.MaxActiveSessions)
	return database
}

func TestSessionLimitDefaultRejectsSixthLoginAndKeepsExistingSessions(t *testing.T) {
	database := sessionLimitDatabase(t)
	ctx := context.Background()
	tokens := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		_, token, _, err := Login(ctx, "session8101", reviewPassword)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	_, token, session, err := Login(ctx, "session8101", reviewPassword)
	if !errors.Is(err, apperrors.ErrRateLimited) || token != "" || session != nil || !strings.Contains(err.Error(), "网站上限") {
		t.Fatalf("sixth login must explain the session limit without issuing a token: token_issued=%t session_issued=%t error=%v", token != "", session != nil, err)
	}
	if token, err := IssueVerifiedSession(ctx, 8101, 1); token != "" || !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("verified legacy issuance bypassed the limit: token_issued=%t error=%v", token != "", err)
	}
	var rows int64
	if err := database.Table(dal.TableUserToken).Where("user_id = ?", 8101).Count(&rows).Error; err != nil || rows != 5 {
		t.Fatalf("rejected login changed session rows: count=%d error=%v", rows, err)
	}
	for _, token := range tokens {
		if _, _, err := CheckSession(ctx, token, false); err != nil {
			t.Fatal("limit rejection invalidated an existing login:", err)
		}
	}
	page, err := ListSessions(ctx, tokens[0], SessionFilter{})
	if err != nil || page.TotalCount != 5 {
		t.Fatalf("session page omitted the applied limit: page=%v error=%v", page, err)
	}
	raw, err := json.Marshal(page)
	var body map[string]interface{}
	if err != nil || json.Unmarshal(raw, &body) != nil || body["max_active_sessions"] != float64(5) {
		t.Fatalf("session page must expose the applied limit: %s", raw)
	}
}

func TestSessionLimitConcurrentMixedIssuanceNeverExceedsCap(t *testing.T) {
	for _, parallelism := range []int{12, 32} {
		t.Run(strconv.Itoa(parallelism), func(t *testing.T) {
			database := sessionLimitDatabase(t)
			start := make(chan struct{})
			results := make(chan error, parallelism)
			for i := 0; i < parallelism; i++ {
				go func(modern bool) {
					<-start
					var err error
					if modern {
						_, _, _, err = Login(context.Background(), "session8101", reviewPassword)
					} else {
						_, err = IssueVerifiedSession(context.Background(), 8101, 1)
					}
					results <- err
				}(i%2 == 0)
			}
			close(start)
			issued, limited := 0, 0
			for i := 0; i < parallelism; i++ {
				switch err := <-results; {
				case err == nil:
					issued++
				case errors.Is(err, apperrors.ErrRateLimited):
					limited++
				default:
					t.Errorf("concurrent issuance returned an unexpected error: %v", err)
				}
			}
			var rows int64
			if err := database.Table(dal.TableUserToken).Where("user_id = ?", 8101).Count(&rows).Error; err != nil || issued != 5 || limited != parallelism-5 || rows != 5 {
				t.Fatalf("concurrent login exceeded the cap: issued=%d limited=%d rows=%d error=%v", issued, limited, rows, err)
			}
		})
	}
}

func TestSessionLimitUsesCurrentReadAfterAnEarlierTransactionSnapshot(t *testing.T) {
	database := sessionLimitDatabase(t)
	setSessionLimit(t, 1)
	ready, resume := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- database.Transaction(func(tx *gorm.DB) error {
			var rows int64
			if err := tx.Table(dal.TableUserToken).Where("user_id = ?", 8101).Count(&rows).Error; err != nil {
				return err
			}
			close(ready)
			<-resume
			user, err := dal.QueryAccount(tx, 8101, true)
			if err != nil {
				return err
			}
			_, _, err = issueSessionTx(tx, user, SessionMetadata{})
			return err
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	}()
	defer close(resume)
	select {
	case <-ready:
	case err := <-result:
		t.Fatal("snapshot setup failed:", err)
	case <-time.After(5 * time.Second):
		t.Fatal("transaction did not establish its initial snapshot")
	}
	if _, err := IssueVerifiedSession(context.Background(), 8101, 1); err != nil {
		t.Fatal(err)
	}
	resume <- struct{}{}
	select {
	case err := <-result:
		if !errors.Is(err, apperrors.ErrRateLimited) {
			t.Fatalf("an earlier snapshot hid the newly committed session: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session check did not finish")
	}
}

func TestSessionLimitInactiveRowsDoNotConsumeSlotsAndRestrictedRowsDo(t *testing.T) {
	database := sessionLimitDatabase(t)
	setSessionLimit(t, 1)
	now := time.Now().Truncate(time.Second)
	sessionTestInsert(t, database, 9600, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(-time.Second))
	sessionTestInsert(t, database, 9601, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9602, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9603, 8101, model.SessionUser, now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9604, 8101, "unknown_purpose", now, now.Add(time.Hour))
	sessionTestInsert(t, database, 9605, 8101, model.SessionPasswordChange, now, now.Add(time.Hour))
	for id, fields := range map[int64]map[string]interface{}{9601: {"is_expired": 1}, 9602: {"auth_version": 0}, 9603: {"token_digest": nil}} {
		if err := database.Table(dal.TableUserToken).Where("id = ?", id).Updates(fields).Error; err != nil {
			t.Fatal(err)
		}
	}
	past := now.Add(-time.Second)
	if err := database.Table(dal.AccountTable).Where("id = ?", 8101).Update("password_expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := IssueVerifiedSession(context.Background(), 8101, 1); err != nil {
		t.Fatal("expired/revoked/stale/unsupported rows consumed a slot:", err)
	}
	future := now.Add(time.Hour)
	if err := database.Table(dal.AccountTable).Where("id = ?", 8102).Updates(map[string]interface{}{"must_change_password": true, "password_expires_at": future}).Error; err != nil {
		t.Fatal(err)
	}
	restricted, err := IssueVerifiedSession(context.Background(), 8102, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IssueVerifiedSession(context.Background(), 8102, 1); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("valid initial-password sessions did not consume a slot: %v", err)
	}
	if _, _, err := CheckSession(context.Background(), restricted, true); err != nil {
		t.Fatal("limited login was revoked by a rejected new login:", err)
	}
}

func TestSessionLimitLoweringKeepsOldLoginsAndRevocationReleasesSlots(t *testing.T) {
	sessionLimitDatabase(t)
	ctx := context.Background()
	tokens := []string{}
	for i := 0; i < 5; i++ {
		token, err := IssueVerifiedSession(ctx, 8101, 1)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	setSessionLimit(t, 2)
	for _, token := range tokens {
		if _, _, err := CheckSession(ctx, token, false); err != nil {
			t.Fatal("lowering the cap invalidated an existing login:", err)
		}
	}
	if _, err := IssueVerifiedSession(ctx, 8101, 1); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("lowered cap was not applied to the next login: %v", err)
	}
	if result, err := RevokeOtherSessions(ctx, tokens[0]); err != nil || result.RevokedCount != 4 {
		t.Fatalf("could not release old session slots: result=%v error=%v", result, err)
	}
	if _, err := IssueVerifiedSession(ctx, 8101, 1); err != nil {
		t.Fatal("revocation did not release a session slot:", err)
	}
	if _, err := IssueVerifiedSession(ctx, 8101, 1); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("second newly issued login exceeded the lowered cap: %v", err)
	}
	setSessionLimit(t, 3)
	if _, err := IssueVerifiedSession(ctx, 8101, 1); err != nil {
		t.Fatal("raised cap was not applied to the next login:", err)
	}
}

func TestSessionLimitInvalidRuntimeFailsClosed(t *testing.T) {
	for _, limit := range []int{0, -1, 101} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			database := sessionLimitDatabase(t)
			setSessionLimit(t, limit)
			if token, err := IssueVerifiedSession(context.Background(), 8101, 1); token != "" || !errors.Is(err, apperrors.ErrDependency) {
				t.Fatalf("invalid runtime silently allowed an unbounded login: token_issued=%t error=%v", token != "", err)
			}
			var rows int64
			if err := database.Table(dal.TableUserToken).Count(&rows).Error; err != nil || rows != 0 {
				t.Fatalf("invalid runtime issued a session: rows=%d error=%v", rows, err)
			}
		})
	}
}
