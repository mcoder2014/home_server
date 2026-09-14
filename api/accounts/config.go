package accounts

import (
	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/app/siteconfig"
	"github.com/mcoder2014/home_server/config"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"gorm.io/gorm"
)

// runtimeService 取得已注入的动态配置服务；服务尚未就绪时直接写出依赖错误，调用方必须停止处理。
func runtimeService(c *gin.Context) *siteconfig.Service {
	configServiceLock.RLock()
	service := configService
	configServiceLock.RUnlock()
	if service == nil {
		respond(c, nil, apperrors.ErrDependency)
	}
	return service
}

// configurationSchema 处理 GET /api/admin/config/schema：返回配置分组的字段定义、校验规则与部署上传硬上限，供管理表单使用。
func configurationSchema(c *gin.Context) {
	conf := config.Global()
	respond(c, map[string]interface{}{"namespaces": config.Registry(conf), "upload_hard_limit_bytes": conf.UploadHardLimitBytes}, nil)
}

// configurationStatus 处理 GET /api/admin/config/status：展示当前实例加载的配置版本及刷新状态，用于区分保存成功与运行生效。
func configurationStatus(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	respond(c, service.Status(ginfmt.RPCContext(c)), nil)
}

// configurationList 处理 GET /api/admin/config：列出可管理配置分组的当前值和版本。
func configurationList(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	result, err := service.List(ginfmt.RPCContext(c))
	respond(c, result, err)
}

// configurationGet 处理 GET /api/admin/config/:namespace：读取单个配置分组，供管理员编辑时携带准确版本。
func configurationGet(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	result, err := service.Get(ginfmt.RPCContext(c), c.Param("namespace"))
	respond(c, result, err)
}

// configurationValidate 处理 POST /api/admin/config/:namespace/validate：校验有界 JSON 中的候选值并返回差异，不发布配置。
func configurationValidate(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	var input struct {
		Values map[string]interface{} `json:"values"`
	}
	if !bind(c, &input, 64<<10) {
		return
	}
	result, err := service.Validate(ginfmt.RPCContext(c), c.Param("namespace"), input.Values)
	respond(c, result, err)
}

// configAuthorization 先验证管理员当前密码，再返回供配置写事务调用的授权复核函数。
// 事务内重查角色、会话版本和密码哈希，防止验密后撤权或改密仍能提交配置。
func configAuthorization(c *gin.Context, password string) (func(*gorm.DB) error, error) {
	actor := currentUser(c)
	verified, err := accountservice.VerifyAdminPassword(ginfmt.RPCContext(c), actor.ID, password)
	if err != nil {
		return nil, err
	}
	return func(tx *gorm.DB) error {
		current, err := accountservice.RequireUserTx(tx, actor.ID, actor.AuthVersion, true)
		if err != nil {
			return err
		}
		if current.PasswordHash != verified.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		return nil
	}, nil
}

// configurationPublish 处理 PUT /api/admin/config/:namespace：解析版本、幂等发布内容及管理员密码，发布后返回保存和加载结果。
func configurationPublish(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	var input struct {
		siteconfig.PublishRequest
		CurrentPassword string `json:"current_password"`
	}
	if !bind(c, &input, 64<<10) {
		return
	}
	authorize, err := configAuthorization(c, input.CurrentPassword)
	if err != nil {
		respond(c, nil, err)
		return
	}
	result, err := service.Publish(ginfmt.RPCContext(c), c.Param("namespace"), currentUser(c).ID, rev, input.PublishRequest, authorize)
	respond(c, result, err)
}

// configurationRollback 处理 POST /api/admin/config/:namespace/rollback：经过版本与管理员校验，以历史值创建新配置版本而不改写历史。
func configurationRollback(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	rev, ok := revision(c)
	if !ok {
		return
	}
	var input struct {
		siteconfig.RollbackRequest
		CurrentPassword string `json:"current_password"`
	}
	if !bind(c, &input, 16<<10) {
		return
	}
	authorize, err := configAuthorization(c, input.CurrentPassword)
	if err != nil {
		respond(c, nil, err)
		return
	}
	result, err := service.Rollback(ginfmt.RPCContext(c), c.Param("namespace"), currentUser(c).ID, rev, input.RollbackRequest, authorize)
	respond(c, result, err)
}

// configurationHistory 处理 GET /api/admin/config/:namespace/history：按游标返回该分组的发布记录和历史版本。
func configurationHistory(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	cursor, limit, ok := pagination(c)
	if !ok {
		return
	}
	result, err := service.History(ginfmt.RPCContext(c), c.Param("namespace"), cursor, limit)
	respond(c, result, err)
}
