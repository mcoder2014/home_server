package ginfmt

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/model"
	myErrors "github.com/mcoder2014/home_server/errors"
)

func FormatWithData(c *gin.Context, data interface{}) {
	resp := model.NewBaseResponse(data)
	c.JSON(http.StatusOK, resp)
}

func FormatWithError(c *gin.Context, err error) {
	if !errors.As(err, &myErrors.Error{}) {
		resp := model.NewErrorResponse(int(myErrors.ErrorCodeUnknownError), err.Error())
		c.JSON(http.StatusOK, resp)
	}

	myErr, ok := err.(myErrors.Error)
	if !ok {
		resp := model.NewErrorResponse(int(myErrors.ErrorCodeUnknownError), err.Error())
		c.JSON(http.StatusOK, resp)
	}

	resp := model.NewErrorResponse(int(myErr.Code), myErr.Error())
	c.JSON(http.StatusOK, resp)
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
