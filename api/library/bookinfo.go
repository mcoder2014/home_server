package library

import (
	"fmt"

	"github.com/mcoder2014/home_server/domain/service"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// Query 从 rpc 处查询书籍
func Query(c *gin.Context) {
	isbn := c.Query("isbn")
	if len(isbn) < 10 || len(isbn) > 13 {
		// error
		ginfmt.FormatWithError(c, errors.NewWithMessage(errors.ErrorCodeParamInvalid, fmt.Sprintf("isbn len is %v", len(isbn))))
		return
	}

	ctx := ginfmt.RPCContext(c)

	bookinfo, err := service.QueryByIsbn(ctx, isbn)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}

	ginfmt.FormatWithData(c, bookinfo)
}
