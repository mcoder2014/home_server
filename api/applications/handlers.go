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

const maxManagementRequestBytes = 16 << 10

func list(c *gin.Context) {
	cursor, err := parseOptionalPositiveInt64(c.Query("cursor"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	limit, err := parseOptionalInt(c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	response, err := applicationApp.List(c.Request.Context(), principal(c), cursor, limit)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

func create(c *gin.Context) {
	var request applicationApp.CreateRequest
	if err := decodeJSON(c, &request); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.Create(c.Request.Context(), principal(c), request)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, response)
}

func get(c *gin.Context) {
	applicationID, err := parsePositiveInt64(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, appErrors.ErrInvalid)
		return
	}
	response, err := applicationApp.Get(c.Request.Context(), principal(c), applicationID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

func update(c *gin.Context) {
	applicationID, revision, err := managementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var request applicationApp.UpdateRequest
	if err := decodeJSON(c, &request); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.Update(c.Request.Context(), principal(c), applicationID, revision, request)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

func rotate(c *gin.Context) {
	applicationID, revision, err := managementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.Rotate(c.Request.Context(), principal(c), applicationID, revision)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

func revoke(c *gin.Context) {
	applicationID, revision, err := managementTarget(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	response, err := applicationApp.Revoke(c.Request.Context(), principal(c), applicationID, revision)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, response)
}

func managementTarget(c *gin.Context) (int64, int64, error) {
	applicationID, err := parsePositiveInt64(c.Param("id"))
	if err != nil {
		return 0, 0, appErrors.ErrInvalid
	}
	revision, err := parseIfMatch(c.GetHeader("If-Match"))
	if err != nil {
		return 0, 0, appErrors.ErrInvalid
	}
	return applicationID, revision, nil
}

func principal(c *gin.Context) *utils.Principal {
	if value, ok := c.Get(utils.CtxKeyPrincipal); ok {
		if principal, ok := value.(*utils.Principal); ok {
			return principal
		}
	}
	principal, _ := c.Request.Context().Value(utils.CtxKeyPrincipal).(*utils.Principal)
	return principal
}

func decodeJSON(c *gin.Context, destination interface{}) error {
	if c.ContentType() != "application/json" {
		return appErrors.ErrUnsupported
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxManagementRequestBytes+1))
	if err != nil || len(body) > maxManagementRequestBytes {
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

func parseIfMatch(value string) (int64, error) {
	if strings.HasPrefix(value, `W/`) || strings.Contains(value, ",") {
		return 0, appErrors.ErrInvalid
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return parsePositiveInt64(value)
}

func parseOptionalPositiveInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return parsePositiveInt64(value)
}

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, appErrors.ErrInvalid
	}
	return parsed, nil
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, appErrors.ErrInvalid
	}
	return parsed, nil
}
