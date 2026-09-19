package manuals

import (
	"errors"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"
	apperrors "github.com/mcoder2014/home_server/errors"
)

func multipartFields(c *gin.Context) (string, string, *multipart.FileHeader, error) {
	form := c.Request.MultipartForm
	if form == nil || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return "", "", nil, apperrors.ErrInvalid
	}
	for name := range form.File {
		if name != "file" {
			return "", "", nil, apperrors.ErrInvalid
		}
	}
	for name, values := range form.Value {
		if (name != "title" && name != "client_request_id") || len(values) != 1 {
			return "", "", nil, apperrors.ErrInvalid
		}
	}
	if len(form.Value["client_request_id"]) != 1 || len(form.Value["title"]) > 1 {
		return "", "", nil, apperrors.ErrInvalid
	}
	title := ""
	if len(form.Value["title"]) == 1 {
		title = form.Value["title"][0]
	}
	return title, form.Value["client_request_id"][0], form.File["file"][0], nil
}

func classifyMultipartError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return apperrors.ErrTooLarge
	}
	return apperrors.ErrInvalid
}
