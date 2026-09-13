package webprojects

import (
	"errors"
	"testing"

	"github.com/mcoder2014/home_server/config"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
)

// TestUploadAdmissionReservesOnlyWebStorageDisk 用可控磁盘余量验证预留空间、动态上限、重复释放和磁盘查询失败的上传准入行为。
func TestUploadAdmissionReservesOnlyWebStorageDisk(t *testing.T) {
	before := config.Global()
	defer config.SetGlobalConfig(before)
	config.SetGlobalConfig(config.Config{IdentitySource: "database"})
	root := t.TempDir()
	conf := &config.WebProjectsConfig{Enabled: true, StorageRoot: root, MaxExpandedBytes: 100, MinFreeDiskBytes: 50, MaxConcurrentUploadsPerUser: 3, MaxConcurrentExtracts: 3}
	application := New(nil)
	free := uint64(149)
	application.diskFree = func(path string) (uint64, error) {
		if path != root {
			t.Fatalf("checked wrong filesystem: %s", path)
		}
		return free, nil
	}
	if release, err := application.AcquireUpload(100, conf); err == nil {
		release()
		t.Fatal("accepted upload without required free space")
	}
	free = 250
	first, err := application.AcquireUpload(100, conf)
	if err != nil {
		t.Fatal(err)
	}
	conf.MinFreeDiskBytes = 51
	if release, err := application.AcquireUpload(200, conf); err == nil {
		release()
		t.Fatal("lost in-flight reservation after live limit change")
	}
	conf.MinFreeDiskBytes = 50
	second, err := application.AcquireUpload(200, conf)
	if err != nil {
		t.Fatal(err)
	}
	first()
	first()
	conf.MaxExpandedBytes = 50
	third, err := application.AcquireUpload(300, conf)
	if err != nil {
		t.Fatalf("released reservation was not reusable: %v", err)
	}
	second()
	third()
	application.diskFree = func(string) (uint64, error) { return 0, errors.New("disk lookup failed") }
	if _, err := application.AcquireUpload(100, conf); !errors.Is(err, service.ErrDependency) {
		t.Fatalf("disk read failure should reject admission, got %v", err)
	}
}

// TestDiskQuotaKeepsFileModeAndMissingRootCompatibility 验证文件身份模式或缺少存储根目录时保留旧准入行为，并检查实际临时目录的磁盘余量读取。
func TestDiskQuotaKeepsFileModeAndMissingRootCompatibility(t *testing.T) {
	before := config.Global()
	defer config.SetGlobalConfig(before)
	for _, conf := range []config.Config{{IdentitySource: "file"}, {IdentitySource: "database"}} {
		config.SetGlobalConfig(conf)
		application := New(nil)
		application.diskFree = func(string) (uint64, error) { t.Fatal("legacy admission unexpectedly checked disk"); return 0, nil }
		limits := &config.WebProjectsConfig{Enabled: true, MaxConcurrentUploadsPerUser: 1, MaxConcurrentExtracts: 1}
		if conf.IdentitySource == "file" {
			limits.StorageRoot = t.TempDir()
		}
		release, err := application.AcquireUpload(100, limits)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	free, err := webDiskFreeBytes(t.TempDir())
	if err != nil || free == 0 {
		t.Fatalf("cannot inspect actual temporary storage filesystem: %d %v", free, err)
	}
}
