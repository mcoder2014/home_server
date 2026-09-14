package webprojects

import (
	"fmt"
	"math"
	"syscall"
)

// webDiskFreeBytes checks the actual filesystem containing storage_root, using
// blocks available to the service user instead of privileged reserved blocks.
func webDiskFreeBytes(root string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || uint64(stat.Bavail) > math.MaxUint64/uint64(stat.Bsize) {
		return 0, fmt.Errorf("invalid filesystem free-space result")
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
