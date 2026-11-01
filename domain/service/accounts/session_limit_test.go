package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
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

func TestSessionLimitDefaultReplacesOldestForEveryLoginEntry(t *testing.T) {
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
	if err != nil || token == "" || session == nil {
		t.Fatalf("sixth login must replace the oldest session: token_issued=%t session_issued=%t error=%v", token != "", session != nil, err)
	}
	if _, _, err := CheckSession(ctx, tokens[0], false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("sixth login did not revoke the oldest session:", err)
	}
	for _, existing := range tokens[1:] {
		if _, _, err := CheckSession(ctx, existing, false); err != nil {
			t.Fatal("replacement revoked a newer session:", err)
		}
	}
	legacy, err := IssueVerifiedSession(ctx, 8101, 1)
	if err != nil || legacy == "" {
		t.Fatal("verified legacy login did not use the same replacement policy:", err)
	}
	if _, _, err := CheckSession(ctx, tokens[1], false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("legacy login did not revoke the next oldest session:", err)
	}
	if _, _, err := CheckSession(ctx, token, false); err != nil {
		t.Fatal("legacy login revoked the newer website session:", err)
	}
	var active int64
	if err := database.Table(dal.TableUserToken).Where("user_id = ? AND is_expired = 0", 8101).Count(&active).Error; err != nil || active != 5 {
		t.Fatalf("default replacement exceeded the cap: active=%d error=%v", active, err)
	}
	page, err := ListSessions(ctx, legacy, SessionFilter{})
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
			issued := 0
			for i := 0; i < parallelism; i++ {
				switch err := <-results; {
				case err == nil:
					issued++
				default:
					t.Errorf("concurrent issuance returned an unexpected error: %v", err)
				}
			}
			var active int64
			if err := database.Table(dal.TableUserToken).Where("user_id = ? AND is_expired = 0", 8101).Count(&active).Error; err != nil || issued != parallelism || active != 5 {
				t.Fatalf("concurrent replacement violated the policy: issued=%d active=%d error=%v", issued, active, err)
			}
		})
	}
}

func TestSessionLimitUsesCurrentReadAfterAnEarlierTransactionSnapshot(t *testing.T) {
	database := sessionLimitDatabase(t)
	setSessionLimit(t, 1)
	ready, resume := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var replacement string
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
			replacement, _, err = issueSessionTx(tx, user, SessionMetadata{})
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
	first, err := IssueVerifiedSession(context.Background(), 8101, 1)
	if err != nil {
		t.Fatal(err)
	}
	resume <- struct{}{}
	select {
	case err := <-result:
		if err != nil || replacement == "" {
			t.Fatalf("replacement after an earlier snapshot failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session check did not finish")
	}
	if _, _, err := CheckSession(context.Background(), first, false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("an earlier snapshot hid the committed session from replacement:", err)
	}
	if _, _, err := CheckSession(context.Background(), replacement, false); err != nil {
		t.Fatal("replacement session is unusable:", err)
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
	replacement, err := IssueVerifiedSession(context.Background(), 8102, 1)
	if err != nil {
		t.Fatal("initial-password session replacement failed:", err)
	}
	if _, _, err := CheckSession(context.Background(), restricted, true); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("valid initial-password session did not consume a replacement slot:", err)
	}
	if _, _, err := CheckSession(context.Background(), replacement, true); err != nil {
		t.Fatal("replacement lost its password-change session:", err)
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
	replacement, err := IssueVerifiedSession(ctx, 8101, 1)
	if err != nil {
		t.Fatal("login after lowering the cap failed:", err)
	}
	page, err := ListSessions(ctx, replacement, SessionFilter{})
	if err != nil || page.TotalCount != 2 {
		t.Fatalf("lowered cap was not applied to replacement: page=%v error=%v", page, err)
	}
	if result, err := RevokeOtherSessions(ctx, replacement); err != nil || result.RevokedCount != 1 {
		t.Fatalf("could not release old session slots: result=%v error=%v", result, err)
	}
	if _, err := IssueVerifiedSession(ctx, 8101, 1); err != nil {
		t.Fatal("revocation did not release a session slot:", err)
	}
	newest, err := IssueVerifiedSession(ctx, 8101, 1)
	if err != nil {
		t.Fatal("subsequent login did not replace the oldest session:", err)
	}
	page, err = ListSessions(ctx, newest, SessionFilter{})
	if err != nil || page.TotalCount != 2 {
		t.Fatalf("subsequent replacement exceeded the cap: page=%v error=%v", page, err)
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

func TestSessionReplacementUsesOnlyEffectiveRowsInLoginOrder(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(strconv.FormatBool(full), func(t *testing.T) {
			database := sessionLimitDatabase(t)
			setSessionLimit(t, 3)
			now := time.Now().UTC().Truncate(time.Second)
			sessionTestInsert(t, database, 9901, 8101, model.SessionUser, now.Add(-time.Minute), now.Add(time.Hour))
			if full {
				// The smaller ID is newer; tied older timestamps must use ID order.
				sessionTestInsert(t, database, 9902, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(time.Hour))
				sessionTestInsert(t, database, 9903, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(time.Hour))
			}
			sessionTestInsert(t, database, 9910, 8102, model.SessionUser, now.Add(-time.Hour), now.Add(time.Hour))
			sessionTestInsert(t, database, 9911, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(-time.Second))
			sessionTestInsert(t, database, 9912, 8101, "unknown_purpose", now.Add(-time.Hour), now.Add(time.Hour))
			for id, fields := range map[int64]map[string]interface{}{9913: {"auth_version": 0}, 9914: {"token_digest": nil}, 9915: {"is_expired": 1}} {
				sessionTestInsert(t, database, id, 8101, model.SessionUser, now.Add(-time.Hour), now.Add(time.Hour))
				if err := database.Table(dal.TableUserToken).Where("id = ?", id).Updates(fields).Error; err != nil {
					t.Fatal(err)
				}
			}
			_, token, _, err := Login(context.Background(), "session8101", reviewPassword)
			if err != nil || token == "" {
				t.Fatal("default replacement login failed:", err)
			}
			var rows []struct {
				ID        int64
				IsExpired int
			}
			if err := database.Table(dal.TableUserToken).Select("id,is_expired").Where("id BETWEEN ? AND ?", 9901, 9915).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				expected := 0
				if row.ID == 9915 || full && row.ID == 9902 {
					expected = 1
				}
				if row.IsExpired != expected {
					t.Fatalf("replacement touched the wrong history or user: id=%d expired=%d want=%d", row.ID, row.IsExpired, expected)
				}
			}
		})
	}
}
