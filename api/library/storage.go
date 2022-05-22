package library

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
)

// AddStorage 添加库存
// Post
func AddStorage(c *gin.Context) {
	type Request struct {
		Isbn     string            `json:"isbn"`
		Quantity int               `json:"quantity"`
		Type     model.StorageType `json:"type"`
		LibId    int64             `json:"lib_id"`
	}
	var req Request
	e := c.BindJSON(&req)
	if e != nil {
		ginfmt.FormatWithError(c, errors.Wrap(e, errors.ErrorCodeParamInvalid))
		return
	}

	ctx := ginfmt.RPCContext(c)

	// 1. 检查是否已有库存
	s, e := service.QueryStorageByIsbn(ctx, req.Isbn)
	if e != nil {
		log.Ctx(ctx).WithError(e).Errorf("AddStorage pre check failed.")
		ginfmt.FormatWithError(c, errors.Wrap(e, errors.ErrorCodePreCheckFailed))
		return
	}
	if s != nil {
		log.Ctx(ctx).WithError(e).Errorf("库存记录已存在")
		ginfmt.FormatWithError(c, errors.Wrap(e, errors.ErrorCodeStorageHasExist))
		return
	}

	// 2. 新增库存
	e = service.AddStorageByIsbn(ctx, req.Isbn, req.Quantity, req.Type, req.LibId)
	if e != nil {
		ginfmt.FormatWithError(c, errors.Wrap(e, errors.ErrorCodeDbError))
		return
	}

	ginfmt.FormatWithData(c, nil)
}

// QueryStorage 查询库存信息
func QueryStorage(c *gin.Context) {
	isbn := c.Query("isbn")
	if isbn == "" {
		ginfmt.FormatWithError(c, errors.New(errors.ErrorCodeParamInvalid))
		return
	}
	ctx := ginfmt.RPCContext(c)

	s, e := service.QueryStorageByIsbn(ctx, isbn)
	if e != nil {
		ginfmt.FormatWithError(c, e)
		return
	}
	if s == nil {
		ginfmt.FormatWithError(c, errors.New(errors.ErrorCodeStorageNotFount))
		return
	}
	ginfmt.FormatWithData(c, s)
}

func AddAddress(c *gin.Context) {
	type Request struct {
		Address   string `json:"address"`
		ShortName string `json:"short_name"`
	}
	type Response struct {
		ID int64
	}
	var req Request
	e := c.BindJSON(&req)
	if e != nil {
		ginfmt.FormatWithError(c, errors.Wrap(e, errors.ErrorCodeParamInvalid))
		return
	}

	a := model.BookAddress{
		Address:   req.Address,
		ShortName: req.ShortName,
	}
	ctx := ginfmt.RPCContext(c)
	id, e := service.AddAddress(ctx, &a)
	if e != nil {
		ginfmt.FormatWithError(c, e)
		return
	}
	resp := Response{
		ID: id,
	}
	ginfmt.FormatWithData(c, resp)
}
