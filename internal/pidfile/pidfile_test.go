//go:build unix

package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("empty path returns nil", func(t *testing.T) {
		pf, err := New("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pf != nil {
			t.Fatal("expected nil PIDFile for empty path")
		}
	})

	t.Run("valid path creates file with current PID", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		defer pf.Close()

		// Verify file exists
		if _, err := os.Stat(pidPath); err != nil {
			t.Fatalf("PID file not created: %v", err)
		}

		// Verify content is our PID
		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("PID = %d, want %d", pid, os.Getpid())
		}
	})

	t.Run("creates parent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "nested", "dir", "test.pid")

		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		defer pf.Close()

		// Verify directory structure was created
		if _, err := os.Stat(filepath.Dir(pidPath)); err != nil {
			t.Fatalf("parent directory not created: %v", err)
		}
	})

	t.Run("second lock acquisition fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// First lock
		pf1, err := New(pidPath)
		if err != nil {
			t.Fatalf("first New failed: %v", err)
		}
		defer pf1.Close()

		// Second lock should fail
		pf2, err := New(pidPath)
		if err == nil {
			pf2.Close()
			t.Fatal("expected error for second lock acquisition")
		}

		// Error should mention "another instance"
		if pf2 != nil {
			t.Error("expected nil PIDFile on failure")
		}
	})

	t.Run("lock released after close allows new acquisition", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// First lock
		pf1, err := New(pidPath)
		if err != nil {
			t.Fatalf("first New failed: %v", err)
		}

		// Close first lock
		if err := pf1.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Second lock should succeed
		pf2, err := New(pidPath)
		if err != nil {
			t.Fatalf("second New failed after Close: %v", err)
		}
		defer pf2.Close()
	})
}

func TestClose(t *testing.T) {
	t.Run("removes pid file", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		// Close should remove the file
		if err := pf.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// File should not exist
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Error("PID file should be removed after Close")
		}
	})

	t.Run("nil receiver is safe", func(t *testing.T) {
		var pf *PIDFile
		err := pf.Close()
		if err != nil {
			t.Fatalf("Close on nil should not error: %v", err)
		}
	})

	t.Run("double close is safe", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		// First close
		if err := pf.Close(); err != nil {
			t.Fatalf("first Close failed: %v", err)
		}

		// Second close - file already gone, should handle gracefully
		// Note: This may error on close since file handle is already closed,
		// but should not panic
		_ = pf.Close()
	})
}

func TestPath(t *testing.T) {
	t.Run("returns correct path", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		defer pf.Close()

		if pf.Path() != pidPath {
			t.Errorf("Path() = %q, want %q", pf.Path(), pidPath)
		}
	})

	t.Run("nil receiver returns empty string", func(t *testing.T) {
		var pf *PIDFile
		if pf.Path() != "" {
			t.Errorf("Path() on nil = %q, want empty string", pf.Path())
		}
	})
}

func TestReadPID(t *testing.T) {
	t.Run("reads valid PID", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write a PID manually
		if err := os.WriteFile(pidPath, []byte("12345\n"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != 12345 {
			t.Errorf("pid = %d, want 12345", pid)
		}
	})

	t.Run("handles trailing newlines", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write with multiple trailing newlines
		if err := os.WriteFile(pidPath, []byte("54321\n\n"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != 54321 {
			t.Errorf("pid = %d, want 54321", pid)
		}
	})

	t.Run("handles CRLF line endings", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write with Windows line endings
		if err := os.WriteFile(pidPath, []byte("99999\r\n"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != 99999 {
			t.Errorf("pid = %d, want 99999", pid)
		}
	})

	t.Run("handles no newline", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write without newline
		if err := os.WriteFile(pidPath, []byte("11111"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != 11111 {
			t.Errorf("pid = %d, want 11111", pid)
		}
	})

	t.Run("invalid PID returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write invalid content
		if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		_, err := ReadPID(pidPath)
		if err == nil {
			t.Fatal("expected error for invalid PID")
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := ReadPID("/nonexistent/path/test.pid")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("empty file returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "test.pid")

		// Write empty file
		if err := os.WriteFile(pidPath, []byte(""), 0644); err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		_, err := ReadPID(pidPath)
		if err == nil {
			t.Fatal("expected error for empty file")
		}
	})
}

func TestIsRunning(t *testing.T) {
	t.Run("current process is running", func(t *testing.T) {
		if !IsRunning(os.Getpid()) {
			t.Error("current process should be running")
		}
	})

	t.Run("parent process is running", func(t *testing.T) {
		// Parent process should always be running
		ppid := os.Getppid()
		if ppid > 0 && !IsRunning(ppid) {
			t.Errorf("parent process (PID %d) should be running", ppid)
		}
	})

	t.Run("non-existent PID is not running", func(t *testing.T) {
		// Use a very high PID that's unlikely to exist
		// Most systems have pid_max around 32768 or 4194304
		if IsRunning(999999999) {
			t.Error("non-existent PID should not be running")
		}
	})

	t.Run("negative PID is not running", func(t *testing.T) {
		if IsRunning(-1) {
			t.Error("negative PID should not be running")
		}
	})

	t.Run("zero PID is not running", func(t *testing.T) {
		// PID 0 is the kernel scheduler, signal 0 to it may behave differently
		// This test just ensures no panic
		_ = IsRunning(0)
	})
}

func TestIntegration(t *testing.T) {
	t.Run("full lifecycle", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "daemon.pid")

		// Create PID file
		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		// Verify path
		if pf.Path() != pidPath {
			t.Errorf("Path() = %q, want %q", pf.Path(), pidPath)
		}

		// Read and verify PID
		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("stored PID = %d, want %d", pid, os.Getpid())
		}

		// Verify process is running
		if !IsRunning(pid) {
			t.Error("current process should be detected as running")
		}

		// Clean up
		if err := pf.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Verify file is gone
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Error("PID file should be removed after Close")
		}
	})

	t.Run("stale PID file detection", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidPath := filepath.Join(tmpDir, "stale.pid")

		// Create a stale PID file with a non-existent PID
		stalePID := 999999999
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)+"\n"), 0644); err != nil {
			t.Fatalf("failed to write stale PID file: %v", err)
		}

		// Verify the PID is not running
		if IsRunning(stalePID) {
			t.Skip("stale PID unexpectedly exists")
		}

		// New should succeed because flock is what matters, not file content
		// The stale PID file will be overwritten with our PID
		pf, err := New(pidPath)
		if err != nil {
			t.Fatalf("New failed on stale PID file: %v", err)
		}
		defer pf.Close()

		// Verify our PID is now in the file
		pid, err := ReadPID(pidPath)
		if err != nil {
			t.Fatalf("ReadPID failed: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("PID = %d, want %d (our PID)", pid, os.Getpid())
		}
	})
}
