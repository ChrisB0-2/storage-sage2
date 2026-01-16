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

	// Should show dry-run in output
	if !strings.Contains(output, "DRY") {
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
		sqlAud.Record(context.Background(), evt)
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
		sqlAud.Record(context.Background(), evt)
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
	sqlAud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "test"})
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
		sqlAud.Record(context.Background(), evt)
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
	sqlAud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "test"})
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

	// Should complete without error
	if strings.Contains(output, "error") && strings.Contains(output, "audit") {
		t.Errorf("audit flags should work, got: %s", output)
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
