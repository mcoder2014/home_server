package ginfmt

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/errors"
)

func GetInt(c *gin.Context, field string) (int, error) {
	val := c.Query(field)
	res, e := strconv.Atoi(val)
	if e != nil {
		e = errors.Wrap(e, errors.ErrorCodeParamInvalid)
	}
	return res, e
}

func GetInt64(c *gin.Context, field string) (int64, error) {
	val := c.Query(field)
	res, e := strconv.ParseInt(val, 10, 64)
	if e != nil {
		e = errors.Wrap(e, errors.ErrorCodeParamInvalid)
	}
	return res, e
}
