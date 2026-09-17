package passport

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/domain/service/rsa"
	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/pkg/errors"
)

// QueryLoginRsa 查询用于 web 加密的 rsa 秘钥
// QueryLoginRsa 处理 GET /passport/rsa：返回旧登录协议使用的公钥，不向客户端暴露私钥。
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

// Login 处理 POST /passport/login 的旧客户端登录：解码 RSA 密文、校验账号密码并返回兼容用户令牌。
// 在 JSON/RSA 解码前限制请求体和来源频率；数据库模式要求可信 HTTPS，保持旧响应封装及令牌协议。
func Login(c *gin.Context) {
	if accounts.DatabaseMode() && !middleware.IsHTTPS(c) {
		ginfmt.Fail(c, myErrors.ErrForbidden)
		return
	}
	source := middleware.TrustedClientIP(c.Request)
	c.Set(accounts.PasswordSourceIPKey, source)
	if !middleware.AllowAccountAttempt("legacy-login:"+source, 30, time.Minute) {
		ginfmt.FormatWithError(c, myErrors.ErrRateLimited)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
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

	metadata := accounts.NewSessionMetadata(source, c.Request.UserAgent(), "legacy")
	token, err := passport.GenTokenWithContext(ctx, res, metadata)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}

	ginfmt.FormatWithData(c, LoginResponse{
		UserName: res.UserName,
		Token:    token,
	})
}

// Logout 处理 POST /passport/logout：撤销身份中间件取得的当前用户令牌，并清除对应浏览器会话 Cookie。
func Logout(c *gin.Context) {
	ctx := ginfmt.RPCContext(c)
	token := utils.GetTokenFromCtx(ctx)
	if token == "" {
		ginfmt.FormatWithError(c, errors.New("token is empty"))
		return
	}
	err := passport.DeleteToken(ctx, token)
	if err != nil {
		ginfmt.FormatWithError(c, err)
		return
	}
	utils.ClearBrowserSession(c)
	ginfmt.FormatWithData(c, nil)
}
