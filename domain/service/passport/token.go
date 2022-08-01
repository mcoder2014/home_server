package passport

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mcoder2014/home_server/domain/model"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/sirupsen/logrus"
)

var (
	tokenStore     = map[string]*TokenValue{}
	tokenStoreLock sync.RWMutex
)

const (
	// TokenExpireTime Token 过期时间
	TokenExpireTime = 7 * 24 * time.Hour
	// TokenExpireCheckDuration 检查 token 过期间隔
	TokenExpireCheckDuration = 1 * time.Hour
)

type TokenValue struct {
	// Identity 身份信息
	Identity *model.UserIdentity
	// 过期时间
	ExpireTime time.Time
}

func GenToken(identity *model.UserIdentity) (string, error) {
	uid, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}
	tokenStoreLock.Lock()
	defer tokenStoreLock.Unlock()

	tokenStore[uid.String()] = &TokenValue{
		Identity:   identity,
		ExpireTime: time.Now().Add(TokenExpireTime),
	}
	return uid.String(), nil
}

func CheckToken(token string) (*model.UserIdentity, error) {
	tokenStoreLock.RLock()
	defer tokenStoreLock.RUnlock()

	tokenValue, ok := tokenStore[token]
	if !ok {
		return nil, myErrors.New(myErrors.ErrorCodeUserNotLogin)
	}
	if tokenValue.ExpireTime.Before(time.Now()) {
		return nil, myErrors.New(myErrors.ErrorCodeUserLoginExpire)
	}
	return tokenValue.Identity, nil
}

func DeleteToken(token string) error {
	tokenStoreLock.Lock()
	defer tokenStoreLock.Unlock()

	delete(tokenStore, token)
	return nil
}

func CleanExpireToken() error {
	tokenStoreLock.Lock()
	defer tokenStoreLock.Unlock()

	for token, value := range tokenStore {
		if value.ExpireTime.Add(TokenExpireTime).Before(time.Now()) {
			delete(tokenStore, token)
		}
	}
	return nil
}

func InitToken() error {
	go func() {
		ticker := time.Tick(TokenExpireCheckDuration)
		select {
		case <-ticker:
			err := CleanExpireToken()
			if err != nil {
				logrus.WithError(err).Warnf("CleanToken failed")
			}
		}
	}()
	return nil
}
