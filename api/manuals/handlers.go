package manuals

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	application "github.com/mcoder2014/home_server/app/manuals"
	"github.com/mcoder2014/home_server/config"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func listManuals(c *gin.Context) {
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	mine, err := queryBool(c, "mine")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if mine && principal == nil {
		ginfmt.Fail(c, apperrors.ErrUnauthorized)
		return
	}
	cursor, limit, err := pagination(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	viewerID := principalUserID(principal)
	page, err := application.Default.List(ginfmt.RPCContext(c), viewerID, mine, cursor, limit, c.Query("q"), c.Query("category"), c.Request.URL.Query().Has("category"), principal)
	respond(c, http.StatusOK, page, err)
}

func listCategories(c *gin.Context) {
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	mine, err := queryBool(c, "mine")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if mine && principal == nil {
		ginfmt.Fail(c, apperrors.ErrUnauthorized)
		return
	}
	cursor, limit, err := categoryPagination(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	page, err := application.Default.Categories(ginfmt.RPCContext(c), principalUserID(principal), mine, cursor, limit)
	respond(c, http.StatusOK, page, err)
}

func createManual(c *gin.Context) {
	var input service.CreateManualInput
	if !bindJSON(c, &input, 32<<10) {
		return
	}
	principal := currentPrincipal(c)
	manual, err := application.Default.Create(ginfmt.RPCContext(c), principal.UserID, input, principal)
	respond(c, http.StatusCreated, manual, err)
}

func getManual(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	manual, err := application.Default.Get(ginfmt.RPCContext(c), manualID, principalUserID(principal), principal)
	respond(c, http.StatusOK, manual, err)
}

func updateManual(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input service.UpdateManualInput
	if !bindJSON(c, &input, 128<<10) {
		return
	}
	principal := currentPrincipal(c)
	manual, err := application.Default.Update(ginfmt.RPCContext(c), principal.UserID, manualID, input, principal)
	respond(c, http.StatusOK, manual, err)
}

func deleteManual(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	input := struct {
		Revision int64 `json:"revision"`
	}{}
	if !bindJSON(c, &input, 4096) {
		return
	}
	principal := currentPrincipal(c)
	revision, err := application.Default.Delete(ginfmt.RPCContext(c), principal.UserID, manualID, input.Revision, principal)
	respond(c, http.StatusOK, gin.H{"id": strconv.FormatInt(manualID, 10), "revision": revision}, err)
}

func addInlineItem(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input service.InlineItemInput
	if !bindJSON(c, &input, 512<<10) {
		return
	}
	principal := currentPrincipal(c)
	result, err := application.Default.AddInline(ginfmt.RPCContext(c), principal.UserID, manualID, input, principal)
	respond(c, http.StatusCreated, result, err)
}

func addFileItem(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	principal := currentPrincipal(c)
	if err := application.Default.CheckUploadOwner(ginfmt.RPCContext(c), principal.UserID, manualID); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	conf := config.Global().Manuals
	release, err := application.Default.AcquireUpload(&conf)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer release()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, conf.MaxFileBytes+(2<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		ginfmt.Fail(c, classifyMultipartError(err))
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	title, requestID, header, err := multipartFields(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	file, err := header.Open()
	if err != nil {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return
	}
	defer file.Close()
	result, err := application.Default.AddReservedFile(ginfmt.RPCContext(c), principal.UserID, manualID, title, requestID, header.Filename, file, principal)
	respond(c, http.StatusCreated, result, err)
}

func deleteItem(c *gin.Context) {
	manualID, err := pathID(c, "id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	itemID, err := pathID(c, "item_id")
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	input := struct {
		Revision int64 `json:"revision"`
	}{}
	if !bindJSON(c, &input, 4096) {
		return
	}
	principal := currentPrincipal(c)
	result, err := application.Default.DeleteItem(ginfmt.RPCContext(c), principal.UserID, manualID, itemID, input.Revision, principal)
	respond(c, http.StatusOK, result, err)
}

func bindJSON(c *gin.Context, value interface{}, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginfmt.Fail(c, apperrors.ErrInvalid)
		return false
	}
	return true
}

func respond(c *gin.Context, status int, value interface{}, err error) {
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, status, value)
}

func currentPrincipal(c *gin.Context) *utils.Principal {
	value, _ := c.Get(utils.CtxKeyPrincipal)
	principal, _ := value.(*utils.Principal)
	return principal
}

func principalUserID(principal *utils.Principal) int64 {
	if principal == nil {
		return 0
	}
	return principal.UserID
}

func pathID(c *gin.Context, name string) (int64, error) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 || strconv.FormatInt(value, 10) != c.Param(name) {
		return 0, apperrors.ErrInvalid
	}
	return value, nil
}

func pagination(c *gin.Context) (int64, int, error) {
	cursor, limit := int64(0), 20
	var err error
	if raw := c.Query("cursor"); raw != "" {
		cursor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor <= 0 {
			return 0, 0, apperrors.ErrInvalid
		}
	}
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, apperrors.ErrInvalid
		}
	}
	return cursor, limit, nil
}

func categoryPagination(c *gin.Context) (string, int, error) {
	cursor, limit := c.Query("cursor"), 200
	if cursor != "" {
		categories, err := service.ValidateCategories([]string{cursor})
		if err != nil || len(categories) != 1 || categories[0] != cursor {
			return "", 0, apperrors.ErrInvalid
		}
	}
	if raw := c.Query("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return "", 0, apperrors.ErrInvalid
		}
	}
	return cursor, limit, nil
}

func queryBool(c *gin.Context, name string) (bool, error) {
	value := c.Query(name)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, apperrors.ErrInvalid
	}
	return parsed, nil
}

func optionalReader(c *gin.Context) (*utils.Principal, error) {
	explicit := len(c.Request.Header.Values("Authorization")) > 0 || len(c.Request.Header.Values(middleware.HeaderKey)) > 0
	hasSession := utils.BrowserSessionToken(c.Request) != ""
	if !explicit && !hasSession {
		return nil, nil
	}
	principal, err := middleware.ResolveIdentity(c, "manuals:read", true, false)
	if err == nil {
		return principal, nil
	}
	if errors.Is(err, apperrors.ErrDependency) || explicit {
		return nil, err
	}
	return nil, nil
}
