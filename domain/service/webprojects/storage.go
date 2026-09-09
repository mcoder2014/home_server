package webprojects

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mcoder2014/home_server/config"
)

type Artifact struct {
	StorageKey string
	EntryFile  string
	SHA256     string
	FileCount  int
	TotalBytes int64
}

var allowedExtensions = map[string]bool{
	".avif": true, ".bmp": true, ".css": true, ".csv": true, ".eot": true,
	".gif": true, ".html": true, ".ico": true, ".jpeg": true, ".jpg": true,
	".js": true, ".json": true, ".map": true, ".md": true, ".mjs": true,
	".otf": true, ".pdf": true, ".png": true, ".svg": true, ".txt": true,
	".wasm": true, ".webmanifest": true, ".webp": true, ".woff": true, ".woff2": true,
	".xml": true,
}

func StoreUpload(conf *config.WebProjectsConfig, projectID, releaseID, fileName, entryFile string, src io.Reader) (*Artifact, error) {
	if conf == nil || !conf.Enabled {
		return nil, fmt.Errorf("%w: web projects are disabled", ErrDependency)
	}
	if !isGeneratedID(projectID) || !isGeneratedID(releaseID) {
		return nil, fmt.Errorf("%w: invalid generated id", ErrDependency)
	}
	stagingDir := filepath.Join(conf.StorageRoot, "staging", releaseID)
	if err := os.RemoveAll(stagingDir); err != nil {
		return nil, fmt.Errorf("%w: clean staging directory: %w", ErrDependency, err)
	}
	if err := os.MkdirAll(filepath.Join(stagingDir, "content"), 0700); err != nil {
		return nil, fmt.Errorf("%w: create staging directory: %w", ErrDependency, err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	ext := strings.ToLower(filepath.Ext(fileName))
	var artifact *Artifact
	var err error
	switch ext {
	case ".html", ".htm":
		artifact, err = storeHTML(conf, stagingDir, src)
	case ".zip":
		artifact, err = storeZIP(conf, stagingDir, entryFile, src)
	default:
		return nil, fmt.Errorf("%w: unsupported upload type", ErrUnsupported)
	}
	if err != nil {
		return nil, err
	}
	finalReleaseDir := filepath.Join(conf.StorageRoot, "projects", projectID, "releases", releaseID)
	if err := os.MkdirAll(filepath.Dir(finalReleaseDir), 0700); err != nil {
		return nil, fmt.Errorf("%w: create release parent: %w", ErrDependency, err)
	}
	if err := os.Rename(stagingDir, finalReleaseDir); err != nil {
		return nil, fmt.Errorf("%w: publish complete release directory: %w", ErrDependency, err)
	}
	artifact.StorageKey = filepath.ToSlash(filepath.Join("projects", projectID, "releases", releaseID, "content"))
	succeeded = true
	return artifact, nil
}

func storeHTML(conf *config.WebProjectsConfig, stagingDir string, src io.Reader) (*Artifact, error) {
	target := filepath.Join(stagingDir, "content", "index.html")
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("%w: create html: %w", ErrDependency, err)
	}
	hash := sha256.New()
	limit := min64(conf.MaxUploadBytes, conf.MaxFileBytes)
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(src, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("%w: write html: %w", ErrDependency, copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%w: close html: %w", ErrDependency, closeErr)
	}
	if written > limit {
		return nil, fmt.Errorf("%w: upload exceeds maximum size", ErrTooLarge)
	}
	return &Artifact{EntryFile: "index.html", SHA256: hex.EncodeToString(hash.Sum(nil)), FileCount: 1, TotalBytes: written}, nil
}

func storeZIP(conf *config.WebProjectsConfig, stagingDir, entryFile string, src io.Reader) (*Artifact, error) {
	archivePath := filepath.Join(stagingDir, "upload.zip")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("%w: create staged archive: %w", ErrDependency, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(src, conf.MaxUploadBytes+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("%w: store archive: %w", ErrDependency, copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%w: close archive: %w", ErrDependency, closeErr)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("%w: stat staged archive: %w", ErrDependency, err)
	}
	if info.Size() > conf.MaxUploadBytes {
		return nil, fmt.Errorf("%w: upload exceeds maximum size", ErrTooLarge)
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		if isZIPValidationError(err) {
			return nil, fmt.Errorf("%w: invalid zip archive: %w", ErrUnprocessable, err)
		}
		return nil, fmt.Errorf("%w: open staged archive: %w", ErrDependency, err)
	}
	defer zr.Close()
	if entryFile == "" {
		entryFile = "index.html"
	}
	entryFile, err = validateRelativePath(entryFile, conf.MaxDirectoryDepth)
	if err != nil || strings.ToLower(path.Ext(entryFile)) != ".html" {
		return nil, fmt.Errorf("%w: invalid entry file", ErrUnprocessable)
	}
	seen := make(map[string]bool, len(zr.File))
	regularFiles := make(map[string]bool, len(zr.File))
	var total int64
	entryCount := 0
	fileCount := 0
	for _, file := range zr.File {
		entryCount++
		if entryCount > conf.MaxFileCount {
			return nil, fmt.Errorf("%w: zip content exceeds file limits", ErrTooLarge)
		}
		name := file.Name
		if file.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		cleanName, pathErr := validateRelativePath(name, conf.MaxDirectoryDepth)
		if pathErr != nil {
			return nil, fmt.Errorf("%w: invalid zip path %q: %w", ErrUnprocessable, file.Name, pathErr)
		}
		if seen[cleanName] {
			return nil, fmt.Errorf("%w: duplicate zip path %q", ErrUnprocessable, cleanName)
		}
		seen[cleanName] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: zip path %q is not a regular file", ErrUnprocessable, cleanName)
		}
		fileCount++
		if !allowedExtensions[strings.ToLower(path.Ext(cleanName))] || hasReservedComponent(cleanName) {
			return nil, fmt.Errorf("%w: zip path %q has unsupported content", ErrUnprocessable, cleanName)
		}
		regularFiles[cleanName] = true
		if file.UncompressedSize64 > uint64(conf.MaxFileBytes) {
			return nil, fmt.Errorf("%w: zip content exceeds file limits", ErrTooLarge)
		}
		total += int64(file.UncompressedSize64)
		if total > conf.MaxExpandedBytes {
			return nil, fmt.Errorf("%w: zip content exceeds expanded size", ErrTooLarge)
		}
		if err := extractZipFile(file, filepath.Join(stagingDir, "content"), cleanName, conf.MaxFileBytes); err != nil {
			return nil, err
		}
	}
	if len(regularFiles) == 0 || !regularFiles[entryFile] {
		return nil, fmt.Errorf("%w: entry file %q is missing", ErrUnprocessable, entryFile)
	}
	if err := os.Remove(archivePath); err != nil {
		return nil, fmt.Errorf("%w: remove staged archive: %w", ErrDependency, err)
	}
	return &Artifact{EntryFile: entryFile, SHA256: hex.EncodeToString(hash.Sum(nil)), FileCount: fileCount, TotalBytes: total}, nil
}

func extractZipFile(file *zip.File, contentRoot, cleanName string, maxFileBytes int64) error {
	target := filepath.Join(contentRoot, filepath.FromSlash(cleanName))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return fmt.Errorf("%w: create zip directory: %w", ErrDependency, err)
	}
	in, err := file.Open()
	if err != nil {
		if isZIPValidationError(err) {
			return fmt.Errorf("%w: open zip path %q: %w", ErrUnprocessable, cleanName, err)
		}
		return fmt.Errorf("%w: open zip path %q: %w", ErrDependency, cleanName, err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("%w: create zip path %q: %w", ErrDependency, cleanName, err)
	}
	written, copyErr := io.Copy(out, io.LimitReader(in, maxFileBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		if isZIPValidationError(copyErr) {
			return fmt.Errorf("%w: extract zip path %q: %w", ErrUnprocessable, cleanName, copyErr)
		}
		return fmt.Errorf("%w: extract zip path %q: %w", ErrDependency, cleanName, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: close zip path %q: %w", ErrDependency, cleanName, closeErr)
	}
	if written > maxFileBytes || written != int64(file.UncompressedSize64) {
		return fmt.Errorf("%w: zip path %q exceeds declared size", ErrUnprocessable, cleanName)
	}
	return nil
}

func validateRelativePath(name string, maxDepth int) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("path is not relative")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != name || len(strings.Split(clean, "/")) > maxDepth {
		return "", fmt.Errorf("path is unsafe")
	}
	return clean, nil
}

func hasReservedComponent(name string) bool {
	for _, component := range strings.Split(strings.ToLower(name), "/") {
		if component == ".git" || component == ".env" || strings.HasPrefix(component, ".env.") {
			return true
		}
	}
	return false
}

func isGeneratedID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func isZIPValidationError(err error) bool {
	return errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, zip.ErrChecksum)
}
