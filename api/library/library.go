package library

import (
	"net/http"

	"github.com/mcoder2014/home_server/data"
)

func InitRouter() {
	// 查询图书信息接口
	data.RouterMap["/bookinfo/query"] = data.HttpRoute{
		Method:  http.MethodGet,
		Path:    "/bookinfo/query",
		Handler: Query,
	}
}
