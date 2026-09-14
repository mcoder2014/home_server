package passport

import (
	"context"
	"sync"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/rsa"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/pkg/errors"
)

var (
	rsaPubKey      []byte
	rsaPrvKey      []byte
	lastUpdateTime time.Time
	rsaLock        sync.Mutex
)

const rsaValidTime = 1 * time.Hour

type Service interface {
	GetIdentity(ctx context.Context, mobileEmailUsername string) (res *model.UserIdentity, err error)
}

func GetService() Service {
	return GetMockData()
}

func Init(conf *config.Config) error {
	if conf.IdentitySource == "database" {
		if _, err := accounts.ListActiveUsers(context.Background()); err != nil {
			return err
		}
		return nil
	}
	if conf.IdentitySource != "" && conf.IdentitySource != "config" && conf.IdentitySource != "file" {
		return errors.New("unsupported identity_source")
	}
	mock := GetMockData()
	err := mock.LoadConf(conf.Passport.MockData)
	if err != nil {
		return errors.Wrap(err, "passport init failed")
	}
	return nil
}

func GetIdentity(ctx context.Context, mobileEmailUsername string) (res *model.UserIdentity, err error) {
	if accounts.DatabaseMode() {
		user, e := accounts.GetByLogin(ctx, mobileEmailUsername)
		return accounts.UserIdentity(user), e
	}
	return GetService().GetIdentity(ctx, mobileEmailUsername)
}

// ValidateUser 按身份来源校验登录凭据，保留限流错误，并将其他凭据失败转换为旧版用户名密码错误码。
func ValidateUser(ctx context.Context, loginKey, loginPasswd string) (res *model.UserIdentity, err error) {
	if accounts.DatabaseMode() {
		user, e := accounts.Authenticate(ctx, loginKey, loginPasswd)
		if errors.Is(e, accounts.ErrCredentials) {
			return nil, myErrors.New(myErrors.ErrorCodeUserNameOrPasswdWrong)
		}
		return accounts.UserIdentity(user), e
	}

	identity, err := GetIdentity(ctx, loginKey)
	if err != nil {
		return nil, myErrors.Wrapf(err, myErrors.ErrorCodeUserNameOrPasswdWrong, "username or passwd wrong")
	}
	var id int64
	hash := ""
	if identity != nil {
		id, hash = identity.ID, identity.BcryptPassword
	}
	if err = accounts.VerifyPassword(ctx, id, loginKey, hash, loginPasswd); err != nil {
		if errors.Is(err, myErrors.ErrRateLimited) {
			return nil, err
		}
		return nil, myErrors.New(myErrors.ErrorCodeUserNameOrPasswdWrong)
	}
	return identity, nil
}

// GetByID 查询可正常使用的身份；数据库模式过滤停用与待改密账号，配置模式补齐原有模块权限。
func GetByID(ctx context.Context, id int64) (*model.UserIdentity, error) {
	if accounts.DatabaseMode() {
		user, err := accounts.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if user == nil || user.Status != model.AccountActive || user.MustChangePassword {
			return nil, nil
		}
		return accounts.UserIdentity(user), nil
	}
	user, err := GetMockData().GetByID(id)
	if err != nil || user == nil {
		return user, err
	}
	copy := *user
	copy.Role = model.RoleUser
	copy.LibraryEnabled = true
	copy.WebDAVPermission = model.WebDAVWrite
	return &copy, nil
}

// ListUsersWithError 统一列出数据库或配置中的身份，保留查询失败，并为配置用户补齐历史权限默认值。
func ListUsersWithError(ctx context.Context) ([]*model.UserIdentity, error) {
	if accounts.DatabaseMode() {
		users, err := accounts.ListActiveUsers(ctx)
		if err != nil {
			return nil, err
		}
		identities := make([]*model.UserIdentity, 0, len(users))
		for _, user := range users {
			identities = append(identities, accounts.UserIdentity(user))
		}
		return identities, nil
	}
	mock := GetMockData()
	mock.Lock.RLock()
	defer mock.Lock.RUnlock()
	identities := make([]*model.UserIdentity, 0, len(mock.UserIdentities))
	for _, user := range mock.UserIdentities {
		if user != nil {
			copy := *user
			copy.Role = model.RoleUser
			copy.LibraryEnabled = true
			copy.WebDAVPermission = model.WebDAVWrite
			identities = append(identities, &copy)
		}
	}
	return identities, nil
}

func GetLoginRsa(ctx context.Context) (pub, prv []byte, err error) {
	rsaLock.Lock()
	defer rsaLock.Unlock()

	if len(rsaPubKey) == 0 || time.Now().Sub(lastUpdateTime) > rsaValidTime {
		err = updateRsa(ctx)
		if err != nil {
			err = myErrors.Wrapf(err, myErrors.ErrorCodeGenRsaKeyFailed, "gen rsa key failed")
			return
		}
		return rsaPubKey, rsaPrvKey, nil
	}
	return rsaPubKey, rsaPrvKey, nil
}

func updateRsa(ctx context.Context) (err error) {
	rsaPubKey, rsaPrvKey, err = rsa.GenKey()

	if err != nil {
		return myErrors.Wrapf(err, myErrors.ErrorCodeGenRsaKeyFailed, "gen rsa key failed")
	}
	lastUpdateTime = time.Now()
	return nil
}
