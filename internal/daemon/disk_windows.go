//go:build windows

package daemon

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// getDiskUsagePercent returns the disk usage percentage for the given path.
func getDiskUsagePercent(path string) (float64, error) {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	err = windows.GetDiskFreeSpaceEx(
		pathPtr,
		(*uint64)(unsafe.Pointer(&freeBytesAvailable)),
		(*uint64)(unsafe.Pointer(&totalBytes)),
		(*uint64)(unsafe.Pointer(&totalFreeBytes)),
	)
	if err != nil {
		return 0, err
	}

	if totalBytes == 0 {
		return 0, nil
	}

	used := totalBytes - totalFreeBytes
	usedPct := (float64(used) / float64(totalBytes)) * 100.0
	return usedPct, nil
}
