package passport

import (
	"context"
	"sync"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/rsa"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/log"
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
	mock := GetMockData()
	err := mock.LoadConf(conf.Passport.MockData)
	if err != nil {
		return errors.Wrap(err, "passport init failed")
	}
	err = InitToken()
	if err != nil {
		return errors.Wrap(err, "init token failed")
	}
	return nil
}

func GetIdentity(ctx context.Context, mobileEmailUsername string) (res *model.UserIdentity, err error) {
	return GetService().GetIdentity(ctx, mobileEmailUsername)
}

func ValidateUser(ctx context.Context, loginKey, loginPasswd string) (res *model.UserIdentity, err error) {

	identity, err := GetIdentity(ctx, loginKey)
	if err != nil || identity == nil {
		return nil, myErrors.Wrapf(err, myErrors.ErrorCodeUserNameOrPasswdWrong, "username or passwd wrong")
	}
	right := utils.ComparePasswords(identity.BcryptPassword, loginPasswd)
	if !right {
		return nil, myErrors.Wrapf(err, myErrors.ErrorCodeUserNameOrPasswdWrong, "username or passwd wrong")
	}
	return identity, nil
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
	log.Ctx(ctx).Infof("update rsa key: pubKey:\n%v\nprivate key:\n%v\n", string(rsaPubKey), string(rsaPrvKey))

	if err != nil {
		return myErrors.Wrapf(err, myErrors.ErrorCodeGenRsaKeyFailed, "gen rsa key failed")
	}
	lastUpdateTime = time.Now()
	return nil
}
