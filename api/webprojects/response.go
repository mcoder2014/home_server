package webprojects

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
)

const (
	codeInvalid      = 501
	codeUnauthorized = 502
	codeForbidden    = 503
	codeNotFound     = 504
	codeConflict     = 505
	codeTooLarge     = 506
	codeUnsupported  = 507
	codeRateLimited  = 508
	codeDependency   = 509
	codeUnprocessable = 510
)

func success(c *gin.Context, status int, data interface{}) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"code": 0, "message": "success", "data": data})
}

func fail(c *gin.Context, status, code int, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"code": code, "message": message, "data": nil})
}

func failWithError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalid):
		fail(c, http.StatusBadRequest, codeInvalid, err.Error())
	case errors.Is(err, service.ErrUnauthorized):
		fail(c, http.StatusUnauthorized, codeUnauthorized, "authentication required")
	case errors.Is(err, service.ErrForbidden):
		fail(c, http.StatusForbidden, codeForbidden, "forbidden")
	case errors.Is(err, service.ErrNotFound):
		fail(c, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, service.ErrConflict):
		fail(c, http.StatusConflict, codeConflict, err.Error())
	case errors.Is(err, service.ErrTooLarge):
		fail(c, http.StatusRequestEntityTooLarge, codeTooLarge, err.Error())
	case errors.Is(err, service.ErrUnsupported):
		fail(c, http.StatusUnsupportedMediaType, codeUnsupported, err.Error())
	case errors.Is(err, service.ErrUnprocessable):
		fail(c, http.StatusUnprocessableEntity, codeUnprocessable, err.Error())
	case errors.Is(err, service.ErrRateLimited):
		fail(c, http.StatusTooManyRequests, codeRateLimited, err.Error())
	default:
		fail(c, http.StatusServiceUnavailable, codeDependency, "service unavailable")
	}
}
