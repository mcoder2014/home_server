package webprojects

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	application "github.com/mcoder2014/home_server/app/webprojects"
	"github.com/mcoder2014/home_server/config"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func listProjects(c *gin.Context) {
	cursor, err := optionalID(c.Query("cursor"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	limit, err := optionalLimit(c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	page, err := application.Default.ListOwnedProjects(currentUserID(c), cursor, limit, c.Query("status"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, page)
}

func createProject(c *gin.Context) {
	var input service.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	project, err := application.Default.CreateProject(currentUserID(c), input)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, project)
}

func getProject(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	project, err := application.Default.GetOwnedProject(currentUserID(c), projectID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

func updateProject(c *gin.Context) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input service.UpdateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	project, err := application.Default.UpdateProject(currentUserID(c), projectID, revision, input)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

func disableProject(c *gin.Context) { changeStatus(c, "disable") }
func deleteProject(c *gin.Context)  { changeStatus(c, "delete") }
func restoreProject(c *gin.Context) { changeStatus(c, "restore") }

func changeStatus(c *gin.Context, action string) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	project, err := application.Default.ChangeProjectStatus(currentUserID(c), projectID, revision, action, config.Global().WebProjects.DeleteRetentionDays)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

func eligibleUsers(c *gin.Context) {
	ginfmt.Success(c, http.StatusOK, gin.H{"items": application.Default.EligibleUsers()})
}

func uploadRelease(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	conf := config.Global().WebProjects
	if err := application.Default.CheckUploadOwner(currentUserID(c), projectID); err != nil {
		ginfmt.Fail(c, err)
		return
	}
	releaseUpload, err := application.Default.AcquireUpload(currentUserID(c), &conf)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer releaseUpload()
	tempPath, fileName, entryFile, err := receiveMultipartUpload(c, &conf)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	defer os.Remove(tempPath)
	file, err := os.Open(tempPath)
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	defer file.Close()
	release, err := application.Default.UploadRelease(&conf, currentUserID(c), projectID, fileName, entryFile, c.GetHeader("Idempotency-Key"), file)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, release)
}

func listReleases(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	cursor, err := optionalID(c.Query("cursor"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	limit, err := optionalLimit(c.Query("limit"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	page, err := application.Default.ListReleases(currentUserID(c), projectID, cursor, limit)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, page)
}

func publishRelease(c *gin.Context) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	var input struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	releaseID, err := service.ParsePositiveID(input.ReleaseID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	conf := config.Global().WebProjects
	project, err := application.Default.PublishRelease(&conf, currentUserID(c), projectID, releaseID, revision)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

func currentUserID(c *gin.Context) int64 {
	value, _ := c.Get(utils.CtxKeyLoginUseID)
	userID, _ := value.(int64)
	return userID
}

func projectAndRevision(c *gin.Context) (int64, int64, error) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		return 0, 0, err
	}
	revision, err := parseIfMatch(c.GetHeader("If-Match"))
	if err != nil {
		return 0, 0, service.ErrInvalid
	}
	return projectID, revision, nil
}

func optionalID(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return service.ParsePositiveID(value)
}
func optionalLimit(value string) (int, error) {
	if value == "" {
		return 20, nil
	}
	result, err := strconv.Atoi(value)
	if err != nil || result <= 0 {
		return 0, service.ErrInvalid
	}
	return result, nil
}

func receiveMultipartUpload(c *gin.Context, conf *config.WebProjectsConfig) (string, string, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, conf.MaxUploadBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return "", "", "", service.ErrInvalid
	}
	temp, err := os.CreateTemp(filepath.Join(conf.StorageRoot, "staging"), "request-*.upload")
	if err != nil {
		return "", "", "", service.ErrDependency
	}
	tempPath := temp.Name()
	fileName, entryFile := "", ""
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", "", "", service.ErrInvalid
		}
		switch part.FormName() {
		case "file":
			if fileName != "" || part.FileName() == "" {
				part.Close()
				return "", "", "", service.ErrInvalid
			}
			fileName = filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			written, copyErr := io.Copy(temp, io.LimitReader(part, conf.MaxUploadBytes+1))
			part.Close()
			if copyErr != nil {
				return "", "", "", service.ErrDependency
			}
			if written > conf.MaxUploadBytes {
				return "", "", "", service.ErrTooLarge
			}
		case "entry_file":
			value, readErr := io.ReadAll(io.LimitReader(part, 2049))
			part.Close()
			if readErr != nil || len(value) > 2048 {
				return "", "", "", service.ErrInvalid
			}
			entryFile = string(value)
		default:
			part.Close()
			return "", "", "", service.ErrInvalid
		}
	}
	if fileName == "" {
		return "", "", "", service.ErrInvalid
	}
	if err := temp.Close(); err != nil {
		return "", "", "", fmt.Errorf("%w: close upload", service.ErrDependency)
	}
	keep = true
	return tempPath, fileName, entryFile, nil
}
