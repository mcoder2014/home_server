package feishu

import (
	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
)

func HandleEvent(c *gin.Context) {
	ctx := ginfmt.RPCContext(c)
	bodyBytes, err := c.GetRawData()
	if err != nil {
		ginfmt.FormatWithError(c, errors.Wrap(err, errors.ErrorCodeParamInvalid))
		return
	}
	var encryptEvent EncryptEvent
	err = jsoniter.Unmarshal(bodyBytes, &encryptEvent)
	if err != nil {
		ginfmt.FormatWithError(c, errors.Wrap(err, errors.ErrorCodeDecryptFailed))
		return
	}
	content, err := Decrypt(encryptEvent.Encrypt, config.Global().Feishu.EncryptKey)
	log.Ctx(ctx).Infof("get event: %v", content)
	var event EventV2
	err = jsoniter.Unmarshal([]byte(content), &event)
	if err != nil {
		ginfmt.FormatWithError(c, errors.Wrap(err, errors.ErrorCodeParamInvalid))
		return
	}
	if event.Type == "url_verification" {
		handleChallenge(c, event.Challenge)
		return
	}
}

func handleChallenge(c *gin.Context, challenge string) {
	ctx := ginfmt.RPCContext(c)
	log.Ctx(ctx).Infof("get challenge: %v", challenge)
	c.JSON(200, gin.H{
		"challenge": challenge,
	})
}
