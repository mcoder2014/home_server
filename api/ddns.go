package api

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/data"
	"github.com/mcoder2014/home_server/utils"
	"github.com/sirupsen/logrus"
)

var (
	// Ipv4Map Key string domain, Value string ipv4
	Ipv4Map sync.Map

	// Ipv6Map Key string domain, Value string ipv6
	Ipv6Map sync.Map
)

// InitDDNSRouter 将旧内存地址表的维护限制为管理员操作，来源地址发现仍可匿名访问。
func InitDDNSRouter() error {
	admin := middleware.RequireAccount(true, false)
	write := middleware.BrowserWrite()
	data.AddRoute(http.MethodGet, "/ddns", admin, GetDomain)
	data.AddRoute(http.MethodGet, "/ddns/all", admin, GetAllRecords)
	data.AddRoute(http.MethodGet, "/ddns/real_ip", GetClientIpAddress)
	data.AddRoute(http.MethodPost, "/ddns/ipv4", admin, write, UpdateIpv4)
	data.AddRoute(http.MethodPost, "/ddns/ipv6", admin, write, UpdateIpv6)
	return nil
}

// UpdateIpv4 处理 POST /ddns/ipv4：将上报的 Domain/Ipv4 写入进程内记录表，供后续查询读取，不调用公网 DNS 提供商。
// 前置路由要求管理员身份及写入校验；正文有界且地址必须为有效 IPv4。
func UpdateIpv4(c *gin.Context) {

	// 解析参数
	type Request struct {
		Domain string
		Ipv4   string
		Name   string
	}

	req := &Request{}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	err := c.BindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Message": err.Error(),
		})
		return
	}
	if strings.TrimSpace(req.Domain) == "" || len(req.Domain) > 253 || net.ParseIP(req.Ipv4).To4() == nil {
		c.JSON(http.StatusBadRequest, gin.H{"Message": "domain or IPv4 is invalid"})
		return
	}

	// 处理
	old, ok := Ipv4Map.Load(req.Domain)
	if ok {
		if old.(string) == req.Ipv4 {
			logrus.Infof("Same as record, No need update. Domain:%v Record:%v", req.Domain, req.Ipv4)
			c.JSON(http.StatusOK, gin.H{
				"Message": "success",
			})
			return
		} else {
			logrus.Infof("Not same as old, Update old record. Domain:%v old record:%v new record:%v", req.Domain, old.(string), req.Ipv4)
		}
	}
	Ipv4Map.Store(req.Domain, req.Ipv4)
	c.JSON(http.StatusOK, gin.H{
		"Message": "Update Record",
	})

}

// UpdateIpv6 对应 POST /ddns/ipv6 的历史占位入口；当前函数尚未实现 IPv6 记录更新。
func UpdateIpv6(c *gin.Context) {

}

// GetDomain 处理 GET /ddns：按 domain 查询进程内保存的 IPv4/IPv6 记录，缺失地址以空值返回。
func GetDomain(c *gin.Context) {
	// 解析参数
	domain := c.Query("domain")

	type Resp struct {
		Domain string  `json:"domain"`
		Ipv4   *string `json:"ipv4"`
		Ipv6   *string `json:"ipv6"`
	}

	resp := Resp{Domain: domain}
	logrus.Infof("Ip: %v query domain %v", c.ClientIP(), domain)

	// 处理逻辑
	if ipv4, ok := Ipv4Map.Load(domain); ok {
		resp.Ipv4 = utils.String(ipv4.(string))
	}

	if ipv6, ok := Ipv6Map.Load(domain); ok {
		resp.Ipv6 = utils.String(ipv6.(string))
	}

	c.PureJSON(http.StatusOK, resp)
}

// GetAllRecords 处理 GET /ddns/all：复制并返回当前进程保存的全部地址记录；当前没有分页或账号访问过滤。
func GetAllRecords(c *gin.Context) {
	// 查询所有记录
	type Resp struct {
		Ipv4 map[string]string
		Ipv6 map[string]string
	}

	resp := Resp{
		Ipv4: map[string]string{},
		Ipv6: map[string]string{},
	}
	Ipv4Map.Range(func(key, value interface{}) bool {
		resp.Ipv4[key.(string)] = value.(string)
		return true
	})
	Ipv6Map.Range(func(key, value interface{}) bool {
		resp.Ipv6[key.(string)] = value.(string)
		return true
	})

	c.JSON(http.StatusOK, &resp)

}

// GetClientIpAddress 处理 GET /ddns/real_ip：返回 Gin 解析的客户端地址，供客户端发现来源 IP，不作为认证依据。
func GetClientIpAddress(c *gin.Context) {
	type Resp struct {
		Ip string
	}

	resp := Resp{}
	resp.Ip = c.ClientIP()

	c.JSON(http.StatusOK, &resp)
}
