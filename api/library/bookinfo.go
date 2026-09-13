package library

import (
	"fmt"

	"github.com/mcoder2014/home_server/domain/service"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// QueryBookInfo 处理 GET /bookinfo/query：按 ISBN 查询书目；本地缺失时由服务层查询外部服务并保存结果。
func QueryBookInfo(c *gin.Context) {
	isbn := c.Query("isbn")
	if len(isbn) < 10 || len(isbn) > 13 {
		// error
		ginfmt.FormatWithError(c, errors.NewWithMessage(errors.ErrorCodeParamInvalid, fmt.Sprintf("isbn len is %v", len(isbn))))
		return
	}

	ctx := ginfmt.RPCContext(c)

	bookinfo, err := service.QueryBookInfoByIsbn(ctx, isbn)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}

	ginfmt.FormatWithData(c, bookinfo)
}
