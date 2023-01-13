package passport

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/log"
)

const (
	// TokenExpireTime Token 过期时间
	TokenExpireTime = 7 * 24 * time.Hour
)

func GenToken(identity *model.UserIdentity) (string, error) {
	token := BuildUserToken(identity.ID, TokenExpireTime)
	_, err := dal.CreateToken(token)
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

func CheckToken(ctx context.Context, token string) (*model.UserIdentity, error) {
	tokenEntity, err := dal.QueryByToken(token)
	if err != nil {
		return nil, err
	}
	if tokenEntity == nil {
		return nil, myErrors.New(myErrors.ErrorCodeUserNotLogin)
	}
	if tokenEntity.IsExpired == model.UserTokenExpired {
		return nil, myErrors.New(myErrors.ErrorCodeUserLoginExpire)
	}

	if tokenEntity.ExpireTime.After(time.Now()) {
		log.Ctx(ctx).Infof("get tokenEntity success :%+v", *tokenEntity)
		return GetMockData().GetByID(tokenEntity.UserID)
	}

	// 设置过期
	_ = dal.ExpireToken(tokenEntity.ID)
	return nil, myErrors.New(myErrors.ErrorCodeUserLoginExpire)
}

func DeleteToken(ctx context.Context, token string) error {
	tokenEntity, err := dal.QueryByToken(token)
	if err != nil {
		return err
	}
	if tokenEntity == nil || tokenEntity.IsExpired == model.UserTokenExpired {
		return nil
	}
	return dal.ExpireToken(tokenEntity.ID)
}

func BuildUserToken(userID int64, expireTime time.Duration) *model.UserToken {
	if expireTime <= 0 {
		expireTime = TokenExpireTime
	}

	tokenString := fmt.Sprintf("%d-%s-%d", time.Now().Unix(), uuid.New().String(), userID)

	token := &model.UserToken{
		ID:         utils.GenInt64ID(),
		UserID:     userID,
		Token:      tokenString,
		ExpireTime: time.Now().Add(expireTime),
		IsExpired:  model.UserTokenNotExpired,
	}
	return token
}
