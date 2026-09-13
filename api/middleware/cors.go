package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
)

// CORS 根据启动配置创建精确 Origin 白名单和跨域预检规则，允许受信任站点携带凭据访问 API。
// 此中间件限制浏览器来源，不替代接口自身的用户或应用认证。
func CORS() gin.HandlerFunc {
	origins := append([]string(nil), config.Global().Auth.SiteOrigins...)
	if origin := config.Global().Auth.SiteOrigin; origin != "" {
		origins = append(origins, origin)
	}
	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			for _, allowed := range origins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"*"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})

}
