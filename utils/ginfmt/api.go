package ginfmt

import (
	"errors"
	"github.com/gin-gonic/gin"
	apperrors "github.com/mcoder2014/home_server/errors"
)

// Success and Fail are the common API envelope for new modules. Legacy
// FormatWithData/FormatWithError keep their existing HTTP-200 wire contract.
func Success(c *gin.Context, status int, data interface{}) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, NewBaseResponse(data))
}

func Fail(c *gin.Context, err error) {
	category := apperrors.ErrDependency
	var typed *apperrors.APIError
	if errors.As(err, &typed) && typed != nil {
		category = typed
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(category.Status, NewErrorResponse(int(category.Code), category.Message))
}
