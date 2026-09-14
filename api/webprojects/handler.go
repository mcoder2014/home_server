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

// listProjects 处理 GET /api/web-share（兼容 /api/web-projects）：按游标及状态列出本人托管网页。
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

// createProject 处理 POST /api/web-share 及兼容入口：解析新网页属性，携带完整操作者身份创建本人项目。
func createProject(c *gin.Context) {
	var input service.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	project, err := application.Default.CreateProject(currentUserID(c), input, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, project)
}

// getProject 处理 GET /api/web-share/:id 及兼容入口：读取本人网页详情，不因网页公开而开放匿名管理。
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

// updateProject 处理 PATCH /api/web-share/:id 及兼容入口：按项目版本更新属性、可见性和成员，并保留提交时的身份复核。
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
	project, err := application.Default.UpdateProject(currentUserID(c), projectID, revision, input, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

// disableProject 处理 POST /api/web-share/:id/disable 及兼容入口：将本人网页停用，使普通内容访问停止。
func disableProject(c *gin.Context) { changeStatus(c, "disable") }
// deleteProject 处理 DELETE /api/web-share/:id 及兼容入口：软删除本人网页并进入配置规定的保留期。
func deleteProject(c *gin.Context)  { changeStatus(c, "delete") }
// restoreProject 处理 POST /api/web-share/:id/restore 及兼容入口：在可恢复期限内恢复本人网页，服务层仍检查管理员审核锁。
func restoreProject(c *gin.Context) { changeStatus(c, "restore") }

// changeStatus 统一承接网页停用、删除和恢复动作，解析项目/版本并把当前保留策略与操作者身份传入业务层。
func changeStatus(c *gin.Context, action string) {
	projectID, revision, err := projectAndRevision(c)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	project, err := application.Default.ChangeProjectStatus(currentUserID(c), projectID, revision, action, config.Runtime().WebProjects.DeleteRetentionDays, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

// eligibleUsers 处理 GET /api/web-share/eligible-users 及兼容入口：返回配置网页指定成员时可选择的有效用户。
func eligibleUsers(c *gin.Context) {
	items, err := application.Default.EligibleUsers()
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, gin.H{"items": items})
}

// uploadRelease 处理 POST /api/web-share/:id/releases 及兼容入口：校验所有者和上传准入，再接收文件并创建版本。
// 请求临时文件与并发名额在所有返回路径释放；最终写入继续使用原请求 Principal。
func uploadRelease(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	conf := config.Runtime().WebProjects
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
	release, err := application.Default.UploadRelease(&conf, currentUserID(c), projectID, fileName, entryFile, c.GetHeader("Idempotency-Key"), file, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusCreated, release)
}

// listReleases 处理 GET /api/web-share/:id/releases 及兼容入口：按项目归属、游标和条数列出历史版本。
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

// publishRelease 处理 POST /api/web-share/:id/publish 及兼容入口：按项目版本选择并发布已有版本，使用当前策略和原身份复核提交。
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
	conf := config.Runtime().WebProjects
	project, err := application.Default.PublishRelease(&conf, currentUserID(c), projectID, releaseID, revision, currentPrincipal(c))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	ginfmt.Success(c, http.StatusOK, project)
}

// currentUserID 读取认证链已设置的用户 ID，不从网页请求体取得所有者。
func currentUserID(c *gin.Context) int64 {
	value, _ := c.Get(utils.CtxKeyLoginUseID)
	userID, _ := value.(int64)
	return userID
}

// projectAndRevision 为网页更新动作共同校验路径中的项目 ID 与 If-Match 版本，避免无版本覆盖。
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

// receiveMultipartUpload 将有界 multipart 请求中的单个上传文件流式写到该用户的暂存目录。
// 只接受 file 与 entry_file 两类字段；失败自动删临时文件，成功交由调用者处理并清理。
func receiveMultipartUpload(c *gin.Context, conf *config.WebProjectsConfig) (string, string, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, conf.MaxUploadBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return "", "", "", service.ErrInvalid
	}
	stagingRoot, err := service.UserStagingRoot(conf, currentUserID(c))
	if err != nil {
		return "", "", "", service.ErrDependency
	}
	temp, err := os.CreateTemp(stagingRoot, "request-*.upload")
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

// currentPrincipal 取得网页请求的完整认证快照，保留应用版本、权限和过期时间以供最终写事务复核。
func currentPrincipal(c *gin.Context) *utils.Principal {
	value, ok := c.Get(utils.CtxKeyPrincipal)
	if !ok {
		return nil
	}
	principal, ok := value.(*utils.Principal)
	if !ok || principal == nil {
		return nil
	}
	return principal
}
