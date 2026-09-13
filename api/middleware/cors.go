package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
)

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
