package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

const (
	HeaderKey = "passport"
)

// ValidateLogin 获取登录用户
func ValidateLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Printf("request:%+v", *c.Request)
		ctx := ginfmt.RPCContext(c)

		log.Ctx(ctx).Infof("request:%+v", *c.Request)
		value := c.GetHeader(HeaderKey)

		userEntity, err := passport.CheckToken(ctx, value)
		if err != nil {
			log.Ctx(ctx).Infof("CheckToken err:%+v", err)
			ginfmt.FormatWithError(c, err)
			c.Abort()
		} else {
			log.Ctx(ctx).Infof("request userID: %d token: %s", userEntity.ID, value)
			// 配置登录信息到上下文
			c.Set(utils.CtxKeyLoginUseID, userEntity.ID)
			c.Set(utils.CtxKeyLoginToken, value)
		}
		c.Next()
	}
}

func ValidateBasicAuth() gin.HandlerFunc {

	unauthorized := func(c *gin.Context) {
		c.Writer.Header().Set("WWW-Authenticate", `basic realm="Restricted"`)
		c.Writer.WriteHeader(http.StatusUnauthorized)
		c.Abort()
	}

	return func(c *gin.Context) {
		ctx := ginfmt.RPCContext(c)
		username, password, ok := c.Request.BasicAuth()
		if !ok {
			unauthorized(c)
			log.Ctx(ctx).Infof("Not login")
			return
		}

		res, err := passport.ValidateUser(ctx, username, password)
		if err != nil {
			unauthorized(c)
			log.Ctx(ctx).Warnf("user:%v, err:%+v", username, err)
			return
		}
		if res == nil {
			unauthorized(c)
			log.Ctx(ctx).Warnf("user not found:%v, err:%+v", username, err)
			return
		}

		logrus.Infof("basic auth success, user:%v", username)
		c.Next()
	}
}
