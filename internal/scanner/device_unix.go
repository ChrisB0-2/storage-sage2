//go:build unix

package scanner

import (
	"os"
	"syscall"
)

// getDeviceID extracts the device ID from file stat info on Unix systems.
func getDeviceID(info os.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Dev, true
}
