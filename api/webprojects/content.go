package webprojects

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/api/middleware"
	"github.com/mcoder2014/home_server/app/acceleration"
	application "github.com/mcoder2014/home_server/app/webprojects"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// serveProjectContent resolves the live project/owner state and reader identity
// before opening a validated published file. Public visibility bypasses reader
// login only; owner bans, moderation, module policy and path checks still apply.
// serveProjectContent 处理 GET/HEAD /p/:slug 及其资源路径，只输出通过发布状态、可见性和安全路径检查的文件。
func serveProjectContent(c *gin.Context) {
	project, release, err := application.Default.GetPublishedProject(c.Param("slug"), ginfmt.RPCContext(c))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrForbidden) {
			contentNotFound(c)
		} else {
			ginfmt.Fail(c, err)
		}
		return
	}
	userID := int64(0)
	explicitCredentials := c.GetHeader("Authorization") != "" || c.GetHeader(middleware.HeaderKey) != ""
	if project.AccessMode != service.AccessModePublic || explicitCredentials {
		principal, loginErr := middleware.ResolveIdentity(c, "web-projects:read", true, false)
		if loginErr == nil {
			userID = principal.UserID
		} else {
			if errors.Is(loginErr, service.ErrDependency) {
				ginfmt.Fail(c, loginErr)
			} else {
				contentNotFound(c)
			}
			return
		}
	}
	isMember := false
	if project.AccessMode == service.AccessModeMembers && userID != project.OwnerUserID {
		isMember, err = application.Default.IsMember(project.ID, userID)
		if err != nil {
			ginfmt.Fail(c, service.ErrDependency)
			return
		}
	}
	if !service.CanReadProject(project.AccessMode, project.OwnerUserID, userID, isMember) {
		contentNotFound(c)
		return
	}
	if c.GetHeader("Service-Worker") != "" {
		ginfmt.Fail(c, service.ErrForbidden)
		return
	}
	if c.Param("path") == "" {
		target := "/p/" + project.Slug + "/"
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusPermanentRedirect, target)
		return
	}
	conf := config.Runtime().WebProjects
	contentRoot, err := service.ReleaseContentRoot(&conf, release)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	requested := strings.TrimPrefix(c.Param("path"), "/")
	if requested == "" {
		requested = release.EntryFile
	}
	isEntry := requested == release.EntryFile
	filePath, err := service.ResolveContentPath(contentRoot, requested)
	if err != nil {
		failContentFile(c, contentFailureStatus(err, isEntry, false))
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		failContentFile(c, contentFailureStatus(err, isEntry, false))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	status := contentFailureStatus(err, isEntry, err == nil && info.Mode().IsRegular())
	if status != 0 {
		failContentFile(c, status)
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filePath)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Issue the independent visitor cookie only after access and file checks.
	// Record after ServeContent so redirects/errors/Range responses are excluded.
	runtime := acceleration.Current.Load()
	visitor := ""
	if runtime != nil && runtime.Analytics != nil && middleware.IsHTTPS(c) && isAnalyticsDocument(c.Request, requested, http.StatusOK) {
		visitor, _ = analyticsVisitor(c)
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("ETag", `"`+strconv.FormatInt(release.ID, 10)+`-`+strconv.FormatInt(info.Size(), 10)+`-`+strconv.FormatInt(info.ModTime().UnixNano(), 10)+`"`)
	if project.ContainerMode == "enhanced" && (strings.EqualFold(filepath.Ext(requested), ".html") || strings.EqualFold(filepath.Ext(requested), ".htm")) {
		if err := serveEnhancedHTML(c, file, info, project, release); err != nil {
			ginfmt.Fail(c, service.ErrDependency)
			return
		}
	} else {
		http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
	}
	if visitor != "" && isAnalyticsDocument(c.Request, requested, c.Writer.Status()) {
		runtime.Analytics.Record(project.ID, runtime.VisitorHash(project.ID, visitor), time.Now())
	}
}

// downloadRelease 处理 GET /api/web-share/:id/releases/:release_id/download 及兼容入口：确认本人版本归属后生成下载包。
func downloadRelease(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	releaseID, err := service.ParsePositiveID(c.Param("release_id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	release, err := application.Default.GetReleaseForDownload(currentUserID(c), projectID, releaseID)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	conf := config.Runtime().WebProjects
	serveReleaseDownload(c, &conf, release)
}

// serveReleaseDownload 先在用户暂存目录生成并校验完整版本 ZIP，再以附件响应客户端；结束时清理临时包，避免返回半成品。
func serveReleaseDownload(c *gin.Context, conf *config.WebProjectsConfig, release *model.WebProjectRelease) {
	if conf == nil || release == nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	contentRoot, err := service.ReleaseContentRoot(conf, release)
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	stagingRoot, err := service.UserStagingRoot(conf, release.UploadedBy)
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	temp, err := os.CreateTemp(stagingRoot, "download-*.zip")
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := writeReleaseArchive(temp, contentRoot, release); err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	info, err := temp.Stat()
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="web-share-%d-release-%d.zip"`, release.ProjectID, release.ID))
	c.Header("Cache-Control", "no-store")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), temp)
}

// writeReleaseArchive writes one complete release ZIP to destination. It rejects
// symbolic links and non-regular files, then verifies file count and byte size
// against release metadata so callers never publish a partial download as valid.
func writeReleaseArchive(destination io.Writer, contentRoot string, release *model.WebProjectRelease) error {
	rootInfo, err := os.Lstat(contentRoot)
	if err != nil {
		return fmt.Errorf("inspect release content root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("release content root is not a regular directory")
	}
	entryPath, err := service.ResolveContentPath(contentRoot, release.EntryFile)
	if err != nil {
		return fmt.Errorf("resolve release entry: %w", err)
	}
	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		return fmt.Errorf("inspect release entry: %w", err)
	}
	if !entryInfo.Mode().IsRegular() {
		return fmt.Errorf("release entry is not a regular file")
	}

	zw := zip.NewWriter(destination)
	fileCount := 0
	var totalBytes int64
	// 逐项拒绝符号链接及特殊文件，把常规文件写入 ZIP，同时累计文件数和实际字节数。
	err = filepath.Walk(contentRoot, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == contentRoot {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("release contains a symbolic link")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release contains a non-regular file")
		}
		relative, relErr := filepath.Rel(contentRoot, filePath)
		if relErr != nil {
			return relErr
		}
		header, headerErr := zip.FileInfoHeader(info)
		if headerErr != nil {
			return headerErr
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		writer, createErr := zw.CreateHeader(header)
		if createErr != nil {
			return createErr
		}
		file, openErr := os.Open(filePath)
		if openErr != nil {
			return openErr
		}
		written, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != info.Size() {
			return fmt.Errorf("release file size changed while reading")
		}
		fileCount++
		totalBytes += written
		return nil
	})
	if err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if fileCount != release.FileCount || totalBytes != release.TotalBytes {
		return fmt.Errorf("release content metadata does not match stored files")
	}
	return nil
}

// contentFailureStatus 把内容路径和文件状态转换为对外错误：普通资源缺失返回404，入口或存储故障返回503。
func contentFailureStatus(err error, isEntry, isRegular bool) int {
	if errors.Is(err, service.ErrInvalid) {
		return http.StatusNotFound
	}
	if errors.Is(err, service.ErrDependency) {
		return http.StatusServiceUnavailable
	}
	if err != nil {
		if !isEntry && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)) {
			return http.StatusNotFound
		}
		return http.StatusServiceUnavailable
	}
	if !isRegular {
		if isEntry {
			return http.StatusServiceUnavailable
		}
		return http.StatusNotFound
	}
	return 0
}

// failContentFile 以统一业务错误输出内容文件访问失败，区分资源不存在与存储依赖异常。
func failContentFile(c *gin.Context, status int) {
	if status == http.StatusNotFound {
		ginfmt.Fail(c, service.ErrNotFound)
		return
	}
	ginfmt.Fail(c, service.ErrDependency)
}
