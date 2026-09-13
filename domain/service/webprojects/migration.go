package webprojects

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	repository "github.com/mcoder2014/home_server/domain/repository/webprojects"
)

const migrationJournalVersion = 1

const (
	MigrationStatusPlanned         = "planned"
	MigrationStatusMigrated        = "migrated"
	MigrationStatusRejected        = "rejected"
	MigrationStatusPendingDatabase = "pending_database"

	MigrationReasonTargetCollision = "target_collision"
	MigrationReasonSymlink         = "symlink"
	MigrationReasonSourceMissing   = "source_missing"
	MigrationReasonStaleMetadata   = "stale_metadata"
	MigrationReasonInvalidJournal  = "invalid_journal"
)

var (
	ErrStorageMigrationRejected = errors.New("web project storage migration rejected")
	ErrStorageMigrationCAS      = errors.New("web project storage migration compare-and-swap failed")
)

type StorageMigrationAction struct {
	OwnerID          string `json:"owner_id,omitempty"`
	ProjectID        string `json:"project_id"`
	ReleaseID        string `json:"release_id"`
	SourceStorageKey string `json:"source_storage_key"`
	TargetStorageKey string `json:"target_storage_key,omitempty"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
}

type StorageMigrationReport struct {
	GeneratedAt time.Time                `json:"generated_at"`
	StorageRoot string                   `json:"storage_root"`
	Apply       bool                     `json:"apply"`
	Actions     []StorageMigrationAction `json:"actions"`
}

type storageMigrationQueries struct {
	auditReferenceQueries
	compareAndSwapStorageKey func(int64, int64, int64, string, string) (bool, error)
}

type storageFileIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type storageMigrationJournal struct {
	Version          int                 `json:"version"`
	OwnerID          int64               `json:"owner_id"`
	ProjectID        int64               `json:"project_id"`
	ReleaseID        int64               `json:"release_id"`
	SourceStorageKey string              `json:"source_storage_key"`
	TargetStorageKey string              `json:"target_storage_key"`
	SourceIdentity   storageFileIdentity `json:"source_identity"`
	State            string              `json:"state"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type storageMigrationPlan struct {
	action         StorageMigrationAction
	release        *model.WebProjectRelease
	project        *model.WebProject
	sourcePath     string
	targetPath     string
	journalPath    string
	sourceIdentity storageFileIdentity
	journal        *storageMigrationJournal
}

// MigrateLegacyReleaseStorage migrates exactly one release. The process running
// the service and every upload writer must remain stopped throughout apply mode.
func MigrateLegacyReleaseStorage(conf *config.WebProjectsConfig, projectID, releaseID int64, apply bool) (*StorageMigrationReport, error) {
	webProjectRepository := repository.New()
	return migrateLegacyReleaseStorageAt(conf, projectID, releaseID, apply, time.Now(), storageMigrationQueries{
		auditReferenceQueries: auditReferenceQueries{
			queryReleases: webProjectRepository.FindReleaseReferences,
			queryProjects: webProjectRepository.FindProjectOwnerReferences,
		},
		compareAndSwapStorageKey: webProjectRepository.CompareAndSwapReleaseStorageKey,
	})
}

func migrateLegacyReleaseStorageAt(conf *config.WebProjectsConfig, projectID, releaseID int64, apply bool, now time.Time, queries storageMigrationQueries) (*StorageMigrationReport, error) {
	if conf == nil || !filepath.IsAbs(conf.StorageRoot) || projectID <= 0 || releaseID <= 0 ||
		queries.queryReleases == nil || queries.queryProjects == nil || queries.compareAndSwapStorageKey == nil {
		return nil, ErrInvalid
	}
	root, err := resolveAuditStorageRoot(conf.StorageRoot)
	if err != nil {
		return nil, err
	}
	report := &StorageMigrationReport{
		GeneratedAt: now,
		StorageRoot: root,
		Apply:       apply,
		Actions:     make([]StorageMigrationAction, 1),
	}
	plan, err := loadStorageMigrationPlan(conf, projectID, releaseID, queries.auditReferenceQueries)
	if err != nil {
		return report, err
	}
	report.Actions[0] = plan.action
	if plan.action.Status == MigrationStatusRejected {
		return report, ErrStorageMigrationRejected
	}
	if !apply {
		return report, nil
	}

	// Re-read both tables immediately before mutation. A dry-run plan is never
	// reused after ownership, key, or filesystem identity changes.
	plan, err = loadStorageMigrationPlan(conf, projectID, releaseID, queries.auditReferenceQueries)
	if err != nil {
		return report, err
	}
	report.Actions[0] = plan.action
	if plan.action.Status == MigrationStatusRejected {
		return report, ErrStorageMigrationRejected
	}
	if err := executeStorageMigrationPlan(conf, plan, now, queries); err != nil {
		report.Actions[0] = plan.action
		return report, err
	}
	report.Actions[0] = plan.action
	return report, nil
}

func loadStorageMigrationPlan(conf *config.WebProjectsConfig, projectID, releaseID int64, queries auditReferenceQueries) (*storageMigrationPlan, error) {
	oldKey, _ := LegacyReleaseStorageKey(projectID, releaseID)
	plan := &storageMigrationPlan{action: StorageMigrationAction{
		ProjectID:        strconv.FormatInt(projectID, 10),
		ReleaseID:        strconv.FormatInt(releaseID, 10),
		SourceStorageKey: oldKey,
		Status:           MigrationStatusRejected,
	}}
	releases, err := queries.queryReleases([]int64{releaseID})
	if err != nil {
		return plan, fmt.Errorf("%w: query release: %v", ErrAuditDatabase, err)
	}
	if len(releases) != 1 || releases[0] == nil {
		plan.action.Reason = AuditReasonMissingDatabaseReference
		return plan, nil
	}
	plan.release = releases[0]
	if plan.release.ProjectID != projectID {
		plan.action.Reason = AuditReasonProjectMismatch
		return plan, nil
	}
	projects, err := queries.queryProjects([]int64{projectID})
	if err != nil {
		return plan, fmt.Errorf("%w: query project: %v", ErrAuditDatabase, err)
	}
	if len(projects) != 1 || projects[0] == nil {
		plan.action.Reason = AuditReasonMissingProjectReference
		return plan, nil
	}
	plan.project = projects[0]
	if plan.project.OwnerUserID <= 0 || plan.release.UploadedBy <= 0 || plan.project.OwnerUserID != plan.release.UploadedBy {
		plan.action.Reason = AuditReasonOwnerMismatch
		return plan, nil
	}
	newKey, err := ReleaseStorageKey(plan.project.OwnerUserID, projectID, releaseID)
	if err != nil {
		plan.action.Reason = MigrationReasonStaleMetadata
		return plan, nil
	}
	plan.action.OwnerID = strconv.FormatInt(plan.project.OwnerUserID, 10)
	plan.action.TargetStorageKey = newKey
	sourcePath, _, sourceErr := storageDirectory(conf, path.Dir(oldKey), false)
	targetPath, _, targetErr := storageDirectory(conf, path.Dir(newKey), false)
	journalRelative := plan.action.OwnerID + "/upload/html/.migration"
	journalDirectory, _, journalErr := storageDirectory(conf, journalRelative, false)
	if sourceErr != nil || targetErr != nil || journalErr != nil {
		plan.action.Reason = MigrationReasonSymlink
		return plan, nil
	}
	plan.sourcePath = sourcePath
	plan.targetPath = targetPath
	plan.journalPath = filepath.Join(journalDirectory, migrationJournalName(projectID, releaseID))
	journal, journalErr := loadStorageMigrationJournal(plan.journalPath)
	if journalErr != nil {
		plan.action.Reason = MigrationReasonInvalidJournal
		return plan, nil
	}
	plan.journal = journal
	if journal != nil {
		if !journalMatches(*journal, plan.project.OwnerUserID, projectID, releaseID, oldKey, newKey) {
			plan.action.Reason = MigrationReasonInvalidJournal
			return plan, nil
		}
		plan.sourceIdentity = journal.SourceIdentity
	}
	if reason := validateStorageMigrationState(plan); reason != "" {
		plan.action.Reason = reason
		return plan, nil
	}
	plan.action.Status = MigrationStatusPlanned
	return plan, nil
}

func validateStorageMigrationState(plan *storageMigrationPlan) string {
	sourceExists, sourceIdentity, sourceReason := inspectMigrationReleaseDirectory(plan.sourcePath)
	if sourceReason != "" {
		return sourceReason
	}
	targetExists, targetIdentity, targetReason := inspectMigrationReleaseDirectory(plan.targetPath)
	if targetReason != "" {
		return targetReason
	}
	if plan.journal == nil {
		if plan.release.StorageKey != plan.action.SourceStorageKey {
			return AuditReasonStorageKeyMismatch
		}
		if !sourceExists {
			return MigrationReasonSourceMissing
		}
		if targetExists {
			return MigrationReasonTargetCollision
		}
		plan.sourceIdentity = sourceIdentity
		return ""
	}
	if plan.release.StorageKey != plan.action.SourceStorageKey && plan.release.StorageKey != plan.action.TargetStorageKey {
		return MigrationReasonStaleMetadata
	}
	if sourceExists && targetExists {
		return MigrationReasonTargetCollision
	}
	if !sourceExists && !targetExists {
		return MigrationReasonSourceMissing
	}
	if sourceExists && (sourceIdentity != plan.sourceIdentity || plan.release.StorageKey != plan.action.SourceStorageKey || plan.journal.State != "prepared") {
		return MigrationReasonStaleMetadata
	}
	if targetExists && targetIdentity != plan.sourceIdentity {
		return MigrationReasonTargetCollision
	}
	return ""
}

func inspectMigrationReleaseDirectory(directory string) (bool, storageFileIdentity, string) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, storageFileIdentity{}, ""
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false, storageFileIdentity{}, MigrationReasonSymlink
	}
	if !info.IsDir() {
		return false, storageFileIdentity{}, MigrationReasonStaleMetadata
	}
	contentInfo, err := os.Lstat(filepath.Join(directory, "content"))
	if err == nil && contentInfo.Mode()&os.ModeSymlink != 0 {
		return false, storageFileIdentity{}, MigrationReasonSymlink
	}
	if err != nil || !contentInfo.IsDir() {
		return false, storageFileIdentity{}, MigrationReasonStaleMetadata
	}
	hasSymlink := false
	if err := filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, walkErr error) error {
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
	}); err != nil {
		return false, storageFileIdentity{}, MigrationReasonStaleMetadata
	}
	if hasSymlink {
		return false, storageFileIdentity{}, MigrationReasonSymlink
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		return false, storageFileIdentity{}, MigrationReasonStaleMetadata
	}
	return true, storageFileIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, ""
}

// executeStorageMigrationPlan leaves target and journal untouched after rename
// when the database outcome is unknown, so a process-interrupted run can resume.
func executeStorageMigrationPlan(conf *config.WebProjectsConfig, plan *storageMigrationPlan, now time.Time, queries storageMigrationQueries) error {
	if plan.journal == nil {
		journalDirectory, _, err := storageDirectory(conf, plan.action.OwnerID+"/upload/html/.migration", true)
		if err != nil {
			return err
		}
		targetParent := path.Dir(path.Dir(plan.action.TargetStorageKey))
		if _, _, err := storageDirectory(conf, targetParent, true); err != nil {
			return err
		}
		sourceExists, identity, reason := inspectMigrationReleaseDirectory(plan.sourcePath)
		targetExists, _, targetReason := inspectMigrationReleaseDirectory(plan.targetPath)
		if !sourceExists || reason != "" || targetReason != "" || targetExists || identity != plan.sourceIdentity {
			plan.action.Status = MigrationStatusRejected
			if targetExists {
				plan.action.Reason = MigrationReasonTargetCollision
			} else {
				plan.action.Reason = MigrationReasonStaleMetadata
			}
			return ErrStorageMigrationRejected
		}
		journal := storageMigrationJournal{
			Version:          migrationJournalVersion,
			OwnerID:          plan.project.OwnerUserID,
			ProjectID:        plan.release.ProjectID,
			ReleaseID:        plan.release.ID,
			SourceStorageKey: plan.action.SourceStorageKey,
			TargetStorageKey: plan.action.TargetStorageKey,
			SourceIdentity:   plan.sourceIdentity,
			State:            "prepared",
			UpdatedAt:        now,
		}
		plan.journalPath = filepath.Join(journalDirectory, migrationJournalName(plan.release.ProjectID, plan.release.ID))
		if err := writeStorageMigrationJournal(plan.journalPath, journal, false); err != nil {
			return err
		}
		plan.journal = &journal
	}

	sourceExists, sourceIdentity, sourceReason := inspectMigrationReleaseDirectory(plan.sourcePath)
	targetExists, targetIdentity, targetReason := inspectMigrationReleaseDirectory(plan.targetPath)
	if sourceReason != "" || targetReason != "" || (sourceExists && sourceIdentity != plan.sourceIdentity) {
		plan.action.Status = MigrationStatusRejected
		plan.action.Reason = MigrationReasonStaleMetadata
		if sourceReason == MigrationReasonSymlink || targetReason == MigrationReasonSymlink {
			plan.action.Reason = MigrationReasonSymlink
		}
		return ErrStorageMigrationRejected
	}
	if sourceExists {
		if targetExists {
			plan.action.Status = MigrationStatusRejected
			plan.action.Reason = MigrationReasonTargetCollision
			return ErrStorageMigrationRejected
		}
		if err := os.Rename(plan.sourcePath, plan.targetPath); err != nil {
			return fmt.Errorf("rename legacy release: %w", err)
		}
		if err := syncStorageDirectory(filepath.Dir(plan.sourcePath)); err != nil {
			plan.action.Status = MigrationStatusPendingDatabase
			return fmt.Errorf("sync legacy release parent: %w", err)
		}
		if err := syncStorageDirectory(filepath.Dir(plan.targetPath)); err != nil {
			plan.action.Status = MigrationStatusPendingDatabase
			return fmt.Errorf("sync owner release parent: %w", err)
		}
		_, targetIdentity, targetReason = inspectMigrationReleaseDirectory(plan.targetPath)
		if targetReason != "" || targetIdentity != plan.sourceIdentity {
			plan.action.Status = MigrationStatusPendingDatabase
			return fmt.Errorf("%w: renamed directory identity changed", ErrStorageMigrationRejected)
		}
		plan.journal.State = "moved"
		plan.journal.UpdatedAt = time.Now()
		if err := writeStorageMigrationJournal(plan.journalPath, *plan.journal, true); err != nil {
			plan.action.Status = MigrationStatusPendingDatabase
			return err
		}
	} else if !targetExists || targetIdentity != plan.sourceIdentity {
		plan.action.Status = MigrationStatusRejected
		plan.action.Reason = MigrationReasonTargetCollision
		return ErrStorageMigrationRejected
	}

	if plan.release.StorageKey != plan.action.TargetStorageKey {
		updated, err := queries.compareAndSwapStorageKey(plan.release.ID, plan.release.ProjectID, plan.release.UploadedBy, plan.action.SourceStorageKey, plan.action.TargetStorageKey)
		if err != nil {
			plan.action.Status = MigrationStatusPendingDatabase
			return err
		}
		if !updated {
			committed, verifyErr := verifyStorageMigrationCommit(plan, queries.auditReferenceQueries)
			if verifyErr != nil {
				plan.action.Status = MigrationStatusPendingDatabase
				return verifyErr
			}
			if !committed {
				plan.action.Status = MigrationStatusPendingDatabase
				return ErrStorageMigrationCAS
			}
		}
	}
	if err := os.Remove(plan.journalPath); err != nil {
		plan.action.Status = MigrationStatusPendingDatabase
		return fmt.Errorf("remove migration journal: %w", err)
	}
	if err := syncStorageDirectory(filepath.Dir(plan.journalPath)); err != nil {
		plan.action.Status = MigrationStatusPendingDatabase
		return err
	}
	plan.action.Status = MigrationStatusMigrated
	plan.action.Reason = ""
	return nil
}

func verifyStorageMigrationCommit(plan *storageMigrationPlan, queries auditReferenceQueries) (bool, error) {
	releases, err := queries.queryReleases([]int64{plan.release.ID})
	if err != nil {
		return false, fmt.Errorf("%w: verify release storage key: %v", ErrAuditDatabase, err)
	}
	projects, err := queries.queryProjects([]int64{plan.project.ID})
	if err != nil {
		return false, fmt.Errorf("%w: verify project owner: %v", ErrAuditDatabase, err)
	}
	if len(releases) != 1 || releases[0] == nil || len(projects) != 1 || projects[0] == nil {
		return false, nil
	}
	return releases[0].ProjectID == projects[0].ID &&
		releases[0].UploadedBy == projects[0].OwnerUserID &&
		projects[0].OwnerUserID == plan.project.OwnerUserID &&
		releases[0].StorageKey == plan.action.TargetStorageKey, nil
}

func loadStorageMigrationJournal(journalPath string) (*storageMigrationJournal, error) {
	info, err := os.Lstat(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrStorageMigrationRejected
	}
	file, err := os.Open(journalPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024))
	decoder.DisallowUnknownFields()
	var journal storageMigrationJournal
	if err := decoder.Decode(&journal); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrStorageMigrationRejected
	}
	return &journal, nil
}

func journalMatches(journal storageMigrationJournal, ownerID, projectID, releaseID int64, sourceKey, targetKey string) bool {
	return journal.Version == migrationJournalVersion &&
		journal.OwnerID == ownerID && journal.ProjectID == projectID && journal.ReleaseID == releaseID &&
		journal.SourceStorageKey == sourceKey && journal.TargetStorageKey == targetKey &&
		journal.SourceIdentity.Inode > 0 && (journal.State == "prepared" || journal.State == "moved")
}

func writeStorageMigrationJournal(journalPath string, journal storageMigrationJournal, replace bool) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	tempPath := journalPath + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if replace {
		err = os.Rename(tempPath, journalPath)
	} else {
		err = os.Link(tempPath, journalPath)
	}
	if err != nil {
		return err
	}
	if !replace {
		if err := os.Remove(tempPath); err != nil {
			return err
		}
	}
	return syncStorageDirectory(filepath.Dir(journalPath))
}

func migrationJournalName(projectID, releaseID int64) string {
	return strconv.FormatInt(projectID, 10) + "-" + strconv.FormatInt(releaseID, 10) + ".json"
}
