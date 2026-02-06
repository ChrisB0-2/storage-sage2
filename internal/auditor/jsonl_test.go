package auditor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestJSONLAuditor_Record(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	evt := core.AuditEvent{
		Time:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Level:  "info",
		Action: "plan",
		Path:   "/tmp/test.txt",
		Fields: map[string]any{
			"decision": "allow",
			"reason":   "age_ok",
			"score":    100,
		},
	}

	_ = aud.Record(context.Background(), evt)

	// Check error state
	if err := aud.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Close to flush
	if err := aud.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Read and parse the file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify fields
	if record["level"] != "info" {
		t.Errorf("expected level 'info', got %v", record["level"])
	}
	if record["action"] != "plan" {
		t.Errorf("expected action 'plan', got %v", record["action"])
	}
	if record["path"] != "/tmp/test.txt" {
		t.Errorf("expected path '/tmp/test.txt', got %v", record["path"])
	}

	// Check fields sub-object
	fields, ok := record["fields"].(map[string]any)
	if !ok {
		t.Fatal("expected fields to be a map")
	}
	if fields["decision"] != "allow" {
		t.Errorf("expected decision 'allow', got %v", fields["decision"])
	}
}

func TestJSONLAuditor_RecordWithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "error",
		Action: "delete",
		Path:   "/tmp/fail.txt",
		Err:    errors.New("permission denied"),
	}

	_ = aud.Record(context.Background(), evt)
	aud.Close()

	// Read and verify error field
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["err"] != "permission denied" {
		t.Errorf("expected err 'permission denied', got %v", record["err"])
	}
}

func TestJSONLAuditor_AutoSetTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	before := time.Now()

	// Record event without time
	evt := core.AuditEvent{
		Level:  "info",
		Action: "test",
	}
	_ = aud.Record(context.Background(), evt)
	aud.Close()

	// Read and verify time was set
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	timeStr, ok := record["time"].(string)
	if !ok {
		t.Fatal("expected time to be a string")
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	if parsedTime.Before(before) {
		t.Errorf("time should be after test start")
	}
}

func TestJSONLAuditor_MultipleRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	// Record multiple events
	for i := 0; i < 5; i++ {
		evt := core.AuditEvent{
			Time:   time.Now(),
			Level:  "info",
			Action: "plan",
			Path:   "/tmp/file" + string(rune('0'+i)) + ".txt",
		}
		_ = aud.Record(context.Background(), evt)
	}
	aud.Close()

	// Read and count lines
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lineCount++

		// Verify each line is valid JSON
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d is not valid JSON: %v", lineCount, err)
		}
	}

	if lineCount != 5 {
		t.Errorf("expected 5 lines, got %d", lineCount)
	}
}

func TestJSONLAuditor_AppendBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	// First auditor session
	aud1, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	_ = aud1.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "first"})
	aud1.Close()

	// Second auditor session (should append)
	aud2, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create second auditor: %v", err)
	}
	_ = aud2.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info", Action: "second"})
	aud2.Close()

	// Read and verify both records exist
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// Verify order
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)

	if first["action"] != "first" {
		t.Errorf("expected first action 'first', got %v", first["action"])
	}
	if second["action"] != "second" {
		t.Errorf("expected second action 'second', got %v", second["action"])
	}
}

func TestJSONLAuditor_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	_ = aud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info"})
	aud.Close()

	// Check file permissions (should be 0600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("expected permissions 0600, got %o", perm)
	}
}

func TestJSONLAuditor_InvalidPath(t *testing.T) {
	// Try to create auditor in non-existent directory
	_, err := NewJSONL("/nonexistent/path/audit.jsonl")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestJSONLAuditor_DoubleClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	// First close
	if err := aud.Close(); err != nil {
		t.Errorf("first close failed: %v", err)
	}

	// Second close should not panic or error
	if err := aud.Close(); err != nil {
		t.Errorf("second close should not error: %v", err)
	}
}

func TestJSONLAuditor_RecordAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	aud.Close()

	// Record after close should not panic
	_ = aud.Record(context.Background(), core.AuditEvent{Time: time.Now(), Level: "info"})

	// File should be empty or only contain records from before close
	data, _ := os.ReadFile(path)
	if len(data) > 0 {
		t.Errorf("expected empty file after close, got %d bytes", len(data))
	}
}

func TestJSONLAuditor_ConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				evt := core.AuditEvent{
					Time:   time.Now(),
					Level:  "info",
					Action: "test",
					Fields: map[string]any{"goroutine": id, "iteration": j},
				}
				_ = aud.Record(context.Background(), evt)
			}
		}(i)
	}

	wg.Wait()
	aud.Close()

	// Verify no write errors
	if err := aud.Err(); err != nil {
		t.Errorf("write error: %v", err)
	}

	// Count lines (each record should be one line)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	expected := goroutines * iterations
	if len(lines) != expected {
		t.Errorf("expected %d lines, got %d", expected, len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i+1, err)
		}
	}
}

func TestJSONLAuditor_OmitsEmptyFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	aud, err := NewJSONL(path)
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	// Record event without error or fields
	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: "test",
		Path:   "/test",
	}
	_ = aud.Record(context.Background(), evt)
	aud.Close()

	// Read and verify fields/err are omitted
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Check that "fields" and "err" keys are not present
	if strings.Contains(string(data), `"fields"`) {
		t.Error("expected fields to be omitted when empty")
	}
	if strings.Contains(string(data), `"err"`) {
		t.Error("expected err to be omitted when empty")
	}
}
