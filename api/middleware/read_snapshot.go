package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"net/http"
)

// EnableReadSnapshot preserves cancellation and confines reused authorization
// source rows to one read-only HTTP request. Write handlers always reread state.
func EnableReadSnapshot(c *gin.Context) {
	if c.Request == nil || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
		return
	}
	c.Request = c.Request.WithContext(accounts.WithReadSnapshot(c.Request.Context()))
}
