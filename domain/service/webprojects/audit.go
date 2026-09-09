package webprojects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

const (
	auditReferenceBatchSize = 200

	AuditReasonMissingDatabaseReference = "missing_database_reference"
	AuditReasonStorageKeyMismatch       = "storage_key_mismatch"
	AuditReasonProjectMismatch          = "project_mismatch"
)

var ErrAuditDatabase = errors.New("web project audit database unavailable")

type AuditCandidate struct {
	ProjectID           string    `json:"project_id"`
	ReleaseID           string    `json:"release_id"`
	RelativePath        string    `json:"relative_path"`
	ExpectedStorageKey  string    `json:"expected_storage_key"`
	DatabaseStorageKey  string    `json:"database_storage_key,omitempty"`
	Reason              string    `json:"reason"`
	DirectoryModifyTime time.Time `json:"directory_modify_time"`
}

type AuditStats struct {
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
	projectID    int64
	releaseID    int64
	relativePath string
	storageKey   string
	modifyTime   time.Time
}

type auditReferenceQuery func([]int64) ([]*model.WebProjectRelease, error)

// AuditStorage reports old generated release directories whose database
// reference is missing or inconsistent. It never changes database or disk.
func AuditStorage(conf *config.WebProjectsConfig, minAge time.Duration) (*StorageAuditReport, error) {
	return auditStorageAt(conf, minAge, time.Now(), dal.QueryWebProjectReleaseReferences)
}

func auditStorageAt(conf *config.WebProjectsConfig, minAge time.Duration, now time.Time, query auditReferenceQuery) (*StorageAuditReport, error) {
	if conf == nil || !filepath.IsAbs(conf.StorageRoot) || minAge < 0 || query == nil {
		return nil, ErrInvalid
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(conf.StorageRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return nil, fmt.Errorf("storage root is not a directory")
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

	references := make(map[int64]*model.WebProjectRelease, len(directories))
	uniqueIDs := make([]int64, 0, len(directories))
	seenIDs := make(map[int64]struct{}, len(directories))
	for _, directory := range directories {
		if _, exists := seenIDs[directory.releaseID]; !exists {
			seenIDs[directory.releaseID] = struct{}{}
			uniqueIDs = append(uniqueIDs, directory.releaseID)
		}
	}
	for start := 0; start < len(uniqueIDs); start += int(auditReferenceBatchSize) {
		end := start + int(auditReferenceBatchSize)
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		rows, queryErr := query(uniqueIDs[start:end])
		if queryErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrAuditDatabase, queryErr)
		}
		report.Stats.DatabaseBatches++
		for _, release := range rows {
			if release != nil {
				references[release.ID] = release
			}
		}
	}

	for _, directory := range directories {
		release := references[directory.releaseID]
		reason := ""
		databaseStorageKey := ""
		switch {
		case release == nil:
			reason = AuditReasonMissingDatabaseReference
		case release.ProjectID != directory.projectID:
			reason = AuditReasonProjectMismatch
			databaseStorageKey = release.StorageKey
		case release.StorageKey != directory.storageKey:
			reason = AuditReasonStorageKeyMismatch
			databaseStorageKey = release.StorageKey
		default:
			report.Stats.ReferencedReleaseDirectories++
			continue
		}
		report.Candidates = append(report.Candidates, AuditCandidate{
			ProjectID:           strconv.FormatInt(directory.projectID, 10),
			ReleaseID:           strconv.FormatInt(directory.releaseID, 10),
			RelativePath:        directory.relativePath,
			ExpectedStorageKey:  directory.storageKey,
			DatabaseStorageKey:  databaseStorageKey,
			Reason:              reason,
			DirectoryModifyTime: directory.modifyTime,
		})
	}
	sort.Slice(report.Candidates, func(i, j int) bool {
		leftProject, _ := strconv.ParseInt(report.Candidates[i].ProjectID, 10, 64)
		rightProject, _ := strconv.ParseInt(report.Candidates[j].ProjectID, 10, 64)
		if leftProject == rightProject {
			leftRelease, _ := strconv.ParseInt(report.Candidates[i].ReleaseID, 10, 64)
			rightRelease, _ := strconv.ParseInt(report.Candidates[j].ReleaseID, 10, 64)
			return leftRelease < rightRelease
		}
		return leftProject < rightProject
	})
	report.Stats.CandidateDirectories = len(report.Candidates)
	return report, nil
}

func scanAuditReleaseDirectories(root string, minAge time.Duration, now time.Time, stats *AuditStats) ([]auditReleaseDirectory, error) {
	projectsRoot := filepath.Join(root, "projects")
	projectsInfo, err := os.Lstat(projectsRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect projects directory: %w", err)
	}
	if projectsInfo.Mode()&os.ModeSymlink != 0 || !projectsInfo.IsDir() {
		return nil, fmt.Errorf("projects path is not a regular directory")
	}
	projects, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil, fmt.Errorf("read projects directory: %w", err)
	}
	directories := make([]auditReleaseDirectory, 0)
	for _, projectEntry := range projects {
		stats.ProjectEntries++
		projectID, ok := generatedDirectoryID(projectEntry.Name())
		if projectEntry.Type()&os.ModeSymlink != 0 {
			stats.SkippedSymlinks++
			continue
		}
		projectInfo, infoErr := projectEntry.Info()
		if !ok || infoErr != nil || !projectInfo.IsDir() {
			stats.SkippedAbnormalEntries++
			continue
		}
		releasesRoot := filepath.Join(projectsRoot, projectEntry.Name(), "releases")
		releasesInfo, statErr := os.Lstat(releasesRoot)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			stats.SkippedAbnormalEntries++
			continue
		}
		if releasesInfo.Mode()&os.ModeSymlink != 0 {
			stats.SkippedSymlinks++
			continue
		}
		if !releasesInfo.IsDir() {
			stats.SkippedAbnormalEntries++
			continue
		}
		releases, readErr := os.ReadDir(releasesRoot)
		if readErr != nil {
			stats.SkippedAbnormalEntries++
			continue
		}
		for _, releaseEntry := range releases {
			stats.ReleaseEntries++
			releaseID, releaseOK := generatedDirectoryID(releaseEntry.Name())
			if releaseEntry.Type()&os.ModeSymlink != 0 {
				stats.SkippedSymlinks++
				continue
			}
			releaseInfo, releaseInfoErr := releaseEntry.Info()
			if !releaseOK || releaseInfoErr != nil || !releaseInfo.IsDir() {
				stats.SkippedAbnormalEntries++
				continue
			}
			contentPath := filepath.Join(releasesRoot, releaseEntry.Name(), "content")
			contentInfo, contentErr := os.Lstat(contentPath)
			if contentErr != nil {
				stats.SkippedAbnormalEntries++
				continue
			}
			if contentInfo.Mode()&os.ModeSymlink != 0 {
				stats.SkippedSymlinks++
				continue
			}
			if !contentInfo.IsDir() {
				stats.SkippedAbnormalEntries++
				continue
			}
			if now.Sub(releaseInfo.ModTime()) < minAge {
				stats.SkippedRecentDirectories++
				continue
			}
			relativePath := filepath.ToSlash(filepath.Join("projects", projectEntry.Name(), "releases", releaseEntry.Name()))
			directories = append(directories, auditReleaseDirectory{
				projectID:    projectID,
				releaseID:    releaseID,
				relativePath: relativePath,
				storageKey:   relativePath + "/content",
				modifyTime:   releaseInfo.ModTime(),
			})
			stats.AuditedReleaseDirectories++
		}
	}
	return directories, nil
}

func generatedDirectoryID(name string) (int64, bool) {
	id, err := strconv.ParseInt(name, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == name
}
