package passport

import (
	"encoding/base64"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/rsa"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/pkg/errors"
)

// QueryLoginRsa 查询用于 web 加密的 rsa 秘钥
func QueryLoginRsa(c *gin.Context) {
	ctx := ginfmt.RPCContext(c)
	pubKey, _, err := passport.GetLoginRsa(ctx)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}
	ginfmt.FormatWithData(c, string(pubKey))
}

type LoginParam struct {
	// UserName 登录凭证，用户名、手机、邮箱之一
	UserName string `json:"user_name"`
	// CryptPasswd 加密密码，由 Rsa 加密
	CryptPasswd string `json:"crypt_passwd"`
}
type LoginResponse struct {
	// UserName 登录凭证，用户名、手机、邮箱之一
	UserName string `json:"user_name"`
	// Token 后续随请求需要携带 token，否则会跳转回 login 页面
	Token string `json:"token"`
}

func Login(c *gin.Context) {
	ctx := ginfmt.RPCContext(c)
	param := LoginParam{}
	err := c.BindJSON(&param)
	if err != nil {
		err = myErrors.Wrapf(err, myErrors.ErrorCodeParamInvalid, "param invalid")
		ginfmt.FormatWithError(c, err)
		return
	}
	_, prv, err := passport.GetLoginRsa(ctx)
	if err != nil {
		err = errors.Wrapf(err, "GetLoginRsa failed")
		ginfmt.FormatWithError(c, err)
		return
	}
	preDecode, err := base64.StdEncoding.DecodeString(param.CryptPasswd)
	if err != nil {
		err = errors.Wrapf(err, "base64 Decrypt failed")
		ginfmt.FormatWithError(c, err)
		return
	}

	plainPasswd, err := rsa.Decrypt(prv, preDecode)
	if err != nil {
		err = errors.Wrapf(err, "Decrypt failed")
		ginfmt.FormatWithError(c, err)
		return
	}

	res, err := passport.ValidateUser(ctx, param.UserName, string(plainPasswd))
	if err != nil {
		err = errors.Wrapf(err, "ValidateUser failed")
		ginfmt.FormatWithError(c, err)
		return
	}
	if res == nil {
		ginfmt.FormatWithError(c, errors.New("res should not be nil, user not found"))
		return
	}
	utils.SetSession(c, map[string]interface{}{
		"user_id":   res.ID,
		"user_name": res.UserName,
	})
	token, err := passport.GenToken(res)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}

	ginfmt.FormatWithData(c, LoginResponse{
		UserName: res.UserName,
		Token:    token,
	})
}
