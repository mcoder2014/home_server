package manuals

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	application "github.com/mcoder2014/home_server/app/manuals"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/manuals"
	"github.com/mcoder2014/home_server/domain/service/resourcepasswords"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func serveItemResource(c *gin.Context) {
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
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	thumbnail := strings.HasSuffix(c.FullPath(), "/thumbnail")
	item, err := application.Default.ResourceItem(ginfmt.RPCContext(c), manualID, itemID, principalUserID(principal), thumbnail, resourcepasswords.GrantToken(c.Request, resourcepasswords.ResourceManual, manualID))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	key := item.StorageKey
	if thumbnail {
		key = item.ThumbnailKey
	}
	if key == nil {
		ginfmt.Fail(c, apperrors.ErrNotFound)
		return
	}
	conf := config.Global().Manuals
	file, info, err := service.OpenStoredFile(&conf, *key)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer file.Close()
	setResourceHeaders(c, item, thumbnail)
	http.ServeContent(c.Writer, c.Request, resourceFileName(item, thumbnail), info.ModTime(), file)
}

func setResourceHeaders(c *gin.Context, item *model.ManualItem, thumbnail bool) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	contentType, disposition := item.ContentType, "inline"
	if thumbnail {
		contentType = "image/jpeg"
	} else if c.Query("download") == "1" {
		disposition = "attachment"
	}
	if item.Kind == model.ManualItemPDF {
		c.Header("Content-Security-Policy", "sandbox")
	}
	c.Header("Content-Type", contentType)
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": resourceFileName(item, thumbnail)}); value != "" {
		c.Header("Content-Disposition", value)
	}
}

func resourceFileName(item *model.ManualItem, thumbnail bool) string {
	if thumbnail {
		return "thumbnail.jpg"
	}
	if item.OriginalName != "" {
		return item.OriginalName
	}
	return "manual-item"
}
