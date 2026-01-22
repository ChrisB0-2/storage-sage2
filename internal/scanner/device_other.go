//go:build !unix

package scanner

import "os"

// getDeviceID is a no-op on non-Unix systems.
func getDeviceID(info os.FileInfo) (uint64, bool) {
	return 0, false
}
