package library

import (
	"net/http"

	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	// 查询图书信息接口
	data.AddRoute(http.MethodGet, "/bookinfo/query", middleware.ValidateLogin(), QueryBookInfo)
	// 新增库存接口
	data.AddRoute(http.MethodPost, "/library/book/add", middleware.ValidateLogin(), AddStorage)
	// 查询库存接口
	data.AddRoute(http.MethodGet, "/library/book/query", middleware.ValidateLogin(), QueryStorage)
	// 分页查询所有库存信息
	data.AddRoute(http.MethodGet, "/library/book/total", middleware.ValidateLogin(), GetTotalBookStorage)
	// 新增地址
	data.AddRoute(http.MethodPost, "/library/address/add", middleware.ValidateLogin(), AddAddress)
	return nil
}
