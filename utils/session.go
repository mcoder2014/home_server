package utils

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// SetSession 设置session
func SetSession(c *gin.Context, fieldMap map[string]interface{}) {
	s := sessions.Default(c)
	for key, value := range fieldMap {
		s.Set(key, value)
	}

	err := s.Save()
	if err != nil {
		logrus.Warnf("set session failed：%+v", err)
	}
}

// GetSession 获取session
func GetSession(c *gin.Context, key string) interface{} {
	s := sessions.Default(c)
	return s.Get(key)
}

// DeleteSession 删除session
func DeleteSession(c *gin.Context, key string) {
	s := sessions.Default(c)
	s.Delete(key)
	_ = s.Save()
}

// ClearSession 清空session
func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	_ = s.Save()
}
