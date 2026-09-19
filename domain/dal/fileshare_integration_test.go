package dal

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type capturedSQLLog struct {
	bytes.Buffer
}

func (capture *capturedSQLLog) Printf(format string, values ...interface{}) {
	_, _ = fmt.Fprintf(&capture.Buffer, format, values...)
}

func fileSharingTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("FILE_SHARING_TEST_DSN")
	if dsn == "" {
		t.Skip("FILE_SHARING_TEST_DSN required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "home_server_file_sharing_test_ci" {
		t.Fatal("dedicated file sharing test database required")
	}
	if err := db.InitDatabase(dsn); err != nil {
		t.Fatal(err)
	}
	database := db.MasterDB()
	for _, table := range []string{FileShareMemberTable, FileShareTable, FileTable} {
		if err := database.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := migrations.SQL.ReadFile("20260919_file_sharing.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := database.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	return database
}

func TestFileShareDownloadLimitIsAtomic(t *testing.T) {
	database := fileSharingTestDatabase(t)
	now := time.Now()
	file := &model.StoredFile{ID: 101, OwnerUserID: 7, OriginalName: "bounded.bin", StorageKey: "7/101/0123456789abcdef0123456789abcdef", SizeBytes: 10, SHA256: strings.Repeat("a", 64), CreateTime: now}
	share := &model.FileShare{ID: 201, FileID: file.ID, OwnerUserID: file.OwnerUserID, Token: strings.Repeat("A", 43), AccessMode: model.FileShareAccessPublic, SecretMode: model.FileShareSecretNone, MaxDownloads: 1, CreateTime: now, UpdateTime: now}
	if err := InsertFile(database, file); err != nil {
		t.Fatal(err)
	}
	if err := InsertFileShare(database, share, nil); err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int64
	var group sync.WaitGroup
	for index := 0; index < 24; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			err := database.Transaction(func(tx *gorm.DB) error {
				updated, err := IncrementDownloadCount(tx, share.ID, time.Now())
				if err == nil && updated {
					starts.Add(1)
				}
				return err
			})
			if err != nil {
				t.Errorf("increment failed: %v", err)
			}
		}()
	}
	group.Wait()
	if starts.Load() != 1 {
		t.Fatalf("one-download share admitted %d transfers", starts.Load())
	}
	stored, err := FindShare(database, share.ID, false)
	if err != nil || stored.DownloadCount != 1 {
		t.Fatalf("stored count = %#v, %v", stored, err)
	}
}

func TestFileShareSecretsNeverEnterGORMLogs(t *testing.T) {
	database := fileSharingTestDatabase(t)
	capture := &capturedSQLLog{}
	database = database.Session(&gorm.Session{Logger: logger.New(capture, logger.Config{LogLevel: logger.Info, Colorful: false})})
	var marker string
	if err := database.Raw("SELECT ? AS marker", "ordinary-visible-marker").Scan(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capture.String(), "ordinary-visible-marker") {
		t.Fatal("test logger did not capture SQL parameters")
	}

	now := time.Now()
	file := &model.StoredFile{ID: 301, OwnerUserID: 9, OriginalName: "private.bin", StorageKey: "9/301/0123456789abcdef0123456789abcdef", SizeBytes: 1, SHA256: strings.Repeat("b", 64), CreateTime: now}
	token := "sensitive-share-token-must-never-enter-gorm-log"
	secretHash := "$2a$04$sensitive-hash-must-never-enter-gorm-log"
	share := &model.FileShare{ID: 401, FileID: file.ID, OwnerUserID: file.OwnerUserID, Token: token, AccessMode: model.FileShareAccessAuthenticated, SecretMode: model.FileShareSecretPassword, SecretHash: secretHash, CreateTime: now, UpdateTime: now}
	if err := InsertFile(database, file); err != nil {
		t.Fatal(err)
	}
	if err := InsertFileShare(database, share, []int64{10}); err != nil {
		t.Fatal(err)
	}
	if stored, err := FindShareByToken(database, token, false); err != nil || stored == nil || stored.ID != share.ID {
		t.Fatalf("sensitive share lookup failed: share=%#v err=%v", stored, err)
	}
	logged := capture.String()
	if strings.Contains(logged, token) || strings.Contains(logged, secretHash) {
		t.Fatalf("GORM log exposed a file share credential: %s", logged)
	}
}
