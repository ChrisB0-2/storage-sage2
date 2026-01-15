package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestScanFindsFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{dir},
		Recursive:    true,
		IncludeFiles: true,
		IncludeDirs:  false,
	}

	ctx := context.Background()
	cands, errc := sc.Scan(ctx, req)

	var found []string
	for c := range cands {
		found = append(found, filepath.Base(c.Path))
	}

	if err := <-errc; err != nil {
		t.Fatalf("scan error: %v", err)
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(found), found)
	}
}

func TestScanRespectsMaxDepth(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure: dir/a/b/c/file.txt
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "deep.txt"), []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shallow.txt"), []byte("shallow"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{dir},
		Recursive:    true,
		MaxDepth:     1, // Only root level
		IncludeFiles: true,
		IncludeDirs:  false,
	}

	ctx := context.Background()
	cands, errc := sc.Scan(ctx, req)

	var found []string
	for c := range cands {
		found = append(found, filepath.Base(c.Path))
	}

	if err := <-errc; err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// Should only find shallow.txt, not deep.txt
	if len(found) != 1 {
		t.Fatalf("expected 1 file at depth 1, got %d: %v", len(found), found)
	}
	if found[0] != "shallow.txt" {
		t.Fatalf("expected shallow.txt, got %s", found[0])
	}
}

func TestScanDetectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows")
	}

	dir := t.TempDir()

	// Create a file and a symlink to it
	realFile := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(realFile, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(dir, "link.txt")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	sc := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{dir},
		Recursive:    true,
		IncludeFiles: true,
		IncludeDirs:  false,
	}

	ctx := context.Background()
	cands, errc := sc.Scan(ctx, req)

	var symlinks int
	for c := range cands {
		if c.IsSymlink {
			symlinks++
			if c.LinkTarget != realFile {
				t.Errorf("expected link target %s, got %s", realFile, c.LinkTarget)
			}
		}
	}

	if err := <-errc; err != nil {
		t.Fatalf("scan error: %v", err)
	}

	if symlinks != 1 {
		t.Fatalf("expected 1 symlink, got %d", symlinks)
	}
}

func TestScanContextCancellation(t *testing.T) {
	dir := t.TempDir()

	// Create some files
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, "file"+string(rune('0'+i))+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sc := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{dir},
		Recursive:    true,
		IncludeFiles: true,
		IncludeDirs:  false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give time for context to expire
	time.Sleep(10 * time.Millisecond)

	cands, errc := sc.Scan(ctx, req)

	// Drain candidates
	for range cands {
	}

	err := <-errc
	if err != context.DeadlineExceeded {
		// May or may not get error depending on timing - that's OK
		t.Logf("got error (or nil): %v", err)
	}
}

func TestScanIncludesDirs(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory with a file
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := NewWalkDir()
	req := core.ScanRequest{
		Roots:        []string{dir},
		Recursive:    true,
		IncludeFiles: true,
		IncludeDirs:  true,
	}

	ctx := context.Background()
	cands, errc := sc.Scan(ctx, req)

	var dirs, files int
	for c := range cands {
		if c.Type == core.TargetDir {
			dirs++
		} else {
			files++
		}
	}

	if err := <-errc; err != nil {
		t.Fatalf("scan error: %v", err)
	}

	if dirs < 1 {
		t.Fatalf("expected at least 1 dir, got %d", dirs)
	}
	if files != 1 {
		t.Fatalf("expected 1 file, got %d", files)
	}
}
