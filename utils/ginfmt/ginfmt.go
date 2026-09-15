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

// FormatWithError preserves legacy HTTP 200 responses while keeping the shared
// numeric code through wrapped errors. Internal causes are logged, not serialized.
func FormatWithError(c *gin.Context, err error) {
	log.Ctx(RPCContext(c)).Errorf("Error:%+v", err)
	code := myErrors.ErrorCodeUnknownError
	message := myErrors.PublicMessage(code)
	var apiError *myErrors.APIError
	var domainError *myErrors.Error
	var valueError myErrors.Error
	switch {
	case errors.As(err, &apiError) && apiError != nil:
		code, message = apiError.Code, apiError.Message
	case errors.As(err, &domainError) && domainError != nil:
		code = domainError.Code
		message = myErrors.PublicMessage(code)
	case errors.As(err, &valueError):
		code = valueError.Code
		message = myErrors.PublicMessage(code)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, NewErrorResponse(int(code), message))
}

func RPCContext(ginCtx *gin.Context) context.Context {
	ctx := context.Background()
	if ginCtx.Request != nil {
		ctx = ginCtx.Request.Context()
	}
	if ginCtx.Keys != nil {
		for key, val := range ginCtx.Keys {
			ctx = context.WithValue(ctx, key, val)
		}
	}
	return ctx
}
