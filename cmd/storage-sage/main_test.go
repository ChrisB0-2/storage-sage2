package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/auditor"
	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/executor"
	"github.com/ChrisB0-2/storage-sage/internal/planner"
	"github.com/ChrisB0-2/storage-sage/internal/policy"
	"github.com/ChrisB0-2/storage-sage/internal/safety"
	"github.com/ChrisB0-2/storage-sage/internal/scanner"
)

// TestVersionFlag tests the -version flag
func TestVersionFlag(t *testing.T) {
	output := runCLI(t, "-version")
	if !strings.Contains(output, "storage-sage") {
		t.Errorf("expected version output to contain 'storage-sage', got: %s", output)
	}
}

// TestHelpOutput tests that running without arguments shows help-like output
func TestHelpOutput(t *testing.T) {
	// Running with -help should not error
	cmd := exec.Command("go", "run", ".", "-help")
	cmd.Dir = getCmdDir(t)

	// -help exits with 0, capture output
	output, _ := cmd.CombinedOutput()
	outputStr := string(output)

	// Should contain usage information
	if !strings.Contains(outputStr, "Usage") && !strings.Contains(outputStr, "usage") {
		// At minimum should have flag info
		if !strings.Contains(outputStr, "-root") {
			t.Errorf("expected help output to contain flag info, got: %s", outputStr)
		}
	}
}

// TestDryRunMode tests dry-run execution with a temp directory
func TestDryRunMode(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create some old files (older than 30 days)
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	for i := 0; i < 3; i++ {
		path := filepath.Join(tmpDir, "old_file_"+string(rune('0'+i))+".tmp")
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set file time: %v", err)
		}
	}

	// Run in dry-run mode
	output := runCLI(t, "-root", tmpDir, "-mode", "dry-run", "-min-age-days", "30", "-max", "10")

	// Should show dry-run in output (structured log format)
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected output to indicate dry-run mode, got: %s", output)
	}

	// Files should still exist
	for i := 0; i < 3; i++ {
		path := filepath.Join(tmpDir, "old_file_"+string(rune('0'+i))+".tmp")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("file should not be deleted in dry-run mode")
		}
	}
}

// TestConfigFileLoading tests loading configuration from a file
func TestConfigFileLoading(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config file
	configContent := `
version: 1
scan:
  roots:
    - /tmp
  recursive: true
  max_depth: 5
policy:
  min_age_days: 7
execution:
  mode: dry-run
  timeout: 10s
  max_items: 5
logging:
  level: info
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run with config file
	output := runCLI(t, "-config", configPath, "-root", tmpDir)

	// Should run without error
	if strings.Contains(output, "error: invalid config") {
		t.Errorf("config should be valid, got: %s", output)
	}
}

// TestFlagOverridesConfig tests that CLI flags override config file values
func TestFlagOverridesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config file with dry-run mode
	configContent := `
version: 1
scan:
  roots:
    - /nonexistent
policy:
  min_age_days: 30
execution:
  mode: dry-run
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Override root with flag
	output := runCLI(t, "-config", configPath, "-root", tmpDir, "-min-age-days", "1")

	// Should use the flag value, not the config value
	if strings.Contains(output, "/nonexistent") {
		t.Error("flag should override config root")
	}
}

// TestQuerySubcommand tests the query subcommand
func TestQuerySubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	// Create an audit database with some records
	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	// Record some events
	events := []core.AuditEvent{
		{Time: time.Now(), Level: "info", Action: "plan", Path: "/tmp/a.txt"},
		{Time: time.Now(), Level: "info", Action: "delete", Path: "/tmp/b.txt"},
	}
	for _, evt := range events {
		_ = sqlAud.Record(context.Background(), evt)
	}
	sqlAud.Close()

	// Run query subcommand
	output := runCLI(t, "query", "-db", dbPath, "-limit", "10")

	// Should show found records
	if !strings.Contains(output, "Found") || !strings.Contains(output, "records") {
		t.Errorf("expected query output to show found records, got: %s", output)
	}
}

// TestQuerySubcommandWithFilters tests query filtering
func TestQuerySubcommandWithFilters(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	// Create an audit database
	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	events := []core.AuditEvent{
		{Time: time.Now(), Level: "info", Action: "plan", Path: "/tmp/a.txt"},
		{Time: time.Now(), Level: "error", Action: "delete", Path: "/tmp/b.txt"},
	}
	for _, evt := range events {
		_ = sqlAud.Record(context.Background(), evt)
	}
	sqlAud.Close()

	// Filter by level
	output := runCLI(t, "query", "-db", dbPath, "-level", "error")
	if !strings.Contains(output, "1 record") {
		t.Errorf("expected 1 error record, got: %s", output)
	}
}

// TestQuerySubcommandJSON tests JSON output format
func TestQuerySubcommandJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	_ = sqlAud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "test"})
	sqlAud.Close()

	output := runCLI(t, "query", "-db", dbPath, "-json")

	// Should be valid JSON (starts with [ for array)
	if !strings.HasPrefix(strings.TrimSpace(output), "[") {
		t.Errorf("expected JSON array output, got: %s", output)
	}
}

// TestStatsSubcommand tests the stats subcommand
func TestStatsSubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	// Create database with records
	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	events := []core.AuditEvent{
		{Time: time.Now(), Level: "info", Action: "delete", Fields: map[string]any{"bytes_freed": int64(1024)}},
		{Time: time.Now(), Level: "info", Action: "delete", Fields: map[string]any{"bytes_freed": int64(2048)}},
	}
	for _, evt := range events {
		_ = sqlAud.Record(context.Background(), evt)
	}
	sqlAud.Close()

	output := runCLI(t, "stats", "-db", dbPath)

	// Should show statistics
	if !strings.Contains(output, "Total Records") {
		t.Errorf("expected stats output, got: %s", output)
	}
	if !strings.Contains(output, "Total Bytes Freed") {
		t.Errorf("expected bytes freed in stats, got: %s", output)
	}
}

// TestVerifySubcommand tests the verify subcommand
func TestVerifySubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "audit.db")

	// Create database with records
	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	_ = sqlAud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "test"})
	sqlAud.Close()

	output := runCLI(t, "verify", "-db", dbPath)

	// Should pass verification
	if !strings.Contains(output, "PASS") {
		t.Errorf("expected verification to pass, got: %s", output)
	}
}

// TestMissingRequiredArgs tests error handling for missing arguments
func TestMissingRequiredArgs(t *testing.T) {
	// Query without -db should fail
	output, exitCode := runCLIWithExitCode(t, "query")
	if exitCode == 0 {
		t.Error("expected non-zero exit code for missing -db")
	}
	if !strings.Contains(output, "-db is required") {
		t.Errorf("expected error about missing -db, got: %s", output)
	}
}

// TestInvalidConfig tests handling of invalid config files
func TestInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Create an invalid YAML file
	if err := os.WriteFile(configPath, []byte("invalid: [yaml: content"), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	output, exitCode := runCLIWithExitCode(t, "-config", configPath)
	if exitCode == 0 {
		t.Error("expected non-zero exit code for invalid config")
	}
	if !strings.Contains(strings.ToLower(output), "error") {
		t.Errorf("expected error message, got: %s", output)
	}
}

// TestProtectedPathsFlag tests the -protected flag
func TestProtectedPathsFlag(t *testing.T) {
	tmpDir := t.TempDir()

	// Run with additional protected paths
	output := runCLI(t, "-root", tmpDir, "-mode", "dry-run", "-protected", "/custom/path,/another/path")

	// Should complete without error (protected paths are merged)
	if strings.Contains(output, "error") && !strings.Contains(output, "DRY") {
		t.Errorf("unexpected error with protected paths: %s", output)
	}
}

// TestExtensionsFlag tests the -extensions flag
func TestExtensionsFlag(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with different extensions
	for _, ext := range []string{".tmp", ".log", ".txt"} {
		path := filepath.Join(tmpDir, "file"+ext)
		_ = os.WriteFile(path, []byte("test"), 0644)
		// Make old
		oldTime := time.Now().Add(-40 * 24 * time.Hour)
		_ = os.Chtimes(path, oldTime, oldTime)
	}

	// Run with extensions filter
	output := runCLI(t, "-root", tmpDir, "-mode", "dry-run", "-extensions", ".tmp,.log", "-min-age-days", "30")

	// Should complete (not checking exact behavior, just that flag is accepted)
	if strings.Contains(output, "unknown flag") {
		t.Errorf("extensions flag should be accepted, got: %s", output)
	}
}

// TestExclusionsFlag tests the -exclude flag
func TestExclusionsFlag(t *testing.T) {
	tmpDir := t.TempDir()

	output := runCLI(t, "-root", tmpDir, "-mode", "dry-run", "-exclude", "*.important,keep-*")

	if strings.Contains(output, "unknown flag") {
		t.Errorf("exclude flag should be accepted, got: %s", output)
	}
}

// TestAuditFlags tests audit-related flags
func TestAuditFlags(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.jsonl")
	auditDBPath := filepath.Join(tmpDir, "audit.db")

	output := runCLI(t, "-root", tmpDir, "-mode", "dry-run", "-audit", auditPath, "-audit-db", auditDBPath)

	// Should complete without audit-specific errors
	// Note: we check for specific audit failure patterns, not generic "error" + "audit"
	// since other errors (e.g., metrics port conflicts) may appear alongside audit logs
	auditErrorPatterns := []string{
		"failed to open audit",
		"failed to initialize audit",
		"audit write error",
		"failed to create audit",
	}
	for _, pattern := range auditErrorPatterns {
		if strings.Contains(output, pattern) {
			t.Errorf("audit flags should work, found error pattern %q in: %s", pattern, output)
		}
	}

	// Audit file should be created (may be empty if no candidates)
	// We don't check the file existence since it depends on whether there were candidates
}

// TestParseTimeArg tests the time argument parsing function
func TestParseTimeArg(t *testing.T) {
	tests := []struct {
		input    string
		wantZero bool
	}{
		{"24h", false},
		{"7d", false},
		{"30m", false},
		{"2024-01-15", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		result := parseTimeArg(tt.input)
		isZero := result.IsZero()
		if isZero != tt.wantZero {
			t.Errorf("parseTimeArg(%q): got zero=%v, want zero=%v", tt.input, isZero, tt.wantZero)
		}
	}
}

// TestFormatBytesHuman tests the byte formatting function
func TestFormatBytesHuman(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		got := formatBytesHuman(tt.bytes)
		if got != tt.want {
			t.Errorf("formatBytesHuman(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// runCLI runs the CLI with given arguments and returns stdout/stderr combined
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	output, _ := runCLIWithExitCode(t, args...)
	return output
}

// runCLIWithExitCode runs the CLI and returns output and exit code
func runCLIWithExitCode(t *testing.T, args ...string) (string, int) {
	t.Helper()

	cmdArgs := append([]string{"run", "."}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = getCmdDir(t)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run command: %v", err)
		}
	}

	return output, exitCode
}

// getCmdDir returns the directory containing the main package
func getCmdDir(t *testing.T) string {
	t.Helper()
	// Get the directory of this test file
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	return dir
}

// ============================================================================
// End-to-End Pipeline Tests
// ============================================================================

// TestE2E_FullPipeline_ScanPlanExecute tests the complete pipeline:
// scan → policy → safety → execute with real filesystem verification.
func TestE2E_FullPipeline_ScanPlanExecute(t *testing.T) {
	// Create test directory structure:
	// root/
	//   old_eligible.tmp      (old, small, .tmp extension - SHOULD BE DELETED)
	//   old_eligible.log      (old, .log extension - SHOULD BE DELETED)
	//   new_file.tmp          (new, .tmp extension - SHOULD BE PRESERVED: too new)
	//   old_excluded.tmp      (old, matches exclusion - SHOULD BE PRESERVED)
	//   important.txt         (old, wrong extension - SHOULD BE PRESERVED)
	//   protected/            (directory - SHOULD BE PRESERVED: protected)
	//     secret.tmp          (old, .tmp - SHOULD BE PRESERVED: under protected path)

	root := t.TempDir()
	protectedDir := filepath.Join(root, "protected")
	if err := os.MkdirAll(protectedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Helper to create file with specific age
	createFile := func(path string, content string, daysOld int) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if daysOld > 0 {
			oldTime := time.Now().Add(-time.Duration(daysOld) * 24 * time.Hour)
			if err := os.Chtimes(path, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Files that SHOULD be deleted (old enough, matching extension, not excluded)
	oldEligibleTmp := filepath.Join(root, "old_eligible.tmp")
	oldEligibleLog := filepath.Join(root, "old_eligible.log")
	createFile(oldEligibleTmp, "delete me tmp", 40)
	createFile(oldEligibleLog, "delete me log", 40)

	// Files that SHOULD be preserved
	newFileTmp := filepath.Join(root, "new_file.tmp")
	oldExcludedTmp := filepath.Join(root, "keep_old_excluded.tmp") // matches "keep_*" exclusion
	importantTxt := filepath.Join(root, "important.txt")
	protectedSecret := filepath.Join(protectedDir, "secret.tmp")
	createFile(newFileTmp, "too new", 5)         // Only 5 days old
	createFile(oldExcludedTmp, "excluded", 40)   // Old but excluded by pattern
	createFile(importantTxt, "wrong ext", 40)    // Old but wrong extension
	createFile(protectedSecret, "protected", 40) // Old but under protected path

	// Set up audit database
	auditDBPath := filepath.Join(t.TempDir(), "audit.db")
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: auditDBPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	// Initialize components
	scan := scanner.NewWalkDir()
	plan := planner.NewSimple()
	safeEngine := safety.New()
	exec := executor.NewSimple(safeEngine, core.SafetyConfig{
		AllowedRoots:   []string{root},
		ProtectedPaths: []string{protectedDir},
	}).WithAuditor(aud)

	// Policy: age >= 30 days AND extensions .tmp or .log AND NOT keep_*
	agePolicy := policy.NewAgePolicy(30)
	extPolicy := policy.NewExtensionPolicy([]string{".tmp", ".log"})
	exclPolicy := policy.NewExclusionPolicy([]string{"keep_*"})
	compositePolicy := policy.NewCompositePolicy(policy.ModeAnd, agePolicy, extPolicy, exclPolicy)

	safetyCfg := core.SafetyConfig{
		AllowedRoots:   []string{root},
		ProtectedPaths: []string{protectedDir},
		AllowDirDelete: false,
	}

	env := core.EnvSnapshot{Now: time.Now()}

	// === PHASE 1: SCAN ===
	ctx := context.Background()
	scanReq := core.ScanRequest{
		Roots:        []string{root},
		Recursive:    true,
		IncludeFiles: true,
		IncludeDirs:  false,
	}

	candCh, errCh := scan.Scan(ctx, scanReq)

	// Drain error channel in background
	go func() {
		for err := range errCh {
			if err != nil {
				t.Logf("scan error: %v", err)
			}
		}
	}()

	// === PHASE 2: PLAN ===
	planItems, err := plan.BuildPlan(ctx, candCh, compositePolicy, safeEngine, env, safetyCfg)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	t.Logf("Plan built with %d items", len(planItems))

	// === PHASE 3: EXECUTE ===
	var results []core.ActionResult
	for _, item := range planItems {
		// Record plan event
		_ = aud.Record(ctx, core.NewPlanAuditEvent(root, core.ModeExecute, item))

		// Execute
		result := exec.Execute(ctx, item, core.ModeExecute)
		results = append(results, result)
	}

	// === PHASE 4: VERIFY ===

	// Check which files still exist
	filesExist := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// Files that SHOULD have been deleted
	if filesExist(oldEligibleTmp) {
		t.Error("old_eligible.tmp should have been deleted")
	}
	if filesExist(oldEligibleLog) {
		t.Error("old_eligible.log should have been deleted")
	}

	// Files that SHOULD be preserved
	if !filesExist(newFileTmp) {
		t.Error("new_file.tmp should be preserved (too new)")
	}
	if !filesExist(oldExcludedTmp) {
		t.Error("keep_old_excluded.tmp should be preserved (excluded by pattern)")
	}
	if !filesExist(importantTxt) {
		t.Error("important.txt should be preserved (wrong extension)")
	}
	if !filesExist(protectedSecret) {
		t.Error("protected/secret.tmp should be preserved (under protected path)")
	}

	// Verify results breakdown
	var deleted, preserved int
	for _, r := range results {
		if r.Deleted {
			deleted++
		} else {
			preserved++
		}
	}

	t.Logf("Results: %d deleted, %d preserved", deleted, preserved)

	if deleted != 2 {
		t.Errorf("expected 2 files deleted, got %d", deleted)
	}

	// Verify audit records
	records, err := aud.Query(ctx, auditor.QueryFilter{Limit: 100})
	if err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}

	t.Logf("Audit records: %d", len(records))

	// Should have plan events + execute events
	if len(records) < len(planItems) {
		t.Errorf("expected at least %d audit records, got %d", len(planItems), len(records))
	}

	// Verify audit integrity
	tampered, err := aud.VerifyIntegrity(ctx)
	if err != nil {
		t.Fatalf("failed to verify audit integrity: %v", err)
	}
	if len(tampered) > 0 {
		t.Errorf("audit integrity check failed: %d tampered records", len(tampered))
	}
}

// TestE2E_DryRunPreservesAllFiles tests that dry-run mode doesn't delete anything.
func TestE2E_DryRunPreservesAllFiles(t *testing.T) {
	root := t.TempDir()

	// Create old files that would be eligible for deletion
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	for i := 0; i < 5; i++ {
		path := filepath.Join(root, "old_file_"+string(rune('0'+i))+".tmp")
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize pipeline
	scan := scanner.NewWalkDir()
	plan := planner.NewSimple()
	safeEngine := safety.New()
	exec := executor.NewSimple(safeEngine, core.SafetyConfig{
		AllowedRoots: []string{root},
	})

	pol := policy.NewAgePolicy(30)
	safetyCfg := core.SafetyConfig{AllowedRoots: []string{root}}
	env := core.EnvSnapshot{Now: time.Now()}

	ctx := context.Background()

	// Scan
	candCh, errCh := scan.Scan(ctx, core.ScanRequest{
		Roots:        []string{root},
		Recursive:    true,
		IncludeFiles: true,
	})
	go func() {
		for range errCh {
		}
	}()

	// Plan
	planItems, err := plan.BuildPlan(ctx, candCh, pol, safeEngine, env, safetyCfg)
	if err != nil {
		t.Fatal(err)
	}

	// Execute in DRY-RUN mode
	for _, item := range planItems {
		result := exec.Execute(ctx, item, core.ModeDryRun)
		if result.Deleted {
			t.Errorf("dry-run should not delete files: %s", result.Path)
		}
		if result.Reason != "would_delete" && result.Reason != "policy_deny:too_new" {
			// Could be policy deny if file was created just now
			t.Logf("unexpected reason for %s: %s", result.Path, result.Reason)
		}
	}

	// Verify all files still exist
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 files preserved, got %d", len(entries))
	}
}

// TestE2E_ProtectedPaths tests that protected paths are never deleted.
func TestE2E_ProtectedPaths(t *testing.T) {
	root := t.TempDir()
	protectedDir := filepath.Join(root, "system")
	if err := os.MkdirAll(protectedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create old files in both locations
	oldTime := time.Now().Add(-40 * 24 * time.Hour)

	regularFile := filepath.Join(root, "regular.tmp")
	protectedFile := filepath.Join(protectedDir, "config.tmp")

	if err := os.WriteFile(regularFile, []byte("regular"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protectedFile, []byte("protected"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(regularFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(protectedFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Pipeline with protected path
	scan := scanner.NewWalkDir()
	plan := planner.NewSimple()
	safeEngine := safety.New()

	safetyCfg := core.SafetyConfig{
		AllowedRoots:   []string{root},
		ProtectedPaths: []string{protectedDir},
	}

	exec := executor.NewSimple(safeEngine, safetyCfg)
	pol := policy.NewAgePolicy(30)
	env := core.EnvSnapshot{Now: time.Now()}
	ctx := context.Background()

	// Scan and plan
	candCh, errCh := scan.Scan(ctx, core.ScanRequest{
		Roots:        []string{root},
		Recursive:    true,
		IncludeFiles: true,
	})
	go func() {
		for range errCh {
		}
	}()

	planItems, err := plan.BuildPlan(ctx, candCh, pol, safeEngine, env, safetyCfg)
	if err != nil {
		t.Fatal(err)
	}

	// Execute
	for _, item := range planItems {
		exec.Execute(ctx, item, core.ModeExecute)
	}

	// Regular file should be deleted
	if _, err := os.Stat(regularFile); err == nil {
		t.Error("regular.tmp should have been deleted")
	}

	// Protected file should still exist
	if _, err := os.Stat(protectedFile); err != nil {
		t.Error("protected config.tmp should NOT have been deleted")
	}
}

// TestE2E_MultipleRoots tests scanning and cleaning multiple root directories.
func TestE2E_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	oldTime := time.Now().Add(-40 * 24 * time.Hour)

	// Create files in both roots
	file1 := filepath.Join(root1, "file1.tmp")
	file2 := filepath.Join(root2, "file2.tmp")

	if err := os.WriteFile(file1, []byte("root1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("root2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file1, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file2, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Pipeline with multiple roots
	scan := scanner.NewWalkDir()
	plan := planner.NewSimple()
	safeEngine := safety.New()

	safetyCfg := core.SafetyConfig{
		AllowedRoots: []string{root1, root2},
	}

	exec := executor.NewSimple(safeEngine, safetyCfg)
	pol := policy.NewAgePolicy(30)
	env := core.EnvSnapshot{Now: time.Now()}
	ctx := context.Background()

	// Scan both roots
	candCh, errCh := scan.Scan(ctx, core.ScanRequest{
		Roots:        []string{root1, root2},
		Recursive:    true,
		IncludeFiles: true,
	})
	go func() {
		for range errCh {
		}
	}()

	planItems, err := plan.BuildPlan(ctx, candCh, pol, safeEngine, env, safetyCfg)
	if err != nil {
		t.Fatal(err)
	}

	// Should have found files from both roots
	var fromRoot1, fromRoot2 int
	for _, item := range planItems {
		if strings.HasPrefix(item.Candidate.Path, root1) {
			fromRoot1++
		}
		if strings.HasPrefix(item.Candidate.Path, root2) {
			fromRoot2++
		}
	}

	if fromRoot1 == 0 {
		t.Error("no files found from root1")
	}
	if fromRoot2 == 0 {
		t.Error("no files found from root2")
	}

	// Execute
	var deleted int
	for _, item := range planItems {
		result := exec.Execute(ctx, item, core.ModeExecute)
		if result.Deleted {
			deleted++
		}
	}

	// Both files should be deleted
	if deleted != 2 {
		t.Errorf("expected 2 files deleted from both roots, got %d", deleted)
	}
}

// TestE2E_AuditRecordsMatchActions verifies audit records accurately reflect actions.
func TestE2E_AuditRecordsMatchActions(t *testing.T) {
	root := t.TempDir()
	auditDBPath := filepath.Join(t.TempDir(), "audit.db")

	// Create mixed files
	oldTime := time.Now().Add(-40 * 24 * time.Hour)

	toDelete := filepath.Join(root, "delete_me.tmp")
	toPreserve := filepath.Join(root, "preserve_me.txt") // wrong extension

	if err := os.WriteFile(toDelete, []byte("delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toPreserve, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(toDelete, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(toPreserve, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Set up auditor
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: auditDBPath})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	// Pipeline
	scan := scanner.NewWalkDir()
	plan := planner.NewSimple()
	safeEngine := safety.New()

	safetyCfg := core.SafetyConfig{AllowedRoots: []string{root}}
	exec := executor.NewSimple(safeEngine, safetyCfg).WithAuditor(aud)

	// Policy requires .tmp extension
	agePolicy := policy.NewAgePolicy(30)
	extPolicy := policy.NewExtensionPolicy([]string{".tmp"})
	pol := policy.NewCompositePolicy(policy.ModeAnd, agePolicy, extPolicy)

	env := core.EnvSnapshot{Now: time.Now()}
	ctx := context.Background()

	// Scan and plan
	candCh, errCh := scan.Scan(ctx, core.ScanRequest{
		Roots:        []string{root},
		Recursive:    true,
		IncludeFiles: true,
	})
	go func() {
		for range errCh {
		}
	}()

	planItems, err := plan.BuildPlan(ctx, candCh, pol, safeEngine, env, safetyCfg)
	if err != nil {
		t.Fatal(err)
	}

	// Execute and track actual outcomes
	actualDeleted := make(map[string]bool)
	for _, item := range planItems {
		result := exec.Execute(ctx, item, core.ModeExecute)
		if result.Deleted {
			actualDeleted[result.Path] = true
		}
	}

	// Query audit records
	records, err := aud.Query(ctx, auditor.QueryFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	// Verify audit records match reality
	for _, rec := range records {
		if rec.Action == "delete" {
			if !actualDeleted[rec.Path] {
				t.Errorf("audit says deleted %s but it wasn't actually deleted", rec.Path)
			}
		}
	}

	// Verify delete_me.tmp was deleted
	if !actualDeleted[toDelete] {
		t.Error("delete_me.tmp should have been deleted")
	}

	// Verify preserve_me.txt was NOT deleted
	if actualDeleted[toPreserve] {
		t.Error("preserve_me.txt should NOT have been deleted")
	}
}
