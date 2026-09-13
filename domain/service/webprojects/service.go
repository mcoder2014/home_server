package webprojects

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
)

const (
	AccessModeOwner         = model.WebProjectAccessOwner
	AccessModeMembers       = model.WebProjectAccessMembers
	AccessModeAuthenticated = model.WebProjectAccessAuthenticated
	AccessModePublic        = model.WebProjectAccessPublic
)

func Init(conf *config.WebProjectsConfig) error {
	if conf == nil || (!conf.Enabled && config.Global().IdentitySource != "database") {
		return nil
	}
	if !filepath.IsAbs(conf.StorageRoot) {
		return fmt.Errorf("web_projects.storage_root must be an absolute path")
	}
	origin, err := url.Parse(conf.SiteOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return fmt.Errorf("web_projects.site_origin must be an https origin without path, query or fragment")
	}
	applyDefaults(conf)
	if err := os.MkdirAll(conf.StorageRoot, 0700); err != nil {
		return fmt.Errorf("create web storage root: %w", err)
	}
	return nil
}

func ValidateStorageIsolation(storageRoot, webDAVRoot string) error {
	if strings.TrimSpace(webDAVRoot) == "" {
		return nil
	}
	privatePath, err := canonicalPath(storageRoot)
	if err != nil {
		return fmt.Errorf("resolve web projects storage: %w", err)
	}
	webDAVPath, err := canonicalPath(webDAVRoot)
	if err != nil {
		return fmt.Errorf("resolve webdav storage: %w", err)
	}
	if pathsOverlap(privatePath, webDAVPath) {
		return fmt.Errorf("web_projects.storage_root must not overlap webdav.share_path")
	}
	return nil
}

func canonicalPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	parent, parentErr := filepath.EvalSymlinks(filepath.Dir(absolute))
	if parentErr != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func pathsOverlap(left, right string) bool {
	separator := string(os.PathSeparator)
	return left == right || strings.HasPrefix(left, right+separator) || strings.HasPrefix(right, left+separator)
}

// applyDefaults 为未设置的上传、解压、项目容量、并发和删除保留期限制填入默认值。
func applyDefaults(conf *config.WebProjectsConfig) {
	if conf.MaxUploadBytes <= 0 {
		conf.MaxUploadBytes = 50 << 20
	}
	if conf.MaxExpandedBytes <= 0 {
		conf.MaxExpandedBytes = 200 << 20
	}
	if conf.MaxFileBytes <= 0 {
		conf.MaxFileBytes = 50 << 20
	}
	if conf.MaxFileCount <= 0 {
		conf.MaxFileCount = 5000
	}
	if conf.MaxDirectoryDepth <= 0 {
		conf.MaxDirectoryDepth = 16
	}
	if conf.MaxProjectBytes <= 0 {
		conf.MaxProjectBytes = 1 << 30
	}
	if conf.MaxReleases <= 0 {
		conf.MaxReleases = 10
	}
	if conf.MaxConcurrentUploadsPerUser <= 0 {
		conf.MaxConcurrentUploadsPerUser = 2
	}
	if conf.MaxConcurrentExtracts <= 0 {
		conf.MaxConcurrentExtracts = 4
	}
	if conf.DeleteRetentionDays <= 0 {
		conf.DeleteRetentionDays = 7
	}
}

func CanReadProject(mode model.WebProjectAccess, ownerUserID, userID int64, isMember bool) bool {
	switch mode {
	case AccessModePublic:
		return true
	case AccessModeAuthenticated:
		return userID > 0
	case AccessModeMembers:
		return userID > 0 && (userID == ownerUserID || isMember)
	case AccessModeOwner:
		return userID > 0 && userID == ownerUserID
	default:
		return false
	}
}
