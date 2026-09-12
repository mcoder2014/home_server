package auth

import (
	"context"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/passport"
	apperrors "github.com/mcoder2014/home_server/errors"
)

type BrowserSession struct {
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	ExpireTime time.Time `json:"expire_time"`
}

// GetBrowserSession resolves the same login_token used by existing user APIs.
// No user/profile claims from a signed browser cookie are accepted as authority.
func GetBrowserSession(ctx context.Context, token string) (*BrowserSession, error) {
	if token == "" {
		return nil, apperrors.ErrUnauthorized
	}
	row, err := dal.QueryByToken(token)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if row == nil || row.IsExpired != model.UserTokenNotExpired || !row.ExpireTime.After(time.Now()) {
		return nil, apperrors.ErrUnauthorized
	}
	user, err := passport.GetMockData().GetByID(row.UserID)
	if err != nil || user == nil || user.ID <= 0 {
		return nil, apperrors.ErrUnauthorized
	}
	return &BrowserSession{UserID: strconv.FormatInt(user.ID, 10), UserName: user.UserName, ExpireTime: row.ExpireTime}, nil
}
