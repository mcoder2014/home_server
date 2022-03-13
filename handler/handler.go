package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/data"
	"net/http"
)

func Hi(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Hi",
	})
}

func init () {
	data.RouterMap["/ddns"] = data.HttpRoute{
		Method: http.MethodGet,
		Path: "/ddns",
		Handler: GetDomain,
	}

	data.RouterMap["/ddns/all"] = data.HttpRoute{
		Method: http.MethodGet,
		Path: "/ddns/all",
		Handler: GetAllRecords,
	}

	data.RouterMap["/ddns/real_ip"] = data.HttpRoute{
		Method: http.MethodGet,
		Path: "ddns/real_ip",
		Handler: GetClientIpAddress,
	}

	data.RouterMap["/ddns/ipv4"] = data.HttpRoute{
		Method: http.MethodPost,
		Path: "/ddns/ipv4",
		Handler: UpdateIpv4,
	}

	data.RouterMap["ddns/ipv6"] = data.HttpRoute{
		Method: http.MethodPost,
		Path: "/ddns/ipv6",
		Handler: UpdateIpv6,
	}
}