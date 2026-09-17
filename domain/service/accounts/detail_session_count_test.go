package accounts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

type detailSnapshotTestKey struct{}

// TestUserDetailSessionCountSharesAccountVersionSnapshot pauses a real account
// read, commits a new authentication version and session on another connection,
// then verifies that the paused detail and a fresh detail each retain one count.
func TestUserDetailSessionCountSharesAccountVersionSnapshot(t *testing.T) {
	database := sessionTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	// Detail also reads invitation state after its account/session summary.
	if err := database.Table(dal.SiteRuntimeStateTable).AutoMigrate(&model.SiteRuntimeState{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("DELETE FROM " + dal.SiteRuntimeStateTable).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Table(dal.SiteRuntimeStateTable).Create(&model.SiteRuntimeState{ID: 1, Revision: 1, RegistrationEpoch: 1, ConfigGeneration: 1, UpdateTime: now}).Error; err != nil {
		t.Fatal(err)
	}
	sessionTestInsert(t, database, 9600, 8101, model.SessionUser, now, now.Add(time.Hour))
	userRead, resume := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), detailSnapshotTestKey{}, true), 15*time.Second)
	defer cancel()
	var once sync.Once
	const callback = "test:pause_detail_account_read"
	if err := database.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		user, ok := query.Statement.Dest.(*model.UserAccount)
		if query.Error != nil || !ok || query.Statement.Table != dal.AccountTable || user.ID != 8101 || user.AuthVersion != 1 || query.Statement.Context.Value(detailSnapshotTestKey{}) != true {
			return
		}
		once.Do(func() {
			close(userRead)
			select {
			case <-resume:
			case <-ctx.Done():
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Callback().Query().Remove(callback) })
	type detailResult struct {
		detail map[string]interface{}
		err    error
	}
	result := make(chan detailResult, 1)
	go func() {
		detail, err := UserDetail(ctx, 8101)
		result <- detailResult{detail, err}
	}()
	select {
	case <-userRead:
	case <-ctx.Done():
		t.Fatal("detail did not reach the account read:", ctx.Err())
	}
	writer := database.Session(&gorm.Session{NewDB: true})
	if err := writer.Table(dal.AccountTable).Where("id = ?", 8101).Updates(map[string]interface{}{"auth_version": 2, "revision": 2}).Error; err != nil {
		t.Fatal(err)
	}
	newToken, err := IssueVerifiedSession(context.Background(), 8101, 2)
	if err != nil {
		t.Fatal("could not issue the next-version session while the detail was paused:", err)
	}
	close(resume)
	var response detailResult
	select {
	case response = <-result:
	case <-ctx.Done():
		t.Fatal("detail did not finish:", ctx.Err())
	}
	if response.err != nil {
		t.Fatal(response.err)
	}
	user := response.detail["user"].(*model.UserAccount)
	if user.AuthVersion != 1 || response.detail["active_session_count"] != int64(1) || response.detail["restricted_session_count"] != int64(0) {
		t.Fatalf("paused detail must retain version 1 and its one effective session: version=%d active=%v restricted=%v", user.AuthVersion, response.detail["active_session_count"], response.detail["restricted_session_count"])
	}
	fresh, err := UserDetail(context.Background(), 8101)
	if err != nil {
		t.Fatal(err)
	}
	if fresh["user"].(*model.UserAccount).AuthVersion != 2 || fresh["active_session_count"] != int64(1) {
		t.Fatal("fresh detail did not count the new version's one effective session")
	}
	if _, _, err := CheckSession(context.Background(), sessionTestToken(9600), false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("the previous authentication version remained usable:", err)
	}
	if current, _, err := CheckSession(context.Background(), newToken, false); err != nil || current.AuthVersion != 2 {
		t.Fatal("the new authentication version was not usable:", err)
	}
}
