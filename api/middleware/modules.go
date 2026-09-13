package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func RequireModule(namespace string) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, err := accounts.ModuleEnabled(ginfmt.RPCContext(c), namespace)
		if err != nil {
			ginfmt.Fail(c, err)
			c.Abort()
			return
		}
		if !enabled {
			ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrForbidden, "功能暂未开放"))
			c.Abort()
			return
		}
		c.Next()
	}
}
