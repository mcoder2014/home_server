package fileshare

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/config"
)

func TestPrivateStorageRejectsOverlapAndSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0700); err != nil {
		t.Fatal(err)
	}
	conf := config.FileSharingConfig{Enabled: true, StorageRoot: filepath.Join(shared, "files")}
	if err := Init(&conf, shared); err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("nested private root was accepted: %v", err)
	}
	conf.StorageRoot = filepath.Join(root, "private")
	if err := Init(&conf, shared); err != nil {
		t.Fatal(err)
	}
	stored, err := StoreUpload(&conf, 7, 9, strings.NewReader("ordinary bytes"))
	if err != nil {
		t.Fatal(err)
	}
	file, info, err := OpenStoredFile(&conf, stored.StorageKey)
	if err != nil || info.Size() != int64(len("ordinary bytes")) {
		t.Fatalf("stored upload cannot be opened: %v", err)
	}
	_ = file.Close()
	path := filepath.Join(conf.StorageRoot, filepath.FromSlash(stored.StorageKey))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(shared, "outside"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenStoredFile(&conf, stored.StorageKey); err != ErrNotFound {
		t.Fatalf("symlink replacement was not hidden: %v", err)
	}
}

func TestPrivateStorageAcceptsEmptyFile(t *testing.T) {
	conf := config.FileSharingConfig{Enabled: true, StorageRoot: filepath.Join(t.TempDir(), "private")}
	if err := Init(&conf); err != nil {
		t.Fatal(err)
	}
	stored, err := StoreUpload(&conf, 7, 10, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if stored.SizeBytes != 0 || stored.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty file metadata = %#v", stored)
	}
	file, info, err := OpenStoredFile(&conf, stored.StorageKey)
	if err != nil || info.Size() != 0 {
		t.Fatalf("empty file cannot be opened: info=%#v err=%v", info, err)
	}
	_ = file.Close()
}
