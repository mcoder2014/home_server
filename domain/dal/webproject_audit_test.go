package dal

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var auditTestIDCounter int64

func TestQueryWebProjectReleaseReferencesSelectsRequestedRows(t *testing.T) {
	database := requireAuditTestDB(t)
	baseID := time.Now().UnixNano()/1000 + atomic.AddInt64(&auditTestIDCounter, 10)
	project := &model.WebProject{
		ID: baseID, OwnerUserID: baseID, Name: "audit-test", Description: "audit-test",
		Slug: fmt.Sprintf("audit-%d", baseID), AccessMode: model.WebProjectAccessOwner, Status: model.WebProjectStatusDraft, Revision: 1,
		CreateTime: time.Now(), UpdateTime: time.Now(),
	}
	release := &model.WebProjectRelease{
		ID: baseID + 1, ProjectID: project.ID, UploadedBy: project.ID,
		StorageKey: fmt.Sprintf("projects/%d/releases/%d/content", project.ID, baseID+1),
		Status:     model.WebProjectReleaseReady, EntryFile: "index.html", SHA256: fmt.Sprintf("%064x", baseID+1),
		FileCount: 1, TotalBytes: 1, CreateTime: time.Now(), UpdateTime: time.Now(),
	}
	require.NoError(t, database.Table(WebProjectTable).Create(project).Error)
	require.NoError(t, database.Table(WebProjectReleaseTable).Create(release).Error)
	t.Cleanup(func() {
		database.Table(WebProjectReleaseTable).Where("project_id = ?", project.ID).Delete(&model.WebProjectRelease{})
		database.Table(WebProjectTable).Where("id = ?", project.ID).Delete(&model.WebProject{})
	})

	references, err := QueryWebProjectReleaseReferences([]int64{release.ID, release.ID + 100})
	require.NoError(t, err)
	require.Len(t, references, 1)
	require.Equal(t, release.ID, references[0].ID)
	require.Equal(t, release.ProjectID, references[0].ProjectID)
	require.Equal(t, release.StorageKey, references[0].StorageKey)
}

func requireAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HOME_SERVER_AUDIT_TEST_DSN")
	if dsn == "" {
		t.Skip("HOME_SERVER_AUDIT_TEST_DSN is required for audit integration tests")
	}
	require.NoError(t, db.InitDatabase(dsn))
	return db.MasterDB()
}
