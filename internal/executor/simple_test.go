package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/safety"
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

// mockLogger implements logger.Logger for testing
type mockLogger struct {
	debugCalls []map[string]any
	infoCalls  []map[string]any
	warnCalls  []map[string]any
	errorCalls []map[string]any
}

func (m *mockLogger) Debug(msg string, fields ...logger.Field) {
	m.debugCalls = append(m.debugCalls, map[string]any{"msg": msg, "fields": fields})
}

func (m *mockLogger) Info(msg string, fields ...logger.Field) {
	m.infoCalls = append(m.infoCalls, map[string]any{"msg": msg, "fields": fields})
}

func (m *mockLogger) Warn(msg string, fields ...logger.Field) {
	m.warnCalls = append(m.warnCalls, map[string]any{"msg": msg, "fields": fields})
}

func (m *mockLogger) Error(msg string, fields ...logger.Field) {
	m.errorCalls = append(m.errorCalls, map[string]any{"msg": msg, "fields": fields})
}

func (m *mockLogger) WithFields(fields ...logger.Field) logger.Logger {
	return m
}

// mockMetrics implements core.Metrics for testing with thread-safety for concurrent tests
type mockMetrics struct {
	mu             sync.Mutex
	filesDeleted   map[string]int
	dirsDeleted    map[string]int
	bytesFreed     int64
	deleteErrors   map[string]int
	filesScanned   map[string]int
	dirsScanned    map[string]int
	policyDecision map[string]int
	safetyVerdict  map[string]int
}

func newMockMetrics() *mockMetrics {
	return &mockMetrics{
		filesDeleted:   make(map[string]int),
		dirsDeleted:    make(map[string]int),
		deleteErrors:   make(map[string]int),
		filesScanned:   make(map[string]int),
		dirsScanned:    make(map[string]int),
		policyDecision: make(map[string]int),
		safetyVerdict:  make(map[string]int),
	}
}

func (m *mockMetrics) IncFilesScanned(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filesScanned[root]++
}
func (m *mockMetrics) IncDirsScanned(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirsScanned[root]++
}
func (m *mockMetrics) ObserveScanDuration(root string, d time.Duration) {}
func (m *mockMetrics) IncPolicyDecision(reason string, allowed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policyDecision[reason]++
}
func (m *mockMetrics) IncSafetyVerdict(reason string, allowed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.safetyVerdict[reason]++
}
func (m *mockMetrics) SetBytesEligible(bytes int64) {}
func (m *mockMetrics) SetFilesEligible(count int)   {}
func (m *mockMetrics) IncFilesDeleted(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filesDeleted[root]++
}
func (m *mockMetrics) IncDirsDeleted(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirsDeleted[root]++
}
func (m *mockMetrics) AddBytesFreed(bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bytesFreed += bytes
}
func (m *mockMetrics) IncDeleteErrors(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteErrors[reason]++
}
func (m *mockMetrics) SetDiskUsage(percent float64)    {}
func (m *mockMetrics) SetCPUUsage(percent float64)     {}
func (m *mockMetrics) SetLastRunTimestamp(t time.Time) {}

// mockAuditor implements core.Auditor for testing with thread-safety
type mockAuditor struct {
	mu     sync.Mutex
	events []core.AuditEvent
}

func (m *mockAuditor) Record(_ context.Context, evt core.AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
}

func (m *mockAuditor) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func TestNewSimpleWithLogger(t *testing.T) {
	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{}

	// Test with nil logger (should use nop)
	exec := NewSimpleWithLogger(safe, cfg, nil)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	if exec.log == nil {
		t.Error("expected non-nil logger")
	}

	// Test with real logger - verify it's used by executing and checking calls
	log := &mockLogger{}
	exec = NewSimpleWithLogger(safe, cfg, log)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	// Verify the logger works by using it
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := core.PlanItem{
		Candidate: core.Candidate{Path: testFile, Type: core.TargetFile},
		Decision:  core.Decision{Allow: true, Reason: "ok"},
		Safety:    core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}
	exec.Execute(context.Background(), item, core.ModeExecute)
	if len(log.debugCalls) == 0 {
		t.Error("expected logger to be called")
	}
}

func TestNewSimpleWithMetrics(t *testing.T) {
	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{}

	// Test with nil logger and nil metrics (should use nop for both)
	exec := NewSimpleWithMetrics(safe, cfg, nil, nil)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	if exec.log == nil {
		t.Error("expected non-nil logger")
	}
	if exec.metrics == nil {
		t.Error("expected non-nil metrics")
	}

	// Test with real logger and metrics - verify they're used by executing
	log := &mockLogger{}
	m := newMockMetrics()
	exec = NewSimpleWithMetrics(safe, cfg, log, m)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	// Verify by executing and checking calls
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := core.PlanItem{
		Candidate: core.Candidate{Path: testFile, Type: core.TargetFile, Root: dir, SizeBytes: 1},
		Decision:  core.Decision{Allow: true, Reason: "ok"},
		Safety:    core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}
	exec.Execute(context.Background(), item, core.ModeExecute)
	if len(log.debugCalls) == 0 {
		t.Error("expected logger to be called")
	}
	if m.filesDeleted[dir] != 1 {
		t.Error("expected metrics to be called")
	}
}

func TestWithAuditor(t *testing.T) {
	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{}
	exec := NewSimple(safe, cfg)

	aud := &mockAuditor{}
	result := exec.WithAuditor(aud)

	if result != exec {
		t.Error("expected WithAuditor to return same executor for chaining")
	}
	if exec.aud != aud {
		t.Error("expected auditor to be set")
	}
}

func TestExecuteRecordsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	aud := &mockAuditor{}
	exec := NewSimple(safe, cfg).WithAuditor(aud)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			Root:      dir,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok", Score: 100},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeExecute)

	if len(aud.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(aud.events))
	}

	evt := aud.events[0]
	if evt.Action != "delete" {
		t.Errorf("expected action 'delete', got '%s'", evt.Action)
	}
	if evt.Path != testFile {
		t.Errorf("expected path '%s', got '%s'", testFile, evt.Path)
	}
	if evt.Level != "info" {
		t.Errorf("expected level 'info', got '%s'", evt.Level)
	}
	if evt.Fields["deleted"] != true {
		t.Errorf("expected deleted=true in fields")
	}
	if evt.Fields["bytes_freed"] != int64(5) {
		t.Errorf("expected bytes_freed=5 in fields, got %v", evt.Fields["bytes_freed"])
	}
}

func TestExecuteAuditEventWouldDelete(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	aud := &mockAuditor{}
	exec := NewSimple(safe, cfg).WithAuditor(aud)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeDryRun)

	if len(aud.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(aud.events))
	}

	evt := aud.events[0]
	if evt.Action != "would_delete" {
		t.Errorf("expected action 'would_delete', got '%s'", evt.Action)
	}
}

func TestExecuteAuditEventSkip(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	aud := &mockAuditor{}
	exec := NewSimple(safe, cfg).WithAuditor(aud)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: testFile,
			Type: core.TargetFile,
		},
		Decision: core.Decision{Allow: false, Reason: "too_new"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeExecute)

	if len(aud.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(aud.events))
	}

	evt := aud.events[0]
	if evt.Action != "skip" {
		t.Errorf("expected action 'skip', got '%s'", evt.Action)
	}
}

func TestExecuteInvalidMode(t *testing.T) {
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

	result := exec.Execute(context.Background(), item, core.Mode("invalid"))

	if result.Deleted {
		t.Error("expected Deleted=false for invalid mode")
	}
	if result.Reason != "invalid_mode" {
		t.Errorf("expected reason 'invalid_mode', got '%s'", result.Reason)
	}
	if result.Err == nil {
		t.Error("expected error for invalid mode")
	}

	// File should still exist
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("file should still exist for invalid mode: %v", err)
	}
}

func TestExecuteUnknownTargetType(t *testing.T) {
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
			Type: core.TargetType("unknown"),
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if result.Deleted {
		t.Error("expected Deleted=false for unknown target type")
	}
	if result.Reason != "unknown_target_type" {
		t.Errorf("expected reason 'unknown_target_type', got '%s'", result.Reason)
	}
	if result.Err == nil {
		t.Error("expected error for unknown target type")
	}
}

func TestExecuteFileDeleteFailure(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make file read-only to cause permission error
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot change directory permissions")
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	m := newMockMetrics()
	exec := NewSimpleWithMetrics(safe, cfg, nil, m)

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
		t.Error("expected Deleted=false when delete fails")
	}
	if result.Reason != "delete_failed" {
		t.Errorf("expected reason 'delete_failed', got '%s'", result.Reason)
	}
	if result.Err == nil {
		t.Error("expected error when delete fails")
	}
	if m.deleteErrors["delete_failed"] != 1 {
		t.Errorf("expected delete_failed metric to be 1, got %d", m.deleteErrors["delete_failed"])
	}
}

func TestExecuteDirectoryAlreadyGone(t *testing.T) {
	// Note: os.RemoveAll is idempotent by design - it returns nil when path doesn't exist.
	// This means for directories, we get "deleted" result with BytesFreed=0, not "already_gone".
	// This is different from file deletion where os.Remove returns ErrNotExist.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nonexistent") // Directory doesn't exist

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

	// os.RemoveAll succeeds for non-existent paths (idempotent)
	if !result.Deleted {
		t.Error("expected Deleted=true (RemoveAll is idempotent)")
	}
	if result.Reason != "deleted" {
		t.Errorf("expected reason 'deleted', got '%s'", result.Reason)
	}
	if result.BytesFreed != 0 {
		t.Errorf("expected BytesFreed=0 for non-existent dir, got %d", result.BytesFreed)
	}
}

func TestExecuteDirectoryDeleteFailure(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Add a file inside
	if err := os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make parent read-only to prevent deletion
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot change directory permissions")
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}, AllowDirDelete: true}
	m := newMockMetrics()
	exec := NewSimpleWithMetrics(safe, cfg, nil, m)

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
		t.Error("expected Deleted=false when directory delete fails")
	}
	if result.Reason != "delete_failed" {
		t.Errorf("expected reason 'delete_failed', got '%s'", result.Reason)
	}
	if result.Err == nil {
		t.Error("expected error when directory delete fails")
	}
	if m.deleteErrors["delete_failed"] != 1 {
		t.Errorf("expected delete_failed metric to be 1, got %d", m.deleteErrors["delete_failed"])
	}
}

func TestExecuteDryRunDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
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

	result := exec.Execute(context.Background(), item, core.ModeDryRun)

	if result.Deleted {
		t.Error("expected Deleted=false for dry-run")
	}
	if result.Reason != "would_delete" {
		t.Errorf("expected reason 'would_delete', got '%s'", result.Reason)
	}
	// BytesFreed should be 0 for dir dry-run (no WalkDir in dry-run)
	if result.BytesFreed != 0 {
		t.Errorf("expected BytesFreed=0 for dir dry-run, got %d", result.BytesFreed)
	}

	// Directory should still exist
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("directory should still exist after dry-run: %v", err)
	}
}

func TestExecuteMetricsIntegration(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	m := newMockMetrics()
	exec := NewSimpleWithMetrics(safe, cfg, nil, m)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			Root:      dir,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeExecute)

	if m.filesDeleted[dir] != 1 {
		t.Errorf("expected filesDeleted[%s]=1, got %d", dir, m.filesDeleted[dir])
	}
	if m.bytesFreed != 5 {
		t.Errorf("expected bytesFreed=5, got %d", m.bytesFreed)
	}
}

func TestExecuteMetricsDirIntegration(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}, AllowDirDelete: true}
	m := newMockMetrics()
	exec := NewSimpleWithMetrics(safe, cfg, nil, m)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path: subdir,
			Type: core.TargetDir,
			Root: dir,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeExecute)

	if m.dirsDeleted[dir] != 1 {
		t.Errorf("expected dirsDeleted[%s]=1, got %d", dir, m.dirsDeleted[dir])
	}
	if m.bytesFreed != 5 {
		t.Errorf("expected bytesFreed=5, got %d", m.bytesFreed)
	}
}

func TestExecuteLoggerIntegration(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	log := &mockLogger{}
	exec := NewSimpleWithLogger(safe, cfg, log)

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	exec.Execute(context.Background(), item, core.ModeExecute)

	if len(log.debugCalls) == 0 {
		t.Error("expected at least one debug log call")
	}
	if len(log.infoCalls) == 0 {
		t.Error("expected at least one info log call for successful deletion")
	}
}

func TestExecuteNoAuditorDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg) // No auditor set

	item := core.PlanItem{
		Candidate: core.Candidate{
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	// Should not panic
	result := exec.Execute(context.Background(), item, core.ModeExecute)

	if !result.Deleted {
		t.Error("expected Deleted=true")
	}
}

func TestExecuteTimestamps(t *testing.T) {
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

	before := time.Now()
	result := exec.Execute(context.Background(), item, core.ModeExecute)
	after := time.Now()

	if result.StartedAt.Before(before) || result.StartedAt.After(after) {
		t.Error("StartedAt timestamp is out of expected range")
	}
	// FinishedAt can equal StartedAt if operation is fast, so use !After instead of Before
	if result.FinishedAt.Before(result.StartedAt) {
		t.Errorf("FinishedAt (%v) should not be before StartedAt (%v)", result.FinishedAt, result.StartedAt)
	}
	if result.FinishedAt.After(after.Add(time.Second)) {
		t.Error("FinishedAt timestamp is too far in the future")
	}
	// Verify timestamps are not zero
	if result.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
	if result.FinishedAt.IsZero() {
		t.Error("FinishedAt should not be zero")
	}
}

// ============================================================================
// TOCTOU (Time-Of-Check-Time-Of-Use) Tests
// ============================================================================

// TestTOCTOU_SymlinkChangedBetweenScanAndExecute tests the scenario where
// a symlink target is changed after scan-time safety check passes, but
// before execute-time re-check. The executor should detect this via TOCTOU
// re-validation and deny the deletion.
func TestTOCTOU_SymlinkChangedBetweenScanAndExecute(t *testing.T) {
	// Create directory structure:
	// allowed/
	//   target.txt (legitimate file)
	//   link.txt -> target.txt (symlink to legitimate file)
	// forbidden/
	//   secret.txt (file outside allowed roots)

	allowedDir := t.TempDir()
	forbiddenDir := t.TempDir()

	targetFile := filepath.Join(allowedDir, "target.txt")
	linkFile := filepath.Join(allowedDir, "link.txt")
	secretFile := filepath.Join(forbiddenDir, "secret.txt")

	// Create files
	if err := os.WriteFile(targetFile, []byte("target content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretFile, []byte("secret content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create symlink pointing to valid target initially
	if err := os.Symlink(targetFile, linkFile); err != nil {
		t.Skip("symlinks not supported")
	}

	// Use real safety engine (not mock) for proper symlink validation
	safeEngine := safety.New()
	cfg := core.SafetyConfig{
		AllowedRoots: []string{allowedDir},
	}

	exec := NewSimple(safeEngine, cfg)

	// Build PlanItem as if scan-time passed (symlink was pointing to valid target)
	item := core.PlanItem{
		Candidate: core.Candidate{
			Root:       allowedDir,
			Path:       linkFile,
			Type:       core.TargetFile,
			SizeBytes:  14,
			IsSymlink:  true,
			LinkTarget: targetFile, // Valid at scan time
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"}, // Passed at scan time
	}

	// NOW CHANGE THE SYMLINK TARGET (simulating TOCTOU attack)
	// Remove old symlink and create new one pointing outside allowed roots
	if err := os.Remove(linkFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretFile, linkFile); err != nil {
		t.Fatal(err)
	}

	// Execute - the TOCTOU re-check should catch the escape
	result := exec.Execute(context.Background(), item, core.ModeExecute)

	// Should be denied due to symlink detection (could be symlink_self, symlink_ancestor, or symlink_escape)
	if result.Deleted {
		t.Error("expected Deleted=false - TOCTOU check should catch symlink change")
	}
	// Accept any symlink-related denial - the key is TOCTOU caught the change
	if !strings.HasPrefix(result.Reason, "safety_deny_execute:symlink_") &&
		result.Reason != "safety_deny_execute:outside_allowed_roots" {
		t.Errorf("expected safety_deny_execute with symlink or outside_allowed_roots reason, got %q", result.Reason)
	}

	// Secret file should NOT be affected
	if _, err := os.Stat(secretFile); err != nil {
		t.Error("secret file should still exist - TOCTOU protection should have prevented deletion")
	}
}

// TestTOCTOU_FileDeletedBetweenScanAndExecute tests idempotent behavior
// when a file is deleted between scan and execute.
func TestTOCTOU_FileDeletedBetweenScanAndExecute(t *testing.T) {
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
			Root:      dir,
			Path:      testFile,
			Type:      core.TargetFile,
			SizeBytes: 5,
		},
		Decision: core.Decision{Allow: true, Reason: "age_ok"},
		Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	// Delete file before execution (simulating race)
	if err := os.Remove(testFile); err != nil {
		t.Fatal(err)
	}

	result := exec.Execute(context.Background(), item, core.ModeExecute)

	// Should return "already_gone" - idempotent behavior
	if result.Deleted {
		t.Error("expected Deleted=false for already-deleted file")
	}
	if result.Reason != "already_gone" {
		t.Errorf("expected reason 'already_gone', got %q", result.Reason)
	}
}

// ============================================================================
// Concurrent Deletion Tests (Race Detector Compatible)
// ============================================================================

// TestConcurrentDeletions_NoRaces verifies that executing multiple deletions
// in parallel doesn't cause data races in metrics, auditing, or shared state.
// Run with: go test -race ./internal/executor/...
func TestConcurrentDeletions_NoRaces(t *testing.T) {
	dir := t.TempDir()

	// Create multiple test files
	const numFiles = 50
	files := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = filepath.Join(dir, "file_"+string(rune('0'+i/10))+string(rune('0'+i%10))+".txt")
		if err := os.WriteFile(files[i], []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	m := newMockMetrics()
	aud := &mockAuditor{}
	exec := NewSimpleWithMetrics(safe, cfg, nil, m).WithAuditor(aud)

	// Execute deletions concurrently
	var wg sync.WaitGroup
	results := make([]core.ActionResult, numFiles)

	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			item := core.PlanItem{
				Candidate: core.Candidate{
					Root:      dir,
					Path:      files[idx],
					Type:      core.TargetFile,
					SizeBytes: 7,
				},
				Decision: core.Decision{Allow: true, Reason: "age_ok"},
				Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
			}
			results[idx] = exec.Execute(context.Background(), item, core.ModeExecute)
		}(i)
	}

	wg.Wait()

	// Verify all deletions succeeded or were already_gone (race condition)
	deletedCount := 0
	alreadyGoneCount := 0
	for i, r := range results {
		switch r.Reason {
		case "deleted":
			deletedCount++
		case "already_gone":
			alreadyGoneCount++
		default:
			t.Errorf("file %d: unexpected reason %q", i, r.Reason)
		}
	}

	// All files should be accounted for
	if deletedCount+alreadyGoneCount != numFiles {
		t.Errorf("expected %d total outcomes, got %d deleted + %d already_gone",
			numFiles, deletedCount, alreadyGoneCount)
	}

	// Metrics should be updated (may not equal numFiles due to already_gone)
	m.mu.Lock()
	deletedMetric := m.filesDeleted[dir]
	m.mu.Unlock()
	if deletedMetric == 0 {
		t.Error("expected some files to be deleted")
	}

	// Audit events should be recorded (thread-safe)
	if aud.EventCount() != numFiles {
		t.Errorf("expected %d audit events, got %d", numFiles, aud.EventCount())
	}
}

// TestConcurrentDeletions_MetricsConsistency verifies metrics are consistent
// under concurrent access.
func TestConcurrentDeletions_MetricsConsistency(t *testing.T) {
	dir := t.TempDir()

	// Create files with known sizes
	const numFiles = 20
	const fileSize int64 = 100
	files := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = filepath.Join(dir, "file_"+string(rune('a'+i))+".txt")
		content := make([]byte, fileSize)
		if err := os.WriteFile(files[i], content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	m := newMockMetrics()
	exec := NewSimpleWithMetrics(safe, cfg, nil, m)

	var wg sync.WaitGroup
	var deletedCount atomic.Int32

	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			item := core.PlanItem{
				Candidate: core.Candidate{
					Root:      dir,
					Path:      files[idx],
					Type:      core.TargetFile,
					SizeBytes: fileSize,
				},
				Decision: core.Decision{Allow: true, Reason: "age_ok"},
				Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
			}
			result := exec.Execute(context.Background(), item, core.ModeExecute)
			if result.Deleted {
				deletedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Verify metrics consistency (use mutex for thread-safe access)
	actualDeleted := deletedCount.Load()
	m.mu.Lock()
	filesDeletedCount := m.filesDeleted[dir]
	bytesFreedCount := m.bytesFreed
	m.mu.Unlock()

	if filesDeletedCount != int(actualDeleted) {
		t.Errorf("metrics filesDeleted=%d doesn't match actual deleted=%d",
			filesDeletedCount, actualDeleted)
	}

	expectedBytes := int64(actualDeleted) * fileSize
	if bytesFreedCount != expectedBytes {
		t.Errorf("metrics bytesFreed=%d doesn't match expected=%d",
			bytesFreedCount, expectedBytes)
	}
}

// TestConcurrentDeletions_WithContextCancellation tests behavior when
// context is cancelled during concurrent deletions.
func TestConcurrentDeletions_WithContextCancellation(t *testing.T) {
	dir := t.TempDir()

	const numFiles = 30
	files := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = filepath.Join(dir, "file_"+string(rune('0'+i/10))+string(rune('0'+i%10))+".txt")
		if err := os.WriteFile(files[i], []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	safe := &mockSafety{allowed: true, reason: "ok"}
	cfg := core.SafetyConfig{AllowedRoots: []string{dir}}
	exec := NewSimple(safe, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context is cancelled on all paths

	var wg sync.WaitGroup
	var cancelledCount atomic.Int32

	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Cancel after some deletions start
			if idx == numFiles/2 {
				cancel()
			}
			item := core.PlanItem{
				Candidate: core.Candidate{
					Root:      dir,
					Path:      files[idx],
					Type:      core.TargetFile,
					SizeBytes: 7,
				},
				Decision: core.Decision{Allow: true, Reason: "age_ok"},
				Safety:   core.SafetyVerdict{Allowed: true, Reason: "ok"},
			}
			result := exec.Execute(ctx, item, core.ModeExecute)
			if result.Reason == "ctx_canceled" {
				cancelledCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Some deletions should have been cancelled
	// (exact number depends on timing, so we just verify it handles gracefully)
	t.Logf("Cancelled: %d out of %d", cancelledCount.Load(), numFiles)
}
