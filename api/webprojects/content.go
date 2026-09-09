package webprojects

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/config"
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
		if c.Request.URL.RawQuery != "" { target += "?" + c.Request.URL.RawQuery }
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
	filePath, err := service.ResolveContentPath(contentRoot, requested)
	if err != nil {
		failWithError(c, service.ErrNotFound)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) && requested != release.EntryFile {
			failWithError(c, service.ErrNotFound)
		} else {
			failWithError(c, service.ErrDependency)
		}
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		failWithError(c, service.ErrNotFound)
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
	contentRoot, err := service.ReleaseContentRoot(&conf, release)
	if err != nil {
		failWithError(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="web-project-%d-release-%d.zip"`, projectID, releaseID))
	c.Header("Cache-Control", "no-store")
	zw := zip.NewWriter(c.Writer)
	err = filepath.Walk(contentRoot, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.Mode().IsRegular() {
			return nil
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
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = c.Error(err)
	}
}

func clearContentCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-web_projects_session", Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
