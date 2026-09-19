package fileshare

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	application "github.com/mcoder2014/home_server/app/fileshare"
	"github.com/mcoder2014/home_server/config"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
	"github.com/sirupsen/logrus"
)

func listFiles(c *gin.Context) {
	cursor, limit, err := service.ParsePagination(c.Query("cursor"), c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.List(ginfmt.RPCContext(c), currentPrincipal(c).UserID, cursor, limit)
	respond(c, http.StatusOK, result, err)
}

func uploadFile(c *gin.Context) {
	conf := config.Global().FileSharing
	principal := currentPrincipal(c)
	release, err := application.Default.AcquireUpload(conf, principal.UserID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer release()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, conf.MaxFileBytes+(2<<20))
	mediaType, parameters, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	reader := multipart.NewReader(c.Request.Body, parameters["boundary"])
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	defer part.Close()
	result, err := application.Default.UploadReserved(ginfmt.RPCContext(c), principal, part.FileName(), part, func() error {
		next, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			return nil
		}
		if next != nil {
			_ = next.Close()
		}
		var maxBytesError *http.MaxBytesError
		if errors.As(nextErr, &maxBytesError) {
			return service.ErrTooLarge
		}
		return service.ErrInvalid
	})
	respond(c, http.StatusCreated, result, err)
}

func eligibleUsers(c *gin.Context) {
	items, err := application.Default.EligibleUsers(ginfmt.RPCContext(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, gin.H{"items": items})
}

func getFile(c *gin.Context) {
	id, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.Get(ginfmt.RPCContext(c), currentPrincipal(c).UserID, id)
	respond(c, http.StatusOK, result, err)
}

func deleteFile(c *gin.Context) {
	id, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.Delete(ginfmt.RPCContext(c), currentPrincipal(c), id)
	respond(c, http.StatusOK, result, err)
}

func listShares(c *gin.Context) {
	fileID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	cursor, limit, err := service.ParsePagination(c.Query("cursor"), c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.ListShares(ginfmt.RPCContext(c), currentPrincipal(c).UserID, fileID, cursor, limit)
	respond(c, http.StatusOK, result, err)
}

func createShare(c *gin.Context) {
	fileID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input service.CreateShareInput
	if !bindJSON(c, &input, 16<<10) {
		return
	}
	result, err := application.Default.CreateShare(ginfmt.RPCContext(c), currentPrincipal(c), fileID, input)
	respond(c, http.StatusCreated, result, err)
}

func revokeShare(c *gin.Context) {
	fileID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	shareID, err := service.ParsePositiveID(c.Param("share_id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.RevokeShare(ginfmt.RPCContext(c), currentPrincipal(c), fileID, shareID)
	respond(c, http.StatusOK, result, err)
}

func getPublicShare(c *gin.Context) {
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	result, err := application.Default.PublicState(ginfmt.RPCContext(c), c.Param("token"), principal, c.Request)
	respond(c, http.StatusOK, result, err)
}

func unlockPublicShare(c *gin.Context) {
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if !browserPublicWrite(c) {
		return
	}
	input := struct {
		Secret string `json:"secret"`
	}{}
	if !bindJSON(c, &input, 1024) {
		return
	}
	result, err := application.Default.Unlock(ginfmt.RPCContext(c), c.Param("token"), input.Secret, middleware.TrustedClientIP(c.Request), principal)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if result.CookieName != "" {
		http.SetCookie(c.Writer, &http.Cookie{Name: result.CookieName, Value: result.CookieValue, Path: "/api/file-shares/" + c.Param("token"), MaxAge: maxAge(result.UnlockedUntil), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	ginfmt.Success(c, http.StatusOK, result)
}

func downloadPublicShare(c *gin.Context) {
	if c.GetHeader("Range") != "" || c.GetHeader("Content-Range") != "" {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	principal, err := optionalReader(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if !browserPublicWrite(c) {
		return
	}
	download, err := application.Default.PrepareDownload(ginfmt.RPCContext(c), c.Param("token"), principal, c.Request)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer download.File.Close()
	c.Header("Content-Type", "application/octet-stream")
	if value := mime.FormatMediaType("attachment", map[string]string{"filename": download.Filename}); value != "" {
		c.Header("Content-Disposition", value)
	}
	c.Header("Content-Length", strconv.FormatInt(download.Info.Size(), 10))
	c.Header("Cache-Control", "no-store")
	c.Header("Accept-Ranges", "none")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if _, err := io.CopyN(c.Writer, download.File, download.Info.Size()); err != nil {
		logrus.WithField("share_download", "interrupted").Warn("file share transfer ended before all bytes were sent")
	}
}

func optionalReader(c *gin.Context) (*utils.Principal, error) {
	explicit := len(c.Request.Header.Values("Authorization")) > 0 || len(c.Request.Header.Values(middleware.HeaderKey)) > 0
	hasSession := utils.BrowserSessionToken(c.Request) != ""
	if !explicit && !hasSession {
		return nil, nil
	}
	principal, err := middleware.ResolveIdentity(c, "files:read", true, false)
	if err == nil {
		return principal, nil
	}
	if explicit || hasSession || errors.Is(err, service.ErrDependency) {
		return nil, err
	}
	return nil, nil
}

func browserPublicWrite(c *gin.Context) bool {
	if c.GetHeader("Authorization") != "" || c.GetHeader(middleware.HeaderKey) != "" {
		return true
	}
	middleware.BrowserWrite()(c)
	return !c.IsAborted()
}

func currentPrincipal(c *gin.Context) *utils.Principal {
	value, _ := c.Get(utils.CtxKeyPrincipal)
	principal, _ := value.(*utils.Principal)
	return principal
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

func maxAge(expiresAt time.Time) int {
	seconds := int(time.Until(expiresAt).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
