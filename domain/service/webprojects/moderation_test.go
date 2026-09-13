package webprojects

import (
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
)

func TestRestoreUsesRecordedDeadlineAndRespectsModeration(t *testing.T) {
	before := config.Global()
	defer config.SetGlobalConfig(before)
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	now := time.Now()
	deleted := now.Add(-2 * 24 * time.Hour)
	purge := now.Add(5 * 24 * time.Hour)
	project := &model.WebProject{Status: ProjectStatusDeleted, DeletedAt: &deleted, PurgeAfter: &purge, ModerationStatus: "normal"}
	if _, err := ProjectStatusFields(project, "restore", 1, now); err != nil {
		t.Fatalf("lower retention changed existing deletion promise: %v", err)
	}
	project.PurgeAfter = nil
	if _, err := ProjectStatusFields(project, "restore", 1, now); err != nil {
		t.Fatalf("legacy tombstone lost original retention promise: %v", err)
	}
	project.PurgeAfter = &purge
	project.ModerationStatus = "blocked"
	if _, err := ProjectStatusFields(project, "restore", 7, now); err == nil {
		t.Fatal("owner bypassed moderator block through restore")
	}
	project.ModerationStatus = "normal"
	project.Status = ProjectStatusEnabled
	fields, err := ProjectStatusFields(project, "delete", 7, now)
	if err != nil {
		t.Fatal(err)
	}
	deadline, ok := fields["purge_after"].(time.Time)
	if !ok || !deadline.Equal(now.Add(7*24*time.Hour)) {
		t.Fatal("delete did not freeze purge deadline")
	}
}
