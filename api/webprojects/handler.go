package webprojects

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/service/passport"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func requireManagementLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(middleware.HeaderKey)
		user, err := passport.CheckToken(ginfmt.RPCContext(c), token)
		if err != nil && service.IsDependencyError(err) {
			failWithError(c, service.ErrDependency)
			c.Abort()
			return
		}
		if err != nil || user == nil {
			fail(c, http.StatusUnauthorized, codeUnauthorized, "authentication required")
			c.Abort()
			return
		}
		c.Set(utils.CtxKeyLoginUseID, user.ID)
		c.Set(utils.CtxKeyLoginToken, token)
		c.Next()
	}
}

func listProjects(c *gin.Context) {
	cursor, err := optionalID(c.Query("cursor"))
	if err != nil { failWithError(c, err); return }
	limit, err := optionalLimit(c.Query("limit"))
	if err != nil { failWithError(c, err); return }
	page, err := service.ListOwnedProjects(currentUserID(c), cursor, limit, c.Query("status"))
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, page)
}

func createProject(c *gin.Context) {
	var input service.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil { failWithError(c, service.ErrInvalid); return }
	project, err := service.CreateProject(currentUserID(c), input)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusCreated, project)
}

func getProject(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil { failWithError(c, err); return }
	project, err := service.GetOwnedProject(currentUserID(c), projectID)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, project)
}

func updateProject(c *gin.Context) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil { failWithError(c, err); return }
	var input service.UpdateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil { failWithError(c, service.ErrInvalid); return }
	project, err := service.UpdateProject(currentUserID(c), projectID, revision, input)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, project)
}

func disableProject(c *gin.Context) { changeStatus(c, "disable") }
func deleteProject(c *gin.Context) { changeStatus(c, "delete") }
func restoreProject(c *gin.Context) { changeStatus(c, "restore") }

func changeStatus(c *gin.Context, action string) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil { failWithError(c, err); return }
	project, err := service.ChangeProjectStatus(currentUserID(c), projectID, revision, action, config.Global().WebProjects.DeleteRetentionDays)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, project)
}

func eligibleUsers(c *gin.Context) { success(c, http.StatusOK, gin.H{"items": service.EligibleUsers()}) }

func uploadRelease(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil { failWithError(c, err); return }
	conf := config.Global().WebProjects
	if err := service.CheckUploadOwner(currentUserID(c), projectID); err != nil { failWithError(c, err); return }
	releaseUpload, err := service.AcquireUpload(currentUserID(c), &conf)
	if err != nil { failWithError(c, err); return }
	defer releaseUpload()
	tempPath, fileName, entryFile, err := receiveMultipartUpload(c, &conf)
	if err != nil { failWithError(c, err); return }
	defer os.Remove(tempPath)
	file, err := os.Open(tempPath)
	if err != nil { failWithError(c, service.ErrDependency); return }
	defer file.Close()
	release, err := service.UploadRelease(&conf, currentUserID(c), projectID, fileName, entryFile, c.GetHeader("Idempotency-Key"), file)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusCreated, release)
}

func listReleases(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id")); if err != nil { failWithError(c, err); return }
	cursor, err := optionalID(c.Query("cursor")); if err != nil { failWithError(c, err); return }
	limit, err := optionalLimit(c.Query("limit")); if err != nil { failWithError(c, err); return }
	page, err := service.ListReleases(currentUserID(c), projectID, cursor, limit)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, page)
}

func publishRelease(c *gin.Context) {
	projectID, revision, err := projectAndRevision(c); if err != nil { failWithError(c, err); return }
	var input struct { ReleaseID string `json:"release_id"` }
	if err := c.ShouldBindJSON(&input); err != nil { failWithError(c, service.ErrInvalid); return }
	releaseID, err := service.ParsePositiveID(input.ReleaseID); if err != nil { failWithError(c, err); return }
	conf := config.Global().WebProjects
	project, err := service.PublishRelease(&conf, currentUserID(c), projectID, releaseID, revision)
	if err != nil { failWithError(c, err); return }
	success(c, http.StatusOK, project)
}

func browserLogin(c *gin.Context) {
	conf := config.Global().WebProjects
	if c.GetHeader("Origin") != conf.SiteOrigin {
		fail(c, http.StatusForbidden, codeForbidden, "origin is not allowed")
		return
	}
	token := c.GetHeader(middleware.HeaderKey)
	tokenEntity, err := dal.QueryByToken(token)
	if err != nil { failWithError(c, service.ErrDependency); return }
	if tokenEntity == nil || tokenEntity.IsExpired != 0 || !tokenEntity.ExpireTime.After(time.Now()) {
		fail(c, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	user, _ := passport.GetMockData().GetByID(tokenEntity.UserID)
	if user == nil { fail(c, http.StatusUnauthorized, codeUnauthorized, "authentication required"); return }
	maxAge := int(time.Until(tokenEntity.ExpireTime).Seconds())
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-web_projects_session", Value: token, Path: "/", MaxAge: maxAge, Expires: tokenEntity.ExpireTime, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	success(c, http.StatusOK, gin.H{"user_id": strconv.FormatInt(user.ID, 10), "user_name": user.UserName, "expire_time": tokenEntity.ExpireTime})
}

func currentUserID(c *gin.Context) int64 { value, _ := c.Get(utils.CtxKeyLoginUseID); userID, _ := value.(int64); return userID }

func projectAndRevision(c *gin.Context) (int64, int64, error) {
	projectID, err := service.ParsePositiveID(c.Param("id")); if err != nil { return 0, 0, err }
	revision, err := parseIfMatch(c.GetHeader("If-Match")); if err != nil { return 0, 0, service.ErrInvalid }
	return projectID, revision, nil
}

func optionalID(value string) (int64, error) { if value == "" { return 0, nil }; return service.ParsePositiveID(value) }
func optionalLimit(value string) (int, error) { if value == "" { return 20, nil }; result, err := strconv.Atoi(value); if err != nil || result <= 0 { return 0, service.ErrInvalid }; return result, nil }

func receiveMultipartUpload(c *gin.Context, conf *config.WebProjectsConfig) (string, string, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, conf.MaxUploadBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil { return "", "", "", service.ErrInvalid }
	temp, err := os.CreateTemp(filepath.Join(conf.StorageRoot, "staging"), "request-*.upload")
	if err != nil { return "", "", "", service.ErrDependency }
	tempPath := temp.Name(); fileName, entryFile := "", ""
	keep := false
	defer func() { _ = temp.Close(); if !keep { _ = os.Remove(tempPath) } }()
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF { break }
		if nextErr != nil { return "", "", "", service.ErrInvalid }
		switch part.FormName() {
		case "file":
			if fileName != "" || part.FileName() == "" { part.Close(); return "", "", "", service.ErrInvalid }
			fileName = filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			written, copyErr := io.Copy(temp, io.LimitReader(part, conf.MaxUploadBytes+1)); part.Close()
			if copyErr != nil { return "", "", "", service.ErrDependency }
			if written > conf.MaxUploadBytes { return "", "", "", service.ErrTooLarge }
		case "entry_file":
			value, readErr := io.ReadAll(io.LimitReader(part, 1025)); part.Close()
			if readErr != nil || len(value) > 1024 { return "", "", "", service.ErrInvalid }
			entryFile = string(value)
		default:
			part.Close()
			return "", "", "", service.ErrInvalid
		}
	}
	if fileName == "" { return "", "", "", service.ErrInvalid }
	if err := temp.Close(); err != nil { return "", "", "", fmt.Errorf("%w: close upload", service.ErrDependency) }
	keep = true
	return tempPath, fileName, entryFile, nil
}
