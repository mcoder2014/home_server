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
	type Request struct {
		Domain string 
		Ipv4 string
		Name string
	}

	req := &Request{}
	err := c.BindJSON(&req)
	if err!=nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Message":err.Error(),
		})
	}

	// 处理
	old, ok :=Ipv4Map.Load(req.Domain)
	if ok {
		if old.(string) == req.Ipv4 {
			logrus.Infof("Same as record, No need update. Domain:%v Record:%v",req.Domain, req.Ipv4)
			c.JSON(http.StatusOK, gin.H{
				"Message":"success",
			})
		} else {
			logrus.Infof("Not same as old, Update old record. Domain:%v old record:%v new record:%v", req.Domain, old.(string), req.Ipv4)
		}
	}
	Ipv4Map.Store(req.Domain, req.Ipv4)
	c.JSON(http.StatusOK, gin.H{
		"Message":"Update Record",
	})

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

func  GetAllRecords(c *gin.Context) {
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

func GetClientIpAddress(c *gin.Context) {
	type Resp struct {
		Ip string
	}

	resp := Resp{}
	resp.Ip = c.ClientIP()

	c.JSON(http.StatusOK, &resp)
}