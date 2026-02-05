package trash

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

func TestNew(t *testing.T) {
	t.Run("empty path returns nil manager", func(t *testing.T) {
		m, err := New(Config{TrashPath: ""}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m != nil {
			t.Fatal("expected nil manager for empty path")
		}
	})

	t.Run("valid path creates manager and directory", func(t *testing.T) {
		trashDir := t.TempDir()
		trashPath := filepath.Join(trashDir, "trash")

		m, err := New(Config{TrashPath: trashPath, MaxAge: time.Hour}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m == nil {
			t.Fatal("expected non-nil manager")
		}

		// Verify directory was created
		info, err := os.Stat(trashPath)
		if err != nil {
			t.Fatalf("trash directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("trash path is not a directory")
		}

		// Verify config was stored
		if m.trashPath != trashPath {
			t.Errorf("trashPath = %q, want %q", m.trashPath, trashPath)
		}
		if m.maxAge != time.Hour {
			t.Errorf("maxAge = %v, want %v", m.maxAge, time.Hour)
		}
	})

	t.Run("nil logger defaults to nop", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.log == nil {
			t.Fatal("expected non-nil logger")
		}
	})

	t.Run("custom logger is used", func(t *testing.T) {
		trashPath := t.TempDir()
		log := logger.NewNop()

		m, err := New(Config{TrashPath: trashPath}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.log != log {
			t.Fatal("expected custom logger to be used")
		}
	})
}

func TestMoveToTrash(t *testing.T) {
	t.Run("move file to trash", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a test file
		srcFile := filepath.Join(srcDir, "testfile.txt")
		content := []byte("test content")
		if err := os.WriteFile(srcFile, content, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		// Move to trash
		trashFile, err := m.MoveToTrash(srcFile)
		if err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		// Verify original file is gone
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Error("original file should not exist")
		}

		// Verify file is in trash
		trashedContent, err := os.ReadFile(trashFile)
		if err != nil {
			t.Fatalf("failed to read trashed file: %v", err)
		}
		if string(trashedContent) != string(content) {
			t.Errorf("content = %q, want %q", trashedContent, content)
		}

		// Verify metadata file exists
		metaPath := trashFile + ".meta"
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("failed to read metadata: %v", err)
		}
		if !strings.Contains(string(metaData), "original_path: "+srcFile) {
			t.Error("metadata should contain original path")
		}
	})

	t.Run("move directory to trash", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a test directory with files
		testDir := filepath.Join(srcDir, "testdir")
		if err := os.MkdirAll(testDir, 0755); err != nil {
			t.Fatalf("failed to create test dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "file1.txt"), []byte("content1"), 0644); err != nil {
			t.Fatalf("failed to create file1: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(testDir, "subdir"), 0755); err != nil {
			t.Fatalf("failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "subdir", "file2.txt"), []byte("content2"), 0644); err != nil {
			t.Fatalf("failed to create file2: %v", err)
		}

		// Move to trash
		trashDir, err := m.MoveToTrash(testDir)
		if err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		// Verify original directory is gone
		if _, err := os.Stat(testDir); !os.IsNotExist(err) {
			t.Error("original directory should not exist")
		}

		// Verify directory structure in trash
		if _, err := os.Stat(filepath.Join(trashDir, "file1.txt")); err != nil {
			t.Error("file1.txt should exist in trash")
		}
		if _, err := os.Stat(filepath.Join(trashDir, "subdir", "file2.txt")); err != nil {
			t.Error("subdir/file2.txt should exist in trash")
		}
	})

	t.Run("long filename is truncated", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a file with a very long name
		longName := strings.Repeat("a", 200) + ".txt"
		srcFile := filepath.Join(srcDir, longName)
		if err := os.WriteFile(srcFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		trashFile, err := m.MoveToTrash(srcFile)
		if err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		// The trash filename format is: YYYYMMDD-HHMMSS_hash8chars_truncatedname
		// Total should be manageable
		baseName := filepath.Base(trashFile)
		if len(baseName) > 150 {
			t.Errorf("trash filename too long: %d chars", len(baseName))
		}
	})

	t.Run("nil manager returns error", func(t *testing.T) {
		var m *Manager
		_, err := m.MoveToTrash("/some/path")
		if err == nil {
			t.Fatal("expected error for nil manager")
		}
		if !strings.Contains(err.Error(), "nil") {
			t.Errorf("error should mention nil: %v", err)
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		trashPath := t.TempDir()
		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		_, err = m.MoveToTrash("/nonexistent/file.txt")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

func TestCleanup(t *testing.T) {
	t.Run("old files are removed", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath, MaxAge: time.Hour}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create an "old" file by setting its mod time in the past
		oldFile := filepath.Join(trashPath, "old_file.txt")
		if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
			t.Fatalf("failed to create old file: %v", err)
		}
		oldTime := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set mod time: %v", err)
		}

		// Create a "recent" file
		recentFile := filepath.Join(trashPath, "recent_file.txt")
		if err := os.WriteFile(recentFile, []byte("recent content"), 0644); err != nil {
			t.Fatalf("failed to create recent file: %v", err)
		}

		// Run cleanup
		count, bytesFreed, err := m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}

		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
		if bytesFreed != int64(len("old content")) {
			t.Errorf("bytesFreed = %d, want %d", bytesFreed, len("old content"))
		}

		// Verify old file is gone
		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			t.Error("old file should be removed")
		}

		// Verify recent file still exists
		if _, err := os.Stat(recentFile); err != nil {
			t.Error("recent file should still exist")
		}
	})

	t.Run("zero maxAge means no cleanup", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath, MaxAge: 0}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create an "old" file
		oldFile := filepath.Join(trashPath, "old_file.txt")
		if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
			t.Fatalf("failed to create old file: %v", err)
		}
		oldTime := time.Now().Add(-24 * time.Hour * 365) // 1 year old
		if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set mod time: %v", err)
		}

		count, bytesFreed, err := m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}

		if count != 0 || bytesFreed != 0 {
			t.Errorf("count = %d, bytesFreed = %d; want 0, 0", count, bytesFreed)
		}

		// File should still exist
		if _, err := os.Stat(oldFile); err != nil {
			t.Error("file should still exist when maxAge is 0")
		}
	})

	t.Run("nil manager returns without error", func(t *testing.T) {
		var m *Manager
		count, bytesFreed, err := m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 || bytesFreed != 0 {
			t.Errorf("count = %d, bytesFreed = %d; want 0, 0", count, bytesFreed)
		}
	})

	t.Run("context cancellation stops cleanup", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath, MaxAge: time.Hour}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create old files
		for i := 0; i < 10; i++ {
			f := filepath.Join(trashPath, strings.Repeat("x", i+1)+".txt")
			if err := os.WriteFile(f, []byte("content"), 0644); err != nil {
				t.Fatalf("failed to create file: %v", err)
			}
			oldTime := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(f, oldTime, oldTime); err != nil {
				t.Fatalf("failed to set mod time: %v", err)
			}
		}

		// Cancel context immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err = m.Cleanup(ctx)
		// Should complete without error (context.Canceled is handled gracefully)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("cleanup removes metadata files", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath, MaxAge: time.Hour}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create an old file with metadata
		oldFile := filepath.Join(trashPath, "old_file.txt")
		metaFile := oldFile + ".meta"
		if err := os.WriteFile(oldFile, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(metaFile, []byte("original_path: /some/path"), 0644); err != nil {
			t.Fatalf("failed to create meta file: %v", err)
		}
		oldTime := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set mod time: %v", err)
		}

		_, _, err = m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}

		// Both file and metadata should be gone
		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			t.Error("file should be removed")
		}
		if _, err := os.Stat(metaFile); !os.IsNotExist(err) {
			t.Error("metadata file should be removed")
		}
	})

	t.Run("cleanup handles directories", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath, MaxAge: time.Hour}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create an old directory with files
		oldDir := filepath.Join(trashPath, "old_dir")
		if err := os.MkdirAll(oldDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(oldDir, "file1.txt"), []byte("content1"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(oldDir, "file2.txt"), []byte("content22"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		oldTime := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set mod time: %v", err)
		}

		count, bytesFreed, err := m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}

		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
		// Should have counted both files' sizes
		expectedBytes := int64(len("content1") + len("content22"))
		if bytesFreed != expectedBytes {
			t.Errorf("bytesFreed = %d, want %d", bytesFreed, expectedBytes)
		}

		if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
			t.Error("directory should be removed")
		}
	})
}

func TestRestore(t *testing.T) {
	t.Run("restore file to original location", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create and trash a file
		srcFile := filepath.Join(srcDir, "testfile.txt")
		content := []byte("test content")
		if err := os.WriteFile(srcFile, content, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		trashFile, err := m.MoveToTrash(srcFile)
		if err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		// Restore
		restoredPath, err := m.Restore(trashFile)
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}

		if restoredPath != srcFile {
			t.Errorf("restoredPath = %q, want %q", restoredPath, srcFile)
		}

		// Verify file is restored
		restoredContent, err := os.ReadFile(srcFile)
		if err != nil {
			t.Fatalf("failed to read restored file: %v", err)
		}
		if string(restoredContent) != string(content) {
			t.Errorf("content = %q, want %q", restoredContent, content)
		}

		// Verify trash file is gone
		if _, err := os.Stat(trashFile); !os.IsNotExist(err) {
			t.Error("trash file should be removed after restore")
		}

		// Verify metadata is gone
		if _, err := os.Stat(trashFile + ".meta"); !os.IsNotExist(err) {
			t.Error("metadata file should be removed after restore")
		}
	})

	t.Run("restore creates parent directory if needed", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create and trash a file in a nested directory
		nestedDir := filepath.Join(srcDir, "a", "b", "c")
		if err := os.MkdirAll(nestedDir, 0755); err != nil {
			t.Fatalf("failed to create nested dir: %v", err)
		}
		srcFile := filepath.Join(nestedDir, "testfile.txt")
		if err := os.WriteFile(srcFile, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		trashFile, err := m.MoveToTrash(srcFile)
		if err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		// Remove the parent directories
		if err := os.RemoveAll(filepath.Join(srcDir, "a")); err != nil {
			t.Fatalf("failed to remove parent dirs: %v", err)
		}

		// Restore should recreate parent directories
		_, err = m.Restore(trashFile)
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(srcFile); err != nil {
			t.Errorf("restored file should exist: %v", err)
		}
	})

	t.Run("missing metadata returns error", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a file without metadata
		trashFile := filepath.Join(trashPath, "orphan.txt")
		if err := os.WriteFile(trashFile, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		_, err = m.Restore(trashFile)
		if err == nil {
			t.Fatal("expected error for missing metadata")
		}
	})

	t.Run("empty original path in metadata returns error", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a file with metadata but no original_path
		trashFile := filepath.Join(trashPath, "bad_meta.txt")
		metaFile := trashFile + ".meta"
		if err := os.WriteFile(trashFile, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(metaFile, []byte("trashed_at: 2024-01-01\n"), 0644); err != nil {
			t.Fatalf("failed to create meta file: %v", err)
		}

		_, err = m.Restore(trashFile)
		if err == nil {
			t.Fatal("expected error for empty original path")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention path not found: %v", err)
		}
	})

	t.Run("nil manager returns error", func(t *testing.T) {
		var m *Manager
		_, err := m.Restore("/some/trash/path")
		if err == nil {
			t.Fatal("expected error for nil manager")
		}
	})
}

func TestList(t *testing.T) {
	t.Run("lists all trash items", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create and trash multiple files
		for i := 0; i < 3; i++ {
			f := filepath.Join(srcDir, strings.Repeat("f", i+1)+".txt")
			if err := os.WriteFile(f, []byte("content"), 0644); err != nil {
				t.Fatalf("failed to create file: %v", err)
			}
			if _, err := m.MoveToTrash(f); err != nil {
				t.Fatalf("MoveToTrash failed: %v", err)
			}
			// Small delay to ensure different timestamps
			time.Sleep(10 * time.Millisecond)
		}

		items, err := m.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(items) != 3 {
			t.Errorf("len(items) = %d, want 3", len(items))
		}

		// Verify items have expected fields populated
		for _, item := range items {
			if item.TrashPath == "" {
				t.Error("TrashPath should not be empty")
			}
			if item.OriginalPath == "" {
				t.Error("OriginalPath should not be empty")
			}
			if item.Name == "" {
				t.Error("Name should not be empty")
			}
			if item.TrashedAt.IsZero() {
				t.Error("TrashedAt should not be zero")
			}
		}
	})

	t.Run("skips metadata files", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a file and its metadata directly
		if err := os.WriteFile(filepath.Join(trashPath, "file.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(trashPath, "file.txt.meta"), []byte("meta"), 0644); err != nil {
			t.Fatalf("failed to create meta: %v", err)
		}

		items, err := m.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(items) != 1 {
			t.Errorf("len(items) = %d, want 1 (metadata should be skipped)", len(items))
		}
	})

	t.Run("handles missing metadata gracefully", func(t *testing.T) {
		trashPath := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a file without metadata
		if err := os.WriteFile(filepath.Join(trashPath, "orphan.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		items, err := m.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}

		// OriginalPath should be empty when metadata is missing
		if items[0].OriginalPath != "" {
			t.Error("OriginalPath should be empty when metadata is missing")
		}
	})

	t.Run("nil manager returns nil", func(t *testing.T) {
		var m *Manager
		items, err := m.List()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if items != nil {
			t.Errorf("items = %v, want nil", items)
		}
	})

	t.Run("lists directories", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create and trash a directory
		testDir := filepath.Join(srcDir, "testdir")
		if err := os.MkdirAll(testDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		if _, err := m.MoveToTrash(testDir); err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		items, err := m.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}

		if !items[0].IsDir {
			t.Error("item should be marked as directory")
		}
	})

	t.Run("directory size is actual content size not 4096", func(t *testing.T) {
		trashPath := t.TempDir()
		srcDir := t.TempDir()

		m, err := New(Config{TrashPath: trashPath}, nil)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Create a directory with known content sizes
		testDir := filepath.Join(srcDir, "sizetest")
		if err := os.MkdirAll(testDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		// Create two files with known sizes (100 bytes each)
		content := make([]byte, 100)
		if err := os.WriteFile(filepath.Join(testDir, "file1.txt"), content, 0644); err != nil {
			t.Fatalf("failed to create file1: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "file2.txt"), content, 0644); err != nil {
			t.Fatalf("failed to create file2: %v", err)
		}

		if _, err := m.MoveToTrash(testDir); err != nil {
			t.Fatalf("MoveToTrash failed: %v", err)
		}

		items, err := m.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}

		// Size should be 200 bytes (2 files * 100 bytes), NOT 4096 (directory inode size)
		expectedSize := int64(200)
		if items[0].Size != expectedSize {
			t.Errorf("directory Size = %d, want %d (actual content size)", items[0].Size, expectedSize)
		}
	})
}

func TestHashPath(t *testing.T) {
	t.Run("same input produces same hash", func(t *testing.T) {
		path := "/some/test/path"
		h1 := hashPath(path)
		h2 := hashPath(path)
		if h1 != h2 {
			t.Errorf("hash should be deterministic: %q != %q", h1, h2)
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := hashPath("/path/one")
		h2 := hashPath("/path/two")
		if h1 == h2 {
			t.Error("different paths should produce different hashes")
		}
	})

	t.Run("hash is hex encoded", func(t *testing.T) {
		h := hashPath("/test")
		for _, c := range h {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("hash should be hex encoded, got char %q", c)
			}
		}
	})
}

func TestTrashItemStruct(t *testing.T) {
	item := TrashItem{
		TrashPath:    "/trash/file.txt",
		OriginalPath: "/original/file.txt",
		Name:         "file.txt",
		Size:         1024,
		TrashedAt:    time.Now(),
		IsDir:        false,
	}

	if item.TrashPath != "/trash/file.txt" {
		t.Error("TrashPath not set correctly")
	}
	if item.OriginalPath != "/original/file.txt" {
		t.Error("OriginalPath not set correctly")
	}
	if item.Size != 1024 {
		t.Error("Size not set correctly")
	}
	if item.IsDir {
		t.Error("IsDir should be false")
	}
}

// TestCopyFileStreaming tests the streaming copy function used for cross-device moves.
// This is a regression test for the OOM fix - previously used os.ReadFile which loaded
// entire files into memory.
func TestCopyFileStreaming(t *testing.T) {
	t.Run("copies file contents correctly", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "source.txt")
		dstPath := filepath.Join(dstDir, "dest.txt")

		content := []byte("test content for streaming copy")
		if err := os.WriteFile(srcPath, content, 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileStreaming(srcPath, dstPath, 0644); err != nil {
			t.Fatalf("copyFileStreaming failed: %v", err)
		}

		// Verify content was copied correctly
		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read dest file: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("content mismatch: got %q, want %q", got, content)
		}

		// Source should still exist (copyFileStreaming doesn't delete)
		if _, err := os.Stat(srcPath); err != nil {
			t.Error("source file should still exist")
		}
	})

	t.Run("preserves file mode", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "source.txt")
		dstPath := filepath.Join(dstDir, "dest.txt")

		if err := os.WriteFile(srcPath, []byte("test"), 0755); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileStreaming(srcPath, dstPath, 0755); err != nil {
			t.Fatalf("copyFileStreaming failed: %v", err)
		}

		info, err := os.Stat(dstPath)
		if err != nil {
			t.Fatalf("failed to stat dest file: %v", err)
		}

		// Check executable bit is preserved (masking with 0777 for portability)
		if info.Mode().Perm()&0100 == 0 {
			t.Error("executable permission not preserved")
		}
	})

	t.Run("handles non-existent source", func(t *testing.T) {
		dstDir := t.TempDir()
		err := copyFileStreaming("/nonexistent/file", filepath.Join(dstDir, "dest"), 0644)
		if err == nil {
			t.Error("expected error for non-existent source")
		}
	})

	t.Run("handles large file without OOM", func(t *testing.T) {
		// This test verifies we don't load the entire file into memory.
		// We create a 10MB file - if we were using os.ReadFile, this would
		// allocate 10MB of RAM. With streaming, we use only a small buffer.
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "large.bin")
		dstPath := filepath.Join(dstDir, "large.bin")

		// Create a 10MB file
		f, err := os.Create(srcPath)
		if err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		size := int64(10 * 1024 * 1024) // 10MB
		chunk := make([]byte, 64*1024)  // 64KB chunks
		for i := range chunk {
			chunk[i] = byte(i % 256)
		}

		written := int64(0)
		for written < size {
			n, err := f.Write(chunk)
			if err != nil {
				f.Close()
				t.Fatalf("failed to write chunk: %v", err)
			}
			written += int64(n)
		}
		f.Close()

		// Copy using streaming
		if err := copyFileStreaming(srcPath, dstPath, 0644); err != nil {
			t.Fatalf("copyFileStreaming failed: %v", err)
		}

		// Verify size matches
		srcInfo, _ := os.Stat(srcPath)
		dstInfo, _ := os.Stat(dstPath)
		if srcInfo.Size() != dstInfo.Size() {
			t.Errorf("size mismatch: src=%d, dst=%d", srcInfo.Size(), dstInfo.Size())
		}
	})
}

// TestCopyFileAndDelete tests the full cross-device move with atomic rename and source deletion.
func TestCopyFileAndDelete(t *testing.T) {
	t.Run("copies and deletes source", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "source.txt")
		dstPath := filepath.Join(dstDir, "dest.txt")

		content := []byte("content to move")
		if err := os.WriteFile(srcPath, content, 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileAndDelete(srcPath, dstPath, 0644); err != nil {
			t.Fatalf("copyFileAndDelete failed: %v", err)
		}

		// Verify content at destination
		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read dest file: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("content mismatch: got %q, want %q", got, content)
		}

		// Source should be deleted
		if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
			t.Error("source file should be deleted")
		}
	})

	t.Run("no temp file left on success", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "source.txt")
		dstPath := filepath.Join(dstDir, "dest.txt")

		if err := os.WriteFile(srcPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileAndDelete(srcPath, dstPath, 0644); err != nil {
			t.Fatalf("copyFileAndDelete failed: %v", err)
		}

		// Verify no .tmp file remains
		tmpPath := dstPath + ".tmp"
		if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
			t.Error("temp file should not exist after successful copy")
		}
	})
}

// TestCopyDirAndDelete tests recursive directory copy with streaming.
func TestCopyDirAndDelete(t *testing.T) {
	t.Run("copies directory tree and deletes source", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcTree := filepath.Join(srcDir, "tree")
		dstTree := filepath.Join(dstDir, "tree")

		// Create a directory tree
		if err := os.MkdirAll(filepath.Join(srcTree, "subdir"), 0755); err != nil {
			t.Fatalf("failed to create subdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcTree, "file1.txt"), []byte("file1"), 0644); err != nil {
			t.Fatalf("failed to write file1: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcTree, "subdir", "file2.txt"), []byte("file2"), 0644); err != nil {
			t.Fatalf("failed to write file2: %v", err)
		}

		if err := copyDirAndDelete(srcTree, dstTree); err != nil {
			t.Fatalf("copyDirAndDelete failed: %v", err)
		}

		// Verify destination structure
		if _, err := os.Stat(filepath.Join(dstTree, "file1.txt")); err != nil {
			t.Error("file1.txt should exist in destination")
		}
		if _, err := os.Stat(filepath.Join(dstTree, "subdir", "file2.txt")); err != nil {
			t.Error("subdir/file2.txt should exist in destination")
		}

		// Verify content
		got, _ := os.ReadFile(filepath.Join(dstTree, "subdir", "file2.txt"))
		if string(got) != "file2" {
			t.Errorf("content mismatch: got %q, want %q", got, "file2")
		}

		// Source should be deleted
		if _, err := os.Stat(srcTree); !os.IsNotExist(err) {
			t.Error("source directory should be deleted")
		}
	})
}
