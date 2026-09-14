package library

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
)

// AddStorage 处理 POST /library/book/add：检查 ISBN 是否已有库存，再向共享藏书写入数量、存放类型与位置。
// 接口保留既有重复库存错误语义，写权限在认证链和服务事务内复核。
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

// QueryStorage 处理 GET /library/book/query：按 ISBN 查询共享库存，参数为空或库存缺失时返回对应业务错误。
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

// AddAddress 处理 POST /library/address/add：创建共享藏书的存放地址，返回新地址 ID；写权限由认证链及服务层复核。
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

// GetTotalBookStorage 处理 GET /library/book/total：按 offset/limit 读取共享库存，并附带总记录数；统计失败保留现有日志与响应语义。
func GetTotalBookStorage(c *gin.Context) {

	type Response struct {
		BookStorages []*model.BookStorage `json:"book_storages"`
		Count        int                  `json:"count"`
	}

	// 获得查询参数
	offset, e := ginfmt.GetInt(c, "offset")
	if e != nil {
		ginfmt.FormatWithError(c, e)
		return
	}
	limit, e := ginfmt.GetInt(c, "limit")
	if e != nil {
		ginfmt.FormatWithError(c, e)
		return
	}

	ctx := ginfmt.RPCContext(c)
	bookStorage, e := service.GetTotalStorage(ctx, offset, limit)
	if e != nil {
		ginfmt.FormatWithError(c, e)
		return
	}
	count, e := dal.GetBookStorageCount()
	if e != nil {
		log.Ctx(ctx).WithError(e).Errorf("GetTotalBookStorage get count error")
	}
	resp := &Response{
		BookStorages: bookStorage,
		Count:        count,
	}
	ginfmt.FormatWithData(c, resp)
}
