package accounts

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func updateAvatar(c *gin.Context) {
	rev, ok := revision(c)
	if !ok {
		return
	}
	remove := c.Request.Method == http.MethodDelete
	var raw []byte
	if !remove {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, accounts.AvatarInputLimit+(64<<10))
		reader, err := c.Request.MultipartReader()
		if err != nil {
			respond(c, nil, apperrors.ErrInvalid)
			return
		}
		for {
			part, e := reader.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil || raw != nil || part.FormName() != "file" || part.FileName() == "" {
				respond(c, nil, apperrors.ErrInvalid)
				return
			}
			raw, e = io.ReadAll(io.LimitReader(part, accounts.AvatarInputLimit+1))
			part.Close()
			if e != nil || len(raw) == 0 || len(raw) > accounts.AvatarInputLimit {
				respond(c, nil, apperrors.ErrInvalid)
				return
			}
		}
		if raw == nil {
			respond(c, nil, apperrors.ErrInvalid)
			return
		}
	}
	updated, err := accounts.UpdateAvatar(ginfmt.RPCContext(c), c.GetString(utils.CtxKeyLoginToken), rev, raw, remove)
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, view(updated, c.GetString(utils.CtxKeyLoginToken)), nil)
}

func readAvatar(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	id, ok := positiveID(c, "user_id")
	if !ok {
		return
	}
	version, ok := positiveID(c, "version")
	if !ok {
		return
	}
	raw, err := accounts.ReadAvatar(ginfmt.RPCContext(c), id, version)
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.Data(http.StatusOK, "image/jpeg", raw)
}
