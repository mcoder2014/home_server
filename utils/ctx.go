package utils

import "context"

const (
	CtxKeyLoginUseID = "login_user_id"
	CtxKeyLoginToken = "login_token"
)

func GetUserIDFromCtx(ctx context.Context) int64 {
	v := ctx.Value(CtxKeyLoginUseID)
	if v == nil {
		return 0
	}
	return v.(int64)
}

func GetTokenFromCtx(ctx context.Context) string {
	v := ctx.Value(CtxKeyLoginToken)
	if v == nil {
		return ""
	}
	return v.(string)
}
