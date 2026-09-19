package fileshare

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mcoder2014/home_server/config"
)

type StoredUpload struct {
	StorageKey string
	SizeBytes  int64
	SHA256     string
}

func Init(conf *config.FileSharingConfig, otherRoots ...string) error {
	if conf == nil {
		return fmt.Errorf("file_sharing configuration is missing")
	}
	applyDefaults(conf)
	if conf.MaxFileBytes < 1 || conf.MaxFilesPerUser < 1 || conf.MaxFilesPerUser > 100000 || conf.MaxUserBytes < conf.MaxFileBytes || conf.MinFreeDiskBytes < 0 || conf.MaxConcurrentUploadsPerUser < 1 || conf.MaxConcurrentUploadsPerUser > 100 || conf.MaxConcurrentUploads < conf.MaxConcurrentUploadsPerUser || conf.MaxConcurrentUploads > 1000 {
		return fmt.Errorf("invalid file_sharing storage limits")
	}
	if !conf.Enabled {
		return nil
	}
	if !filepath.IsAbs(conf.StorageRoot) {
		return fmt.Errorf("file_sharing.storage_root must be an absolute path")
	}
	if err := os.MkdirAll(conf.StorageRoot, 0700); err != nil {
		return fmt.Errorf("create file sharing storage root: %w", err)
	}
	rootInfo, err := os.Lstat(conf.StorageRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("file_sharing.storage_root must be a regular directory")
	}
	for _, other := range otherRoots {
		if strings.TrimSpace(other) == "" {
			continue
		}
		if err := rejectStorageOverlap(conf.StorageRoot, other); err != nil {
			return err
		}
	}
	if err := os.Chmod(conf.StorageRoot, 0700); err != nil {
		return fmt.Errorf("secure file sharing storage root: %w", err)
	}
	return os.MkdirAll(filepath.Join(conf.StorageRoot, ".staging"), 0700)
}

func applyDefaults(conf *config.FileSharingConfig) {
	if conf.MaxFileBytes <= 0 {
		conf.MaxFileBytes = 50 << 20
	}
	if conf.MaxFilesPerUser <= 0 {
		conf.MaxFilesPerUser = 1000
	}
	if conf.MaxUserBytes <= 0 {
		conf.MaxUserBytes = 10 << 30
	}
	if conf.MinFreeDiskBytes <= 0 {
		conf.MinFreeDiskBytes = 2 << 30
	}
	if conf.MaxConcurrentUploadsPerUser <= 0 {
		conf.MaxConcurrentUploadsPerUser = 2
	}
	if conf.MaxConcurrentUploads <= 0 {
		conf.MaxConcurrentUploads = 4
	}
}

func rejectStorageOverlap(storageRoot, otherRoot string) error {
	left, err := canonicalPath(storageRoot)
	if err != nil {
		return fmt.Errorf("resolve file_sharing storage: %w", err)
	}
	right, err := canonicalPath(otherRoot)
	if err != nil {
		return fmt.Errorf("resolve existing storage: %w", err)
	}
	separator := string(os.PathSeparator)
	if left == right || strings.HasPrefix(left, right+separator) || strings.HasPrefix(right, left+separator) {
		return fmt.Errorf("file_sharing.storage_root must not overlap another storage root")
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

// StoreUpload writes to private staging, fsyncs the complete bounded file, then
// atomically renames it under service-generated owner and file directories.
func StoreUpload(conf *config.FileSharingConfig, ownerID, fileID int64, source io.Reader) (*StoredUpload, error) {
	if conf == nil || !conf.Enabled || ownerID <= 0 || fileID <= 0 || source == nil {
		return nil, ErrDependency
	}
	temporary, err := os.CreateTemp(filepath.Join(conf.StorageRoot, ".staging"), "upload-")
	if err != nil {
		return nil, ErrDependency
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(source, conf.MaxFileBytes+1))
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return nil, ErrDependency
	}
	if written > conf.MaxFileBytes {
		return nil, ErrTooLarge
	}
	ownerDirectory, err := ensureDirectory(conf.StorageRoot, strconv.FormatInt(ownerID, 10))
	if err != nil {
		return nil, ErrDependency
	}
	fileDirectory, err := ensureDirectory(ownerDirectory, strconv.FormatInt(fileID, 10))
	if err != nil {
		return nil, ErrDependency
	}
	randomName, err := randomStorageName()
	if err != nil {
		return nil, ErrDependency
	}
	finalPath := filepath.Join(fileDirectory, randomName)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return nil, ErrDependency
	}
	if err := syncDirectory(fileDirectory); err != nil {
		_ = os.Remove(finalPath)
		return nil, ErrDependency
	}
	return &StoredUpload{StorageKey: path.Join(strconv.FormatInt(ownerID, 10), strconv.FormatInt(fileID, 10), randomName), SizeBytes: written, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func ensureDirectory(parent, name string) (string, error) {
	target := filepath.Join(parent, name)
	if err := os.Mkdir(target, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("unsafe storage directory")
	}
	if err := os.Chmod(target, 0700); err != nil {
		return "", err
	}
	return target, nil
}

func randomStorageName() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func OpenStoredFile(conf *config.FileSharingConfig, key string) (*os.File, os.FileInfo, error) {
	if conf == nil || !validStorageKey(key) {
		return nil, nil, ErrNotFound
	}
	root, err := filepath.EvalSymlinks(conf.StorageRoot)
	if err != nil {
		return nil, nil, ErrDependency
	}
	target := filepath.Join(conf.StorageRoot, filepath.FromSlash(key))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil || !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return nil, nil, ErrNotFound
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, ErrNotFound
	}
	return file, info, nil
}

func RemoveStoredFile(conf *config.FileSharingConfig, key string) error {
	file, _, err := OpenStoredFile(conf, key)
	if err != nil {
		if err == ErrNotFound {
			return nil
		}
		return err
	}
	name := file.Name()
	_ = file.Close()
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return ErrDependency
	}
	return nil
}

func validStorageKey(key string) bool {
	if key == "" || path.Clean(key) != key || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return false
	}
	parts := strings.Split(key, "/")
	if len(parts) != 3 || len(parts[2]) != 32 {
		return false
	}
	for _, part := range parts[:2] {
		value, err := strconv.ParseInt(part, 10, 64)
		if err != nil || value <= 0 || strconv.FormatInt(value, 10) != part {
			return false
		}
	}
	_, err := hex.DecodeString(parts[2])
	return err == nil
}

func DiskFreeBytes(root string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || uint64(stat.Bavail) > math.MaxUint64/uint64(stat.Bsize) {
		return 0, fmt.Errorf("invalid filesystem free-space result")
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
