package ginfmt

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/log"
)

func FormatWithData(c *gin.Context, data interface{}) {
	resp := NewBaseResponse(data)
	c.JSON(http.StatusOK, resp)
}

func FormatWithError(c *gin.Context, err error) {
	log.Ctx(RPCContext(c)).Errorf("Error:%+v", err)
	if !errors.As(err, &myErrors.Error{}) {
		resp := NewErrorResponse(int(myErrors.ErrorCodeUnknownError), err.Error())
		c.JSON(http.StatusOK, resp)
		return
	}

	myErr, ok := err.(myErrors.Error)
	if !ok {
		resp := NewErrorResponse(int(myErrors.ErrorCodeUnknownError), err.Error())
		c.JSON(http.StatusOK, resp)
		return
	}

	resp := NewErrorResponse(int(myErr.Code), myErr.Error())
	c.JSON(http.StatusOK, resp)
	return
}

func RPCContext(ginCtx *gin.Context) context.Context {
	ctx := context.Background()
	if ginCtx.Keys != nil {
		for key, val := range ginCtx.Keys {
			ctx = context.WithValue(ctx, key, val)
		}
	}
	return ctx
}
