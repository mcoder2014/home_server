package library

import (
	"net/http"

	"github.com/mcoder2014/home_server/data"
)

func InitRouter() {
	// 查询图书信息接口
	data.AddRoute(http.MethodGet, "/bookinfo/query", QueryBookInfo)

	// 新增库存接口
	data.AddRoute(http.MethodPost, "/library/book/add", AddStorage)
	// 查询库存接口
	data.AddRoute(http.MethodGet, "/library/book/query", QueryStorage)
	// 分页查询所有库存信息
	data.AddRoute(http.MethodGet, "/library/book/total", GetTotalBookStorage)
	// 新增地址
	data.AddRoute(http.MethodPost, "/library/address/add", AddAddress)
}
