package utils

import (
	"context"
	"strings"
)

const (
	CtxKeyLoginUseID = "login_user_id"
	CtxKeyLoginToken = "login_token"
	CtxKeyPrincipal  = "principal"
)

// Principal keeps authentication kind distinct from the user whose existing
// resource permissions apply. Secrets and access tokens never belong here.
type Principal struct {
	Kind          string
	UserID        int64
	ApplicationID int64
	Scopes        []string
}

func (p *Principal) Allows(scope string) bool {
	if p == nil || scope == "" {
		return false
	}
	for _, allowed := range p.Scopes {
		if allowed == scope || (strings.HasSuffix(scope, ":read") && allowed == strings.TrimSuffix(scope, ":read")+":write") {
			return true
		}
	}
	return false
}

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
