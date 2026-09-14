package applications

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	applicationApp "github.com/mcoder2014/home_server/app/applications"
	appErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

const maxApplicationCredentialManagementRequestBytes = 16 << 10

// listApplicationCredentials 处理 GET /api/applications：按游标列出当前真实用户名下的应用凭证元信息。
func listApplicationCredentials(c *gin.Context) {
	cursor, err := parseOptionalApplicationCredentialCursor(c.Query("cursor"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	limit, err := parseApplicationCredentialListLimit(c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	response, err := applicationApp.ListApplicationCredentials(c.Request.Context(), applicationManagementActor(c), cursor, limit)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

// createApplicationCredential 处理 POST /api/applications：为本人创建带业务 scope 的应用，成功响应仅此次返回新密钥。
func createApplicationCredential(c *gin.Context) {
	var request applicationApp.CreateApplicationCredentialRequest
	if err := decodeApplicationCredentialManagementJSON(c, &request); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.CreateApplicationCredential(c.Request.Context(), applicationManagementActor(c), request)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, response)
}

// getApplicationCredential 处理 GET /api/applications/:id：读取本人的应用详情，不能通过资源 ID 查询其他用户凭证。
func getApplicationCredential(c *gin.Context) {
	applicationID, err := parsePositiveApplicationManagementInt64(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	response, err := applicationApp.GetApplicationCredential(c.Request.Context(), applicationManagementActor(c), applicationID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

// updateApplicationCredential 处理 PATCH /api/applications/:id：按 If-Match 版本修改本人应用的属性、权限或启用状态。
func updateApplicationCredential(c *gin.Context) {
	applicationID, revision, err := parseApplicationCredentialManagementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var request applicationApp.UpdateApplicationCredentialRequest
	if err := decodeApplicationCredentialManagementJSON(c, &request); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.UpdateApplicationCredential(c.Request.Context(), applicationManagementActor(c), applicationID, revision, request)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

// rotateApplicationSecret 处理 POST /api/applications/:id/rotate：按版本轮换本人应用密钥，返回新密钥并使旧凭证版本失效。
func rotateApplicationSecret(c *gin.Context) {
	applicationID, revision, err := parseApplicationCredentialManagementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.RotateApplicationSecret(c.Request.Context(), applicationManagementActor(c), applicationID, revision)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

// revokeApplicationCredential 处理 DELETE /api/applications/:id：按版本永久吊销本人的应用凭证，而非删除其他用户资源。
func revokeApplicationCredential(c *gin.Context) {
	applicationID, revision, err := parseApplicationCredentialManagementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.RevokeApplicationCredential(c.Request.Context(), applicationManagementActor(c), applicationID, revision)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

// parseApplicationCredentialManagementTarget 共同解析凭证管理动作的资源 ID 和 If-Match 版本，任一非法就阻止后续变更。
func parseApplicationCredentialManagementTarget(c *gin.Context) (int64, int64, error) {
	applicationID, err := parsePositiveApplicationManagementInt64(c.Param("id"))
	if err != nil {
		return 0, 0, appErrors.ErrInvalid
	}
	revision, err := parseApplicationCredentialRevision(c.GetHeader("If-Match"))
	if err != nil {
		return 0, 0, appErrors.ErrInvalid
	}
	return applicationID, revision, nil
}

// applicationManagementActor 从认证中间件写入的 Gin 或请求上下文取得操作者，保留完整用户 Principal。
func applicationManagementActor(c *gin.Context) *utils.Principal {
	if value, ok := c.Get(utils.CtxKeyPrincipal); ok {
		if principal, ok := value.(*utils.Principal); ok {
			return principal
		}
	}
	principal, _ := c.Request.Context().Value(utils.CtxKeyPrincipal).(*utils.Principal)
	return principal
}

// decodeApplicationCredentialManagementJSON 只接受有界 application/json 管理请求；拒绝未知字段、过大的正文及尾随 JSON，避免管理操作误解输入。
func decodeApplicationCredentialManagementJSON(c *gin.Context, destination interface{}) error {
	if c.ContentType() != "application/json" {
		return appErrors.ErrUnsupported
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxApplicationCredentialManagementRequestBytes+1))
	if err != nil || len(body) > maxApplicationCredentialManagementRequestBytes {
		return appErrors.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return appErrors.ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return appErrors.ErrInvalid
	}
	return nil
}

func parseApplicationCredentialRevision(value string) (int64, error) {
	if strings.HasPrefix(value, `W/`) || strings.Contains(value, ",") {
		return 0, appErrors.ErrInvalid
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return parsePositiveApplicationManagementInt64(value)
}

func parseOptionalApplicationCredentialCursor(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return parsePositiveApplicationManagementInt64(value)
}

func parsePositiveApplicationManagementInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, appErrors.ErrInvalid
	}
	return parsed, nil
}

func parseApplicationCredentialListLimit(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, appErrors.ErrInvalid
	}
	return parsed, nil
}
