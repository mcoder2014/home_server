package accounts

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const reviewPassword = "SyntheticPasswordForReview123"

// reviewAccountDatabase 仅在指定的独立测试库重建账号相关样例，清空失败预算并在结束时恢复全局配置。
func reviewAccountDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	passwordFailureLock.Lock()
	passwordFailures = map[[32]byte]passwordFailure{}
	passwordFailureLock.Unlock()
	dsn := os.Getenv("ACCOUNTS_REVIEW_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNTS_REVIEW_TEST_DSN to the dedicated synthetic database")
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_accounts_review_test" {
		t.Fatal("unsafe review database")
	}
	if err = db.InitDatabase(dsn); err != nil {
		t.Fatal("review database initialization failed")
	}
	database := db.MasterDB()
	database.Logger = logger.Discard
	old := config.Global()
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	t.Cleanup(func() { config.SetGlobalConfig(old); connection, _ := database.DB(); connection.Close() })
	for _, table := range []struct {
		name  string
		value interface{}
	}{
		{dal.AccountTable, &model.UserAccount{}}, {dal.LoginAliasTable, &model.LoginAlias{}},
		{dal.ApplicationTable, &model.Application{}}, {dal.AdminAuditTable, &model.AdminAuditLog{}},
		{dal.SiteRuntimeStateTable, &model.SiteRuntimeState{}}, {dal.TableUserToken, &model.AccountSession{}},
	} {
		if err := database.Table(table.name).AutoMigrate(table.value); err != nil {
			t.Fatal(err)
		}
		if err := database.Exec("DELETE FROM " + table.name).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Table(dal.SiteRuntimeStateTable).Create(&model.SiteRuntimeState{ID: 1, Revision: 1, RegistrationEpoch: 1, ConfigGeneration: 1, UpdateTime: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(reviewPassword), 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{7101, 7102} {
		user := &model.UserAccount{ID: id, Username: "review" + strconv.FormatInt(id, 10), UsernameKey: "review" + strconv.FormatInt(id, 10), DisplayName: "Review", PasswordHash: string(hash), Status: model.AccountActive, Role: model.RoleAdmin, WebDAVPermission: model.WebDAVNone, AuthVersion: 1, Revision: 1, InviteEligibleAt: time.Now(), Source: "test", CreateTime: time.Now(), UpdateTime: time.Now()}
		if err := database.Transaction(func(tx *gorm.DB) error { return dal.InsertAccount(tx, user) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Table(dal.LoginAliasTable).Create(&model.LoginAlias{LoginKey: "alias@example.com", UserID: 7101, Kind: "email"}).Error; err != nil {
		t.Fatal(err)
	}
	return database
}

// TestPasswordFailuresAreSharedAcrossAccountEntryPoints 验证登录、管理员确认和自助改密共享账号失败预算，别名不能绕过限流。
func TestPasswordFailuresAreSharedAcrossAccountEntryPoints(t *testing.T) {
	reviewAccountDatabase(t)
	ctx := context.WithValue(context.Background(), "home_server.password_source_ip", "198.51.100.71")
	for i := 0; i < 10; i++ {
		var err error
		switch i % 3 {
		case 0:
			_, err = Authenticate(ctx, "review7101", "incorrect password")
		case 1:
			_, err = VerifyAdminPassword(ctx, 7101, "incorrect password")
		case 2:
			err = ChangePassword(ctx, 7101, 1, "incorrect password", "AnotherSyntheticPassword456", "AnotherSyntheticPassword456")
		}
		if !errors.Is(err, ErrCredentials) {
			t.Fatalf("failure %d did not return invalid credentials: %v", i, err)
		}
	}
	if _, err := Authenticate(ctx, "alias@example.com", reviewPassword); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("alias login bypassed shared failure budget: %v", err)
	}
	if _, err := VerifyAdminPassword(ctx, 7101, reviewPassword); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("admin password bypassed shared failure budget: %v", err)
	}
	if err := ChangePassword(ctx, 7101, 1, reviewPassword, "AnotherSyntheticPassword456", "AnotherSyntheticPassword456"); !errors.Is(err, apperrors.ErrRateLimited) {
		t.Fatalf("self password bypassed shared failure budget: %v", err)
	}
}

// TestAdminGeneratedPasswordsFollowRuntimeMinimum 验证管理员建号和重置密码遵循动态长度策略，并保留临时密码的强制修改状态。
func TestAdminGeneratedPasswordsFollowRuntimeMinimum(t *testing.T) {
	for _, minimum := range []int{15, 21, 64} {
		// 为当前密码下限重新准备独立账号数据，核对自动生成的初始密码和重置密码。
		t.Run(strconv.Itoa(minimum), func(t *testing.T) {
			reviewAccountDatabase(t)
			snapshot := config.Runtime()
			snapshot.AccountPolicy.MinPasswordLength = minimum
			if err := config.StoreRuntimeSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), "home_server.password_source_ip", "198.51.100.72")
			created, err := AdminCreate(ctx, 7101, 1, CreateInput{UserName: "generated-review", GeneratePassword: true})
			if err != nil {
				t.Fatalf("random creation under minimum %d failed: %v", minimum, err)
			}
			if err := ValidatePassword(created.InitialPassword, created.InitialPassword, minimum); err != nil {
				t.Fatalf("generated initial password violates policy: %v", err)
			}
			reset, err := AdminChange(ctx, 7101, 1, 7102, 1, "reset-password", AdminInput{Reason: "synthetic test", CurrentPassword: reviewPassword, GeneratePassword: true})
			if err != nil {
				t.Fatalf("random reset under minimum %d failed: %v", minimum, err)
			}
			if err := ValidatePassword(reset.InitialPassword, reset.InitialPassword, minimum); err != nil {
				t.Fatalf("generated reset password violates policy: %v", err)
			}
			if reset.InitialPassword == created.InitialPassword || !reset.User.MustChangePassword || reset.PasswordExpiresAt == nil {
				t.Fatal("generated credential did not preserve temporary one-time-display semantics")
			}
		})
	}
}
