//go:build unix

package daemon

import (
	"fmt"
	"syscall"
)

// getDiskUsagePercent returns the disk usage percentage for the given path.
func getDiskUsagePercent(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}

	// Bsize is int64 on Linux; ensure it's positive before converting to uint64
	if stat.Bsize <= 0 {
		return 0, fmt.Errorf("invalid block size: %d", stat.Bsize)
	}
	bsize := uint64(stat.Bsize)

	// Total and available blocks
	total := stat.Blocks * bsize
	avail := stat.Bavail * bsize

	if total == 0 {
		return 0, nil
	}

	used := total - avail
	usedPct := (float64(used) / float64(total)) * 100.0
	return usedPct, nil
}
