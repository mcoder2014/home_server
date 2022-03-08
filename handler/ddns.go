package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/pointer"
	"github.com/sirupsen/logrus"
	"net/http"
	"sync"
)

var(
	// Ipv4Map Key string domain, Value string ipv4
	Ipv4Map sync.Map

	// Ipv6Map Key string domain, Value string ipv6
	Ipv6Map sync.Map
)

func UpdateIpv4(c *gin.Context) {

	// 解析参数


}

func UpdateIpv6(c *gin.Context) {

}

func GetDomain(c *gin.Context) {
	// 解析参数
	domain := c.Query("domain")

	type Resp struct {
		Domain string `json:"domain"`
		Ipv4 *string `json:"ipv4"`
		Ipv6 *string `json:"ipv6"`
	}

	resp := Resp{Domain: domain}
	logrus.Infof("Ip: %v query domain %v", c.ClientIP(), domain)

	// 处理逻辑
	if ipv4, ok := Ipv4Map.Load(domain); ok {
		resp.Ipv4 = pointer.String(ipv4.(string))
	}

	if ipv6, ok:= Ipv6Map.Load(domain); ok {
		resp.Ipv6 = pointer.String(ipv6.(string))
	}

	c.PureJSON(http.StatusOK, resp)
}