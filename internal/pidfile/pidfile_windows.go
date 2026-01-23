//go:build windows

// Package pidfile provides PID file management.
// On Windows, file locking uses LockFileEx.
package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/windows"
)

// PIDFile manages a PID file with exclusive locking.
type PIDFile struct {
	path   string
	file   *os.File
	handle windows.Handle
}

// New creates and locks a PID file at the given path.
// Returns an error if another process already holds the lock.
func New(path string) (*PIDFile, error) {
	if path == "" {
		return nil, nil // No PID file requested
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating pid directory: %w", err)
	}

	// Open or create the PID file
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening pid file: %w", err)
	}

	handle := windows.Handle(file.Fd())

	// Try to acquire an exclusive lock (non-blocking)
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err != nil {
		file.Close()

		// Try to read the existing PID for a better error message
		existingPID := "unknown"
		if data, readErr := os.ReadFile(path); readErr == nil {
			existingPID = string(data)
		}

		return nil, fmt.Errorf("another instance is running (pid: %s): %w", existingPID, err)
	}

	// Truncate and write our PID
	if err := file.Truncate(0); err != nil {
		windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		file.Close()
		return nil, fmt.Errorf("truncating pid file: %w", err)
	}

	if _, err := file.Seek(0, 0); err != nil {
		windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		file.Close()
		return nil, fmt.Errorf("seeking pid file: %w", err)
	}

	pid := os.Getpid()
	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		file.Close()
		return nil, fmt.Errorf("writing pid: %w", err)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		file.Close()
		return nil, fmt.Errorf("syncing pid file: %w", err)
	}

	return &PIDFile{
		path:   path,
		file:   file,
		handle: handle,
	}, nil
}

// Close releases the lock and removes the PID file.
func (p *PIDFile) Close() error {
	if p == nil || p.file == nil {
		return nil
	}

	// Release the lock
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(p.handle, 0, 1, 0, overlapped)

	// Close the file
	if err := p.file.Close(); err != nil {
		return fmt.Errorf("closing pid file: %w", err)
	}

	// Remove the file
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing pid file: %w", err)
	}

	return nil
}

// Path returns the PID file path.
func (p *PIDFile) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

// ReadPID reads the PID from an existing PID file (for status checks).
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Trim whitespace and parse
	pidStr := string(data)
	for len(pidStr) > 0 && (pidStr[len(pidStr)-1] == '\n' || pidStr[len(pidStr)-1] == '\r') {
		pidStr = pidStr[:len(pidStr)-1]
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid pid in file: %w", err)
	}

	return pid, nil
}

// IsRunning checks if a process with the given PID is still running.
func IsRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(handle)
	return true
}
