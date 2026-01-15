package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// mockSafety implements core.Safety for testing
type mockSafety struct {
	allowed bool
	reason  string
}

func (m *mockSafety) Validate(_ context.Context, _ core.Candidate, _ core.SafetyConfig) core.SafetyVerdict {
	return core.SafetyVerdict{Allowed: m.allowed, Reason: m.reason}
}

func TestExecuteDryRunReportsWouldDelete(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeDryRun)

	if result.Deleted {
		t.Error("expected Deleted=false for dry-run")
	}
	if result.Reason != "would_delete" {
		t.Errorf("expected reason 'would_delete', got '%s'", result.Reason)
	}
	if result.BytesFreed != 5 {
		t.Errorf("expected BytesFreed=5, got %d", result.BytesFreed)
	}

	// File should still exist
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("file should still exist after dry-run: %v", err)
	}
}

func TestExecuteDeletesFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if !result.Deleted {
		t.Errorf("expected Deleted=true, got false (reason: %s)", result.Reason)
	}
	if result.Reason != "deleted" {
		t.Errorf("expected reason 'deleted', got '%s'", result.Reason)
	}
	if result.BytesFreed != 5 {
		t.Errorf("expected BytesFreed=5, got %d", result.BytesFreed)
	}

	// File should be gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("file should not exist after execute: %v", err)
	}
}

func TestExecuteRejectsPolicyDeny(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: false, Reason: "too_new"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when policy denies")
	}
	if result.Reason != "policy_deny:too_new" {
		t.Errorf("expected reason 'policy_deny:too_new', got '%s'", result.Reason)
	}

	// File should still exist
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("file should still exist when policy denies: %v", err)
	}
}

func TestExecuteRejectsSafetyDeny(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: false, Reason: "protected_path"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when safety denies at scan-time")
	}
	if result.Reason != "safety_deny_scan:protected_path" {
		t.Errorf("expected reason 'safety_deny_scan:protected_path', got '%s'", result.Reason)
	}

	// File should still exist
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("file should still exist when safety denies: %v", err)
	}
}

func TestExecuteTOCTOURecheck(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Safety will deny at execute-time (TOCTOU check)
	safe := &mockSafety{allowed: false, reason: "symlink_escape"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"}, // Allowed at scan-time
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when TOCTOU check fails")
	}
	if result.Reason != "safety_deny_execute:symlink_escape" {
		t.Errorf("expected reason 'safety_deny_execute:symlink_escape', got '%s'", result.Reason)
	}

	// File should still exist
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("file should still exist when TOCTOU check fails: %v", err)
	}
}

func TestExecuteIdempotentAlreadyGone(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "nonexistent.txt") // File doesn't exist

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when file already gone")
	}
	if result.Reason != "already_gone" {
		t.Errorf("expected reason 'already_gone', got '%s'", result.Reason)
	}
}

func TestExecuteDeletesDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Add files to make it non-empty
	if err := os.WriteFile(filepath.Join(subdir, "file1.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file2.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}, AllowDirDelete: true}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: subdir,
			Type: core.TargetDir,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if !result.Deleted {
		t.Errorf("expected Deleted=true, got false (reason: %s)", result.Reason)
	}
	if result.Reason != "deleted" {
		t.Errorf("expected reason 'deleted', got '%s'", result.Reason)
	}
	// Should report bytes freed (5 + 5 = 10 bytes from the two files)
	if result.BytesFreed != 10 {
		t.Errorf("expected BytesFreed=10, got %d", result.BytesFreed)
	}

	// Directory should be gone
	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Errorf("directory should not exist after execute: %v", err)
	}
}

func TestExecuteDirDeleteDisabled(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}, AllowDirDelete: false}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: subdir,
			Type: core.TargetDir,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when dir delete is disabled")
	}
	if result.Reason != "dir_delete_disabled" {
		t.Errorf("expected reason 'dir_delete_disabled', got '%s'", result.Reason)
	}

	// Directory should still exist
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("directory should still exist when dir delete disabled: %v", err)
	}
}

func TestExecuteContextCancellation(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure context is expired

	result := exec.Execute(ctx, item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false when context canceled")
	}
	if result.Reason != "ctx_canceled" {
		t.Errorf("expected reason 'ctx_canceled', got '%s'", result.Reason)
	}
}
