package homevirtualsub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	accountservice "github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	virtual "github.com/mcoder2014/home_server/rpc/homevirtualsub"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/mcoder2014/home_server/utils/log"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func read(c *gin.Context, client *virtual.Client, view string) {
	if client == nil {
		if view == "status" {
			ginfmt.Success(c, http.StatusOK, virtual.Status{Workers: []virtual.Worker{}})
		} else {
			ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrNotFound, "home virtual sub 管理尚未启用"))
		}
		return
	}
	var result interface{}
	var err error
	switch view {
	case "status":
		result, err = client.Status(c.Request.Context())
	case "subscriptions":
		result, err = client.List(c.Request.Context())
	case "subscriptions/detail":
		result, err = client.Detail(c.Request.Context(), c.Query("name"))
	}
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, result)
}

type mutationInput struct {
	virtual.SubscriptionInput
	Reason          string `json:"reason"`
	CurrentPassword string `json:"current_password"`
}

// mutate confirms the administrator and records intent before contacting the
// service. No database transaction spans the network call. Service results and
// audit failures are reported independently; an external change is never
// described as rolled back because local audit finalization failed.
func mutate(c *gin.Context, client *virtual.Client, action string) {
	if client == nil {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrNotFound, "home virtual sub 管理尚未启用"))
		return
	}
	revision := c.GetHeader("If-Match")
	if strings.HasPrefix(revision, `"`) && strings.HasSuffix(revision, `"`) {
		revision = strings.TrimSuffix(strings.TrimPrefix(revision, `"`), `"`)
	}
	if len(c.Request.Header.Values("If-Match")) != 1 || !virtual.ValidRevision(revision) {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrInvalid, "请刷新配置并携带当前版本"))
		return
	}
	var input mutationInput
	if err := decodeInput(c, &input); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	name := c.Query("name")
	if action == "create" {
		name = input.Name
	}
	if name == "" || len(name) > 256 || !utf8.ValidString(name) || strings.ContainsAny(name, "\r\n\x00") {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return
	}
	if action != "create" && input.Name != "" && input.Name != name {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrInvalid, "编辑时不能修改订阅名称"))
		return
	}
	if action == "create" && len(input.URLs) == 0 {
		ginfmt.Fail(c, apperrors.WithMessage(apperrors.ErrInvalid, "请填写订阅地址"))
		return
	}
	audit, err := beginAudit(c, action, name, revision, input)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, callErr := client.Mutate(c.Request.Context(), action, c.Query("name"), revision, input.SubscriptionInput)
	// Finalize with a short independent context even if the browser disconnected.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outcome, after := "unknown", "{}"
	if callErr == nil {
		outcome = result.ApplyState
		content, _ := json.Marshal(map[string]interface{}{"name": name, "saved": result.Saved, "revision": result.Revision, "apply_state": result.ApplyState})
		after = string(content)
	} else if errors.Is(callErr, apperrors.ErrInvalid) || errors.Is(callErr, apperrors.ErrConflict) || errors.Is(callErr, apperrors.ErrNotFound) {
		outcome = "rejected"
	}
	finished := db.MasterDB().WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Table(dal.AdminAuditTable).Where("id = ?", audit.ID).Updates(map[string]interface{}{"result": outcome, "after_summary": after})
	if finished.Error != nil || finished.RowsAffected != 1 {
		log.Ctx(ctx).Error("home virtual sub audit result could not be recorded")
	}
	if callErr != nil {
		ginfmt.Fail(c, callErr)
		return
	}
	result.AuditRecorded = finished.Error == nil && finished.RowsAffected == 1
	if !result.AuditRecorded {
		result.Message += "；操作记录结果待核对"
	}
	ginfmt.Success(c, http.StatusOK, result)
}

func decodeInput(c *gin.Context, input *mutationInput) error {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return apperrors.ErrUnsupported
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	var raw map[string]json.RawMessage
	if err = decoder.Decode(&raw); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			return apperrors.ErrTooLarge
		}
		return apperrors.ErrInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return apperrors.ErrInvalid
	}
	if raw == nil {
		return apperrors.ErrInvalid
	}
	// Explicit null must not turn into an omitted field in the service PATCH.
	for _, value := range raw {
		if strings.TrimSpace(string(value)) == "null" {
			return apperrors.ErrInvalid
		}
	}
	content, err := json.Marshal(raw)
	if err != nil {
		return apperrors.ErrInvalid
	}
	fields := json.NewDecoder(bytes.NewReader(content))
	fields.DisallowUnknownFields()
	if fields.Decode(input) != nil || (input.URLs != nil && len(input.URLs) == 0) {
		return apperrors.ErrInvalid
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len(input.Reason) > 512 || !utf8.ValidString(input.Reason) || input.CurrentPassword == "" || len(input.CurrentPassword) > 1024 {
		return apperrors.ErrInvalid
	}
	return nil
}

// beginAudit rechecks the exact acting session and the account password version
// in a short transaction. Intent must be durable before the remote side effect.
func beginAudit(c *gin.Context, action, name, revision string, input mutationInput) (*model.AdminAuditLog, error) {
	value, _ := c.Get(middleware.AccountContextKey)
	actor, ok := value.(*model.UserAccount)
	if !ok || actor == nil {
		return nil, apperrors.ErrUnauthorized
	}
	verified, err := accountservice.VerifyAdminPassword(ginfmt.RPCContext(c), actor.ID, input.CurrentPassword)
	if err != nil {
		return nil, err
	}
	database := db.MasterDB()
	if database == nil {
		return nil, apperrors.ErrDependency
	}
	content, _ := json.Marshal(map[string]interface{}{"name": name, "revision": revision, "http_port": input.HTTPPort, "socket_port": input.SocketPort, "url_count": len(input.URLs)})
	audit := &model.AdminAuditLog{ActorUserID: actor.ID, Action: "home_virtual_sub." + action, TargetType: "home_virtual_sub", BeforeSummary: string(content), AfterSummary: "{}", Reason: input.Reason, Result: "in_progress", RequestID: c.GetString(log.LogIDKey), CreateTime: time.Now()}
	err = database.WithContext(c.Request.Context()).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		fresh, e := accountservice.RequireUserSessionTx(tx, c.GetString(utils.CtxKeyLoginToken), actor.ID)
		if e != nil {
			return e
		}
		if fresh.Role != model.RoleAdmin || fresh.AuthVersion != verified.AuthVersion || fresh.PasswordHash != verified.PasswordHash {
			return apperrors.ErrForbidden
		}
		return tx.Table(dal.AdminAuditTable).Create(audit).Error
	})
	if err != nil {
		return nil, err
	}
	return audit, nil
}
