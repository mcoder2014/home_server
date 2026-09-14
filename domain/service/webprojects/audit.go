package webprojects

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/webprojects"
)

const auditReferenceBatchSize = 200

const (
	AuditReasonMissingDatabaseReference = "missing_database_reference"
	AuditReasonMissingProjectReference  = "missing_project_reference"
	AuditReasonStorageKeyMismatch       = "storage_key_mismatch"
	AuditReasonProjectMismatch          = "project_mismatch"
	AuditReasonOwnerMismatch            = "owner_mismatch"
	AuditReasonDirectoryOwnerMismatch   = "directory_owner_mismatch"
)

var ErrAuditDatabase = errors.New("web project audit database unavailable")

type AuditCandidate struct {
	OwnerID             string    `json:"owner_id,omitempty"`
	DirectoryOwnerID    string    `json:"directory_owner_id,omitempty"`
	ProjectID           string    `json:"project_id"`
	ReleaseID           string    `json:"release_id"`
	RelativePath        string    `json:"relative_path"`
	ExpectedStorageKey  string    `json:"expected_storage_key"`
	DatabaseStorageKey  string    `json:"database_storage_key,omitempty"`
	Reason              string    `json:"reason"`
	DirectoryModifyTime time.Time `json:"directory_modify_time"`
}

type AuditStats struct {
	OwnerEntries                 int `json:"owner_entries"`
	ProjectEntries               int `json:"project_entries"`
	ReleaseEntries               int `json:"release_entries"`
	AuditedReleaseDirectories    int `json:"audited_release_directories"`
	ReferencedReleaseDirectories int `json:"referenced_release_directories"`
	CandidateDirectories         int `json:"candidate_directories"`
	SkippedRecentDirectories     int `json:"skipped_recent_directories"`
	SkippedSymlinks              int `json:"skipped_symlinks"`
	SkippedAbnormalEntries       int `json:"skipped_abnormal_entries"`
	DatabaseBatches              int `json:"database_batches"`
}

type StorageAuditReport struct {
	GeneratedAt   time.Time        `json:"generated_at"`
	StorageRoot   string           `json:"storage_root"`
	MinAgeSeconds int64            `json:"min_age_seconds"`
	Candidates    []AuditCandidate `json:"candidates"`
	Stats         AuditStats       `json:"stats"`
}

type auditReleaseDirectory struct {
	ownerID      int64
	projectID    int64
	releaseID    int64
	relativePath string
	storageKey   string
	modifyTime   time.Time
}

type auditReferenceQueries struct {
	queryReleases func([]int64) ([]*model.WebProjectRelease, error)
	queryProjects func([]int64) ([]*model.WebProject, error)
}

type auditReferences struct {
	releases map[int64]*model.WebProjectRelease
	projects map[int64]*model.WebProject
}

// AuditStorage verifies storage ownership with two bounded single-table query
// streams. The owner encoded in a filesystem path is never treated as trusted.
func AuditStorage(conf *config.WebProjectsConfig, minAge time.Duration) (*StorageAuditReport, error) {
	webProjectRepository := repository.New()
	return auditStorageAt(conf, minAge, time.Now(), auditReferenceQueries{
		queryReleases: webProjectRepository.FindReleaseReferences,
		queryProjects: webProjectRepository.FindProjectOwnerReferences,
	})
}

// auditStorageAt 按给定时钟扫描达到最小年龄的发布目录，核对数据库引用与所有权并生成排序后的只读审计报告。
func auditStorageAt(conf *config.WebProjectsConfig, minAge time.Duration, now time.Time, queries auditReferenceQueries) (*StorageAuditReport, error) {
	if conf == nil || !filepath.IsAbs(conf.StorageRoot) || minAge < 0 || queries.queryReleases == nil || queries.queryProjects == nil {
		return nil, ErrInvalid
	}
	root, err := resolveAuditStorageRoot(conf.StorageRoot)
	if err != nil {
		return nil, err
	}
	report := &StorageAuditReport{
		GeneratedAt:   now,
		StorageRoot:   root,
		MinAgeSeconds: int64(minAge / time.Second),
		Candidates:    make([]AuditCandidate, 0),
	}
	directories, err := scanAuditReleaseDirectories(root, minAge, now, &report.Stats)
	if err != nil {
		return nil, err
	}
	references, err := loadAuditReferences(directories, queries, &report.Stats)
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		candidate, referenced := inspectAuditReference(directory, references)
		if referenced {
			report.Stats.ReferencedReleaseDirectories++
			continue
		}
		report.Candidates = append(report.Candidates, candidate)
	}
	sort.Slice(report.Candidates, func(i, j int) bool {
		left := report.Candidates[i]
		right := report.Candidates[j]
		if left.ProjectID != right.ProjectID {
			leftProject, _ := strconv.ParseInt(left.ProjectID, 10, 64)
			rightProject, _ := strconv.ParseInt(right.ProjectID, 10, 64)
			return leftProject < rightProject
		}
		if left.ReleaseID != right.ReleaseID {
			leftRelease, _ := strconv.ParseInt(left.ReleaseID, 10, 64)
			rightRelease, _ := strconv.ParseInt(right.ReleaseID, 10, 64)
			return leftRelease < rightRelease
		}
		return left.RelativePath < right.RelativePath
	})
	report.Stats.CandidateDirectories = len(report.Candidates)
	return report, nil
}

func resolveAuditStorageRoot(storageRoot string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Clean(storageRoot))
	if err != nil {
		return "", fmt.Errorf("resolve storage root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return "", fmt.Errorf("storage root is not a directory")
	}
	return root, nil
}

// loadAuditReferences 将目录中的版本 ID 去重后分批查版本，再按真实项目 ID 分批查所有权，任一查询失败即停止审计。
func loadAuditReferences(directories []auditReleaseDirectory, queries auditReferenceQueries, stats *AuditStats) (*auditReferences, error) {
	releaseIDs := make([]int64, 0, len(directories))
	seenReleaseIDs := make(map[int64]struct{}, len(directories))
	for _, directory := range directories {
		if _, exists := seenReleaseIDs[directory.releaseID]; exists {
			continue
		}
		seenReleaseIDs[directory.releaseID] = struct{}{}
		releaseIDs = append(releaseIDs, directory.releaseID)
	}
	sort.Slice(releaseIDs, func(i, j int) bool { return releaseIDs[i] < releaseIDs[j] })

	releases := make(map[int64]*model.WebProjectRelease, len(releaseIDs))
	for start := 0; start < len(releaseIDs); start += auditReferenceBatchSize {
		end := start + auditReferenceBatchSize
		if end > len(releaseIDs) {
			end = len(releaseIDs)
		}
		rows, err := queries.queryReleases(releaseIDs[start:end])
		if err != nil {
			return nil, fmt.Errorf("%w: release references: %v", ErrAuditDatabase, err)
		}
		stats.DatabaseBatches++
		for _, release := range rows {
			if release != nil {
				releases[release.ID] = release
			}
		}
	}

	projectIDs := make([]int64, 0, len(releases))
	seenProjectIDs := make(map[int64]struct{}, len(releases))
	for _, release := range releases {
		if release.ProjectID <= 0 {
			continue
		}
		if _, exists := seenProjectIDs[release.ProjectID]; exists {
			continue
		}
		seenProjectIDs[release.ProjectID] = struct{}{}
		projectIDs = append(projectIDs, release.ProjectID)
	}
	sort.Slice(projectIDs, func(i, j int) bool { return projectIDs[i] < projectIDs[j] })

	projects := make(map[int64]*model.WebProject, len(projectIDs))
	for start := 0; start < len(projectIDs); start += auditReferenceBatchSize {
		end := start + auditReferenceBatchSize
		if end > len(projectIDs) {
			end = len(projectIDs)
		}
		rows, err := queries.queryProjects(projectIDs[start:end])
		if err != nil {
			return nil, fmt.Errorf("%w: project references: %v", ErrAuditDatabase, err)
		}
		stats.DatabaseBatches++
		for _, project := range rows {
			if project != nil {
				projects[project.ID] = project
			}
		}
	}
	return &auditReferences{releases: releases, projects: projects}, nil
}

// inspectAuditReference 逐项核对目录、发布记录与项目的归属和存储键，返回首个不一致原因或已确认引用标记。
func inspectAuditReference(directory auditReleaseDirectory, references *auditReferences) (AuditCandidate, bool) {
	candidate := AuditCandidate{
		ProjectID:           strconv.FormatInt(directory.projectID, 10),
		ReleaseID:           strconv.FormatInt(directory.releaseID, 10),
		RelativePath:        directory.relativePath,
		ExpectedStorageKey:  directory.storageKey,
		DirectoryModifyTime: directory.modifyTime,
	}
	if directory.ownerID > 0 {
		candidate.DirectoryOwnerID = strconv.FormatInt(directory.ownerID, 10)
	}
	release := references.releases[directory.releaseID]
	if release == nil {
		candidate.Reason = AuditReasonMissingDatabaseReference
		return candidate, false
	}
	candidate.DatabaseStorageKey = release.StorageKey
	if release.ProjectID != directory.projectID {
		candidate.Reason = AuditReasonProjectMismatch
		return candidate, false
	}
	project := references.projects[release.ProjectID]
	if project == nil {
		candidate.Reason = AuditReasonMissingProjectReference
		return candidate, false
	}
	if project.OwnerUserID <= 0 || release.UploadedBy <= 0 || project.OwnerUserID != release.UploadedBy {
		candidate.Reason = AuditReasonOwnerMismatch
		return candidate, false
	}
	candidate.OwnerID = strconv.FormatInt(project.OwnerUserID, 10)
	if directory.ownerID > 0 && directory.ownerID != project.OwnerUserID {
		candidate.Reason = AuditReasonDirectoryOwnerMismatch
		return candidate, false
	}
	if release.StorageKey != directory.storageKey {
		candidate.Reason = AuditReasonStorageKeyMismatch
		return candidate, false
	}
	return candidate, true
}

// scanAuditReleaseDirectories reads metadata for both legacy and owner-scoped
// layouts. It does not open hosted content and never follows a symlink.
func scanAuditReleaseDirectories(root string, minAge time.Duration, now time.Time, stats *AuditStats) ([]auditReleaseDirectory, error) {
	directories := make([]auditReleaseDirectory, 0)
	legacyRoot := filepath.Join(root, "projects")
	if entries, ok := auditDirectoryEntries(legacyRoot, stats); ok {
		directories = append(directories, scanAuditProjects(root, legacyRoot, 0, entries, minAge, now, stats)...)
	}

	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read storage root: %w", err)
	}
	for _, ownerEntry := range rootEntries {
		if ownerEntry.Name() == "projects" || ownerEntry.Name() == "staging" {
			continue
		}
		stats.OwnerEntries++
		ownerID, validOwner := generatedDirectoryID(ownerEntry.Name())
		ownerInfo, infoErr := ownerEntry.Info()
		if infoErr == nil && ownerInfo.Mode()&os.ModeSymlink != 0 {
			stats.SkippedSymlinks++
			continue
		}
		if !validOwner || infoErr != nil || !ownerInfo.IsDir() {
			stats.SkippedAbnormalEntries++
			continue
		}
		uploadRoot := filepath.Join(root, ownerEntry.Name(), "upload")
		if _, ok := auditDirectoryEntries(uploadRoot, stats); !ok {
			continue
		}
		htmlRoot := filepath.Join(uploadRoot, "html")
		projectEntries, ok := auditDirectoryEntries(htmlRoot, stats)
		if !ok {
			continue
		}
		filtered := make([]os.DirEntry, 0, len(projectEntries))
		for _, entry := range projectEntries {
			if entry.Name() != ".staging" && entry.Name() != ".migration" {
				filtered = append(filtered, entry)
			}
		}
		directories = append(directories, scanAuditProjects(root, htmlRoot, ownerID, filtered, minAge, now, stats)...)
	}
	return directories, nil
}

// auditDirectoryEntries 仅枚举真实目录，将符号链接与异常条目计入跳过统计；路径不存在时直接跳过。
func auditDirectoryEntries(path string, stats *AuditStats) ([]os.DirEntry, bool) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		stats.SkippedAbnormalEntries++
		return nil, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		stats.SkippedSymlinks++
		return nil, false
	}
	if !info.IsDir() {
		stats.SkippedAbnormalEntries++
		return nil, false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		stats.SkippedAbnormalEntries++
		return nil, false
	}
	return entries, true
}

// scanAuditProjects 遍历项目和版本目录，排除异常、符号链接与近期目录，再按新旧布局生成待核对的存储键。
func scanAuditProjects(root, projectsRoot string, ownerID int64, projectEntries []os.DirEntry, minAge time.Duration, now time.Time, stats *AuditStats) []auditReleaseDirectory {
	directories := make([]auditReleaseDirectory, 0)
	for _, projectEntry := range projectEntries {
		stats.ProjectEntries++
		projectID, validProject := generatedDirectoryID(projectEntry.Name())
		projectInfo, infoErr := projectEntry.Info()
		if infoErr == nil && projectInfo.Mode()&os.ModeSymlink != 0 {
			stats.SkippedSymlinks++
			continue
		}
		if !validProject || infoErr != nil || !projectInfo.IsDir() {
			stats.SkippedAbnormalEntries++
			continue
		}
		releasesRoot := filepath.Join(projectsRoot, projectEntry.Name(), "releases")
		releaseEntries, ok := auditDirectoryEntries(releasesRoot, stats)
		if !ok {
			continue
		}
		for _, releaseEntry := range releaseEntries {
			stats.ReleaseEntries++
			releaseID, validRelease := generatedDirectoryID(releaseEntry.Name())
			releasePath := filepath.Join(releasesRoot, releaseEntry.Name())
			if !validRelease {
				stats.SkippedAbnormalEntries++
				continue
			}
			if !auditReleasePath(releasePath, releaseEntry, stats) {
				continue
			}
			releaseInfo, err := releaseEntry.Info()
			if err != nil {
				stats.SkippedAbnormalEntries++
				continue
			}
			if now.Sub(releaseInfo.ModTime()) < minAge {
				stats.SkippedRecentDirectories++
				continue
			}
			var storageKey string
			if ownerID > 0 {
				storageKey, err = ReleaseStorageKey(ownerID, projectID, releaseID)
			} else {
				storageKey, err = LegacyReleaseStorageKey(projectID, releaseID)
			}
			if err != nil {
				stats.SkippedAbnormalEntries++
				continue
			}
			directories = append(directories, auditReleaseDirectory{
				ownerID:      ownerID,
				projectID:    projectID,
				releaseID:    releaseID,
				relativePath: filepath.ToSlash(filepath.Dir(storageKey)),
				storageKey:   storageKey,
				modifyTime:   releaseInfo.ModTime(),
			})
			stats.AuditedReleaseDirectories++
		}
	}
	return directories
}

// auditReleasePath 检查版本目录和 content 是否为真实目录，并遍历元数据排除含符号链接或读取异常的版本。
func auditReleasePath(releasePath string, releaseEntry os.DirEntry, stats *AuditStats) bool {
	releaseInfo, err := releaseEntry.Info()
	if err == nil && releaseInfo.Mode()&os.ModeSymlink != 0 {
		stats.SkippedSymlinks++
		return false
	}
	if err != nil || !releaseInfo.IsDir() {
		stats.SkippedAbnormalEntries++
		return false
	}
	contentInfo, err := os.Lstat(filepath.Join(releasePath, "content"))
	if err == nil && contentInfo.Mode()&os.ModeSymlink != 0 {
		stats.SkippedSymlinks++
		return false
	}
	if err != nil || !contentInfo.IsDir() {
		stats.SkippedAbnormalEntries++
		return false
	}
	hasSymlink := false
	walkErr := filepath.WalkDir(releasePath, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			hasSymlink = true
			return fs.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		stats.SkippedAbnormalEntries++
		return false
	}
	if hasSymlink {
		stats.SkippedSymlinks++
		return false
	}
	return true
}

func generatedDirectoryID(name string) (int64, bool) {
	id, err := strconv.ParseInt(name, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == name
}
