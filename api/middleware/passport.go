package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
)

const (
	HeaderKey = "passport"
)

// ValidateLogin 获取登录用户
func ValidateLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Printf("request:%+v", *c.Request)

		log.Ctx(ginfmt.RPCContext(c)).Infof("request:%+v", *c.Request)
		value := c.GetHeader(HeaderKey)

		_, err := passport.CheckToken(value)
		if err != nil {
			c.Redirect(http.StatusMovedPermanently, config.Global().Passport.RedirectLoginPath)
			c.Abort()
		}
		c.Next()
	}
}
