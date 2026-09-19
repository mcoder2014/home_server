package manuals

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
)

const (
	MaxNameRunes         = 120
	MaxCategoryRunes     = 100
	MaxCategories        = 20
	MaxDescriptionRunes  = 2000
	MaxTitleRunes        = 200
	MaxTextRunes         = 100000
	MaxURLBytes          = 2048
	MaxRequestIDBytes    = 128
	MaxOriginalNameRunes = 255
	MaxImagePixels       = 40_000_000
	ThumbnailMaxEdge     = 480
)

type InlineItemInput struct {
	Kind            string `json:"kind"`
	Title           string `json:"title"`
	Text            string `json:"text"`
	URL             string `json:"url"`
	ClientRequestID string `json:"client_request_id"`
}

type ValidatedInlineItem struct {
	Kind            model.ManualItemKind
	Title           string
	Text            string
	URL             string
	ClientRequestID string
}

// Init applies fixed defaults and prepares the private storage root only when
// the statically configured module is enabled.
func Init(conf *config.ManualsConfig, webDAVRoot, webProjectsRoot string) error {
	if conf == nil {
		return fmt.Errorf("manuals configuration is missing")
	}
	applyDefaults(conf)
	if err := validateLimits(conf); err != nil {
		return err
	}
	configurePreviewGate(conf.MaxConcurrentPDFPreviews)
	if !conf.Enabled {
		return nil
	}
	if !filepath.IsAbs(conf.StorageRoot) {
		return fmt.Errorf("manuals.storage_root must be an absolute path")
	}
	if err := os.MkdirAll(conf.StorageRoot, 0700); err != nil {
		return fmt.Errorf("create manuals storage root: %w", err)
	}
	if err := rejectRootSymlink(conf.StorageRoot); err != nil {
		return err
	}
	if err := validateStorageIsolation(conf.StorageRoot, webDAVRoot, "webdav.share_path"); err != nil {
		return err
	}
	if err := validateStorageIsolation(conf.StorageRoot, webProjectsRoot, "web_projects.storage_root"); err != nil {
		return err
	}
	if err := os.Chmod(conf.StorageRoot, 0700); err != nil {
		return fmt.Errorf("secure manuals storage root: %w", err)
	}
	return os.MkdirAll(filepath.Join(conf.StorageRoot, ".staging"), 0700)
}

func applyDefaults(conf *config.ManualsConfig) {
	if conf.MaxFileBytes <= 0 {
		conf.MaxFileBytes = 50 << 20
	}
	if conf.MaxItemsPerManual <= 0 {
		conf.MaxItemsPerManual = 100
	}
	if conf.MaxManualBytes <= 0 {
		conf.MaxManualBytes = 500 << 20
	}
	if conf.MaxManualsPerUser <= 0 {
		conf.MaxManualsPerUser = 1000
	}
	if conf.MaxUserBytes <= 0 {
		conf.MaxUserBytes = 10 << 30
	}
	if conf.MinFreeDiskBytes <= 0 {
		conf.MinFreeDiskBytes = 2 << 30
	}
	if conf.PDFToPPMPath == "" {
		conf.PDFToPPMPath = "/usr/bin/pdftoppm"
	}
	if conf.PRLimitPath == "" {
		conf.PRLimitPath = "/usr/bin/prlimit"
	}
	if conf.PDFPreviewTimeoutSeconds <= 0 {
		conf.PDFPreviewTimeoutSeconds = 10
	}
	if conf.MaxConcurrentPDFPreviews <= 0 {
		conf.MaxConcurrentPDFPreviews = 1
	}
	if conf.PDFPreviewMemoryLimitBytes <= 0 {
		conf.PDFPreviewMemoryLimitBytes = 512 << 20
	}
	if conf.PDFPreviewCPUSeconds <= 0 {
		conf.PDFPreviewCPUSeconds = 10
	}
	if conf.PDFPreviewOutputLimitBytes <= 0 {
		conf.PDFPreviewOutputLimitBytes = 16 << 20
	}
	if conf.PDFPreviewOpenFilesLimit <= 0 {
		conf.PDFPreviewOpenFilesLimit = 32
	}
}

func validateLimits(conf *config.ManualsConfig) error {
	if conf.MaxFileBytes <= 0 || conf.MaxItemsPerManual < 1 || conf.MaxItemsPerManual > 1000 || conf.MaxManualBytes < conf.MaxFileBytes || conf.MaxUserBytes < conf.MaxManualBytes || conf.MaxManualsPerUser < 1 || conf.MinFreeDiskBytes < 0 {
		return fmt.Errorf("invalid manuals storage limits")
	}
	if conf.PDFPreviewTimeoutSeconds < 1 || conf.PDFPreviewTimeoutSeconds > 60 || conf.MaxConcurrentPDFPreviews < 1 || conf.MaxConcurrentPDFPreviews > 8 || conf.PDFPreviewMemoryLimitBytes < 64<<20 || conf.PDFPreviewCPUSeconds < 1 || conf.PDFPreviewCPUSeconds > 60 || conf.PDFPreviewOutputLimitBytes < 1<<20 || conf.PDFPreviewOpenFilesLimit < 8 {
		return fmt.Errorf("invalid manuals PDF preview limits")
	}
	if !filepath.IsAbs(conf.PDFToPPMPath) || !filepath.IsAbs(conf.PRLimitPath) {
		return fmt.Errorf("manuals PDF tools must use absolute paths")
	}
	return nil
}

func rejectRootSymlink(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect manuals storage root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("manuals.storage_root must be a real directory, not a symlink")
	}
	return nil
}

func validateStorageIsolation(root, other, otherName string) error {
	if strings.TrimSpace(other) == "" {
		return nil
	}
	manualPath, err := canonicalPath(root)
	if err != nil {
		return fmt.Errorf("resolve manuals storage: %w", err)
	}
	otherPath, err := canonicalPath(other)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", otherName, err)
	}
	separator := string(os.PathSeparator)
	if manualPath == otherPath || strings.HasPrefix(manualPath, otherPath+separator) || strings.HasPrefix(otherPath, manualPath+separator) {
		return fmt.Errorf("manuals.storage_root must not overlap %s", otherName)
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

func ValidateManualFields(name, description, accessMode string) (string, string, model.ManualAccess, error) {
	name = strings.TrimSpace(name)
	if accessMode == "" {
		accessMode = "owner"
	}
	access, ok := model.ParseManualAccess(accessMode)
	if name == "" || !ok || !validRunes(name, MaxNameRunes) || !validRunes(description, MaxDescriptionRunes) {
		return "", "", 0, apperrors.ErrInvalid
	}
	return name, description, access, nil
}

func ValidateCategories(values []string) ([]string, error) {
	if len(values) > MaxCategories {
		return nil, apperrors.ErrInvalid
	}
	categories := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		category := strings.TrimSpace(value)
		if category == "" || !validRunes(category, MaxCategoryRunes) {
			return nil, apperrors.ErrInvalid
		}
		if _, exists := seen[category]; exists {
			continue
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}
	sort.Strings(categories)
	return categories, nil
}

func ValidateInlineItem(input InlineItemInput) (*ValidatedInlineItem, error) {
	kind, ok := model.ParseManualItemKind(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	if !ok || (kind != model.ManualItemText && kind != model.ManualItemURL) || !validRunes(input.Title, MaxTitleRunes) || !ValidRequestID(input.ClientRequestID) {
		return nil, apperrors.ErrInvalid
	}
	result := &ValidatedInlineItem{Kind: kind, Title: input.Title, ClientRequestID: input.ClientRequestID}
	if kind == model.ManualItemText {
		if input.Text == "" || !validRunes(input.Text, MaxTextRunes) || input.URL != "" {
			return nil, apperrors.ErrInvalid
		}
		result.Text = input.Text
		return result, nil
	}
	if input.Text != "" || len(input.URL) == 0 || len(input.URL) > MaxURLBytes || strings.TrimSpace(input.URL) != input.URL {
		return nil, apperrors.ErrInvalid
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, apperrors.ErrInvalid
	}
	result.URL = parsed.String()
	return result, nil
}

func ValidRequestID(value string) bool {
	if value == "" || len(value) > MaxRequestIDBytes {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && !strings.ContainsRune("._:-", character) {
			return false
		}
	}
	return true
}

func ValidateFileMetadata(title, requestID string) (string, error) {
	title = strings.TrimSpace(title)
	if !validRunes(title, MaxTitleRunes) || !ValidRequestID(requestID) {
		return "", apperrors.ErrInvalid
	}
	return title, nil
}

func CanReadManual(access model.ManualAccess, status model.ManualStatus, ownerUserID, viewerUserID int64) bool {
	if status == model.ManualStatusDeleted {
		return false
	}
	if viewerUserID > 0 && viewerUserID == ownerUserID {
		return true
	}
	if status != model.ManualStatusActive {
		return false
	}
	return access == model.ManualAccessPublic || (access == model.ManualAccessAuthenticated && viewerUserID > 0)
}

func validRunes(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}
