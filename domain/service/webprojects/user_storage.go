package webprojects

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
)

func ReleaseStorageKey(ownerUserID, projectID, releaseID int64) (string, error) {
	if ownerUserID <= 0 || projectID <= 0 || releaseID <= 0 {
		return "", fmt.Errorf("%w: invalid storage identity", ErrInvalid)
	}
	return fmt.Sprintf("%d/upload/html/%d/releases/%d/content", ownerUserID, projectID, releaseID), nil
}

func LegacyReleaseStorageKey(projectID, releaseID int64) (string, error) {
	if projectID <= 0 || releaseID <= 0 {
		return "", fmt.Errorf("%w: invalid storage identity", ErrInvalid)
	}
	return fmt.Sprintf("projects/%d/releases/%d/content", projectID, releaseID), nil
}

func UserStagingRoot(conf *config.WebProjectsConfig, ownerUserID int64) (string, error) {
	if ownerUserID <= 0 {
		return "", fmt.Errorf("%w: invalid storage owner", ErrInvalid)
	}
	relative := strconv.FormatInt(ownerUserID, 10) + "/upload/html/.staging"
	directory, _, err := storageDirectory(conf, relative, true)
	return directory, err
}

// ReleaseDirectory accepts only a key derived from the persisted release
// identity. Callers must additionally verify UploadedBy against the project
// owner. Missing descendants are allowed for idempotent cleanup; a missing
// storage volume is an error and must never erase database references.
func ReleaseDirectory(conf *config.WebProjectsConfig, release *model.WebProjectRelease) (string, bool, error) {
	if release == nil {
		return "", false, ErrInvalid
	}
	canonical, err := ReleaseStorageKey(release.UploadedBy, release.ProjectID, release.ID)
	if err != nil {
		return "", false, err
	}
	legacy, _ := LegacyReleaseStorageKey(release.ProjectID, release.ID)
	if release.StorageKey != canonical && release.StorageKey != legacy {
		return "", false, fmt.Errorf("%w: unexpected release storage key", ErrInvalid)
	}
	return storageDirectory(conf, path.Dir(release.StorageKey), false)
}

// storageDirectory walks only server-selected relative directories. The
// configured volume root may be a symlink, but every descendant must be a real
// directory; this prevents user trees and cleanup from following links across
// storage owners. Creation is private and does not replace existing paths.
func storageDirectory(conf *config.WebProjectsConfig, relative string, create bool) (string, bool, error) {
	if conf == nil || !filepath.IsAbs(conf.StorageRoot) {
		return "", false, fmt.Errorf("%w: storage root must be absolute", ErrDependency)
	}
	if _, err := validateRelativePath(relative, 64); err != nil {
		return "", false, fmt.Errorf("%w: invalid storage directory", ErrInvalid)
	}
	root, err := filepath.EvalSymlinks(conf.StorageRoot)
	if err != nil {
		return "", false, fmt.Errorf("%w: resolve storage volume: %w", ErrDependency, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", false, fmt.Errorf("%w: inspect storage volume: %w", ErrDependency, err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("%w: %w", ErrDependency, &os.PathError{Op: "stat", Path: root, Err: syscall.ENOTDIR})
	}
	current := root
	for _, component := range strings.Split(relative, "/") {
		current = filepath.Join(current, component)
		if create {
			mkdirErr := os.Mkdir(current, 0700)
			if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", false, fmt.Errorf("%w: create private directory: %w", ErrDependency, mkdirErr)
			}
			// Persist each new ancestor before a migration can move data into it.
			if mkdirErr == nil {
				if err := syncStorageDirectory(filepath.Dir(current)); err != nil {
					return "", false, fmt.Errorf("%w: persist storage directory: %w", ErrDependency, err)
				}
			}
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && !create {
			return filepath.Join(root, filepath.FromSlash(relative)), true, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("%w: inspect storage directory: %w", ErrDependency, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, fmt.Errorf("%w: storage ancestry must contain only real directories", ErrInvalid)
		}
	}
	return current, false, nil
}

func syncStorageDirectory(directoryPath string) error {
	directory, err := os.Open(directoryPath)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
