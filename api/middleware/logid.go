package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

func AddLogID(ctx *gin.Context) {
	if _, exist := ctx.Get(log.LogIDKey); exist {
		return
	}
	logid, err := uuid.NewUUID()
	if err != nil {
		logrus.Infof("Generator logid failed.")
		return
	}
	ctx.Set(log.LogIDKey, logid.String())
}
