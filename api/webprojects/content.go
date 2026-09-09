package webprojects

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

func serveProjectContent(c *gin.Context) {
	project, release, err := service.GetPublishedProject(c.Param("slug"))
	if err != nil {
		failWithError(c, err)
		return
	}
	userID := int64(0)
	if project.AccessMode != service.AccessModePublic {
		cookie, cookieErr := c.Cookie("__Host-web_projects_session")
		if cookieErr == nil {
			user, loginErr := service.CheckContentUser(ginfmt.RPCContext(c), cookie)
			if loginErr == nil && user != nil {
				userID = user.ID
			} else if loginErr != nil && service.IsDependencyError(loginErr) {
				failWithError(c, loginErr)
				return
			} else {
				clearContentCookie(c)
			}
		}
		if userID == 0 {
			if isDocumentNavigation(c.Request.Method, c.Request.URL.Path, c.GetHeader("Accept")) {
				target := c.Request.URL.RequestURI()
				if validateProjectTarget(target) == nil {
					c.Header("Cache-Control", "no-store")
					c.Redirect(http.StatusFound, "/web-projects/open?target="+url.QueryEscape(target))
					return
				}
			}
			failWithError(c, service.ErrUnauthorized)
			return
		}
	}
	isMember := false
	if project.AccessMode == service.AccessModeMembers && userID != project.OwnerUserID {
		isMember, err = service.IsMember(project.ID, userID)
		if err != nil {
			failWithError(c, service.ErrDependency)
			return
		}
	}
	if !service.CanReadProject(project.AccessMode, project.OwnerUserID, userID, isMember) {
		failWithError(c, service.ErrNotFound)
		return
	}
	if c.GetHeader("Service-Worker") != "" {
		failWithError(c, service.ErrForbidden)
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
	conf := config.Global().WebProjects
	contentRoot, err := service.ReleaseContentRoot(&conf, release)
	if err != nil {
		failWithError(c, err)
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
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("ETag", `"`+strconv.FormatInt(release.ID, 10)+`-`+strconv.FormatInt(info.Size(), 10)+`-`+strconv.FormatInt(info.ModTime().UnixNano(), 10)+`"`)
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func downloadRelease(c *gin.Context) {
	projectID, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		failWithError(c, err)
		return
	}
	releaseID, err := service.ParsePositiveID(c.Param("release_id"))
	if err != nil {
		failWithError(c, err)
		return
	}
	release, err := service.GetReleaseForDownload(currentUserID(c), projectID, releaseID)
	if err != nil {
		failWithError(c, err)
		return
	}
	conf := config.Global().WebProjects
	serveReleaseDownload(c, &conf, release)
}

func serveReleaseDownload(c *gin.Context, conf *config.WebProjectsConfig, release *model.WebProjectRelease) {
	if conf == nil || release == nil {
		failWithError(c, service.ErrDependency)
		return
	}
	contentRoot, err := service.ReleaseContentRoot(conf, release)
	if err != nil {
		failWithError(c, service.ErrDependency)
		return
	}
	temp, err := os.CreateTemp(filepath.Join(conf.StorageRoot, "staging"), "download-*.zip")
	if err != nil {
		failWithError(c, service.ErrDependency)
		return
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := writeReleaseArchive(temp, contentRoot, release); err != nil {
		failWithError(c, service.ErrDependency)
		return
	}
	info, err := temp.Stat()
	if err != nil {
		failWithError(c, service.ErrDependency)
		return
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		failWithError(c, service.ErrDependency)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="web-project-%d-release-%d.zip"`, release.ProjectID, release.ID))
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

func clearContentCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-web_projects_session", Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

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

func failContentFile(c *gin.Context, status int) {
	if status == http.StatusNotFound {
		failWithError(c, service.ErrNotFound)
		return
	}
	failWithError(c, service.ErrDependency)
}
