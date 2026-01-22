//go:build unix

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDeviceID_Unix(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	deviceID, ok := getDeviceID(info)
	if !ok {
		t.Fatal("expected getDeviceID to return true on Unix")
	}

	if deviceID == 0 {
		t.Error("expected non-zero device ID")
	}
}

func TestGetDeviceID_SameFilesystem(t *testing.T) {
	// Create two temp files in the same directory (same filesystem)
	dir := t.TempDir()
	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")

	if err := os.WriteFile(file1, []byte("test1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("test2"), 0o644); err != nil {
		t.Fatal(err)
	}

	info1, err := os.Lstat(file1)
	if err != nil {
		t.Fatal(err)
	}
	info2, err := os.Lstat(file2)
	if err != nil {
		t.Fatal(err)
	}

	dev1, ok1 := getDeviceID(info1)
	dev2, ok2 := getDeviceID(info2)

	if !ok1 || !ok2 {
		t.Fatal("expected getDeviceID to return true for both files")
	}

	if dev1 != dev2 {
		t.Errorf("expected same device ID for files in same directory: %d != %d", dev1, dev2)
	}
}

func TestGetDeviceID_DirectoryAndFile(t *testing.T) {
	// Create a directory and a file within it
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Lstat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	dirDev, dirOK := getDeviceID(dirInfo)
	fileDev, fileOK := getDeviceID(fileInfo)

	if !dirOK || !fileOK {
		t.Fatal("expected getDeviceID to return true for both dir and file")
	}

	// Directory and file in same filesystem should have same device ID
	if dirDev != fileDev {
		t.Errorf("expected same device ID for dir and file in same filesystem: %d != %d", dirDev, fileDev)
	}
}
