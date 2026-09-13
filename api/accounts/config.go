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

func runtimeService(c *gin.Context) *siteconfig.Service {
	configServiceLock.RLock()
	service := configService
	configServiceLock.RUnlock()
	if service == nil {
		respond(c, nil, apperrors.ErrDependency)
	}
	return service
}

func configurationSchema(c *gin.Context) {
	conf := config.Global()
	respond(c, map[string]interface{}{"namespaces": config.Registry(conf), "upload_hard_limit_bytes": conf.UploadHardLimitBytes}, nil)
}

func configurationStatus(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	respond(c, service.Status(ginfmt.RPCContext(c)), nil)
}

func configurationList(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	result, err := service.List(ginfmt.RPCContext(c))
	respond(c, result, err)
}

func configurationGet(c *gin.Context) {
	service := runtimeService(c)
	if service == nil {
		return
	}
	result, err := service.Get(ginfmt.RPCContext(c), c.Param("namespace"))
	respond(c, result, err)
}

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
