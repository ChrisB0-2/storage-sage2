package auditor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestSQLiteAuditor_Record(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	aud, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	// Record an event
	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: "plan",
		Path:   "/tmp/test.txt",
		Fields: map[string]any{
			"decision": "allow",
			"reason":   "age_ok",
			"score":    100,
		},
	}

	aud.Record(context.Background(), evt)

	// Query it back
	records, err := aud.Query(context.Background(), QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	if records[0].Action != "plan" {
		t.Errorf("expected action 'plan', got %q", records[0].Action)
	}
	if records[0].Path != "/tmp/test.txt" {
		t.Errorf("expected path '/tmp/test.txt', got %q", records[0].Path)
	}
	if records[0].Checksum == "" {
		t.Error("expected checksum to be set")
	}
}

func TestSQLiteAuditor_Query(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	aud, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	// Record multiple events
	events := []core.AuditEvent{
		{Time: time.Now().Add(-2 * time.Hour), Level: "info", Action: "plan", Path: "/tmp/a.txt"},
		{Time: time.Now().Add(-1 * time.Hour), Level: "info", Action: "delete", Path: "/tmp/b.txt", Fields: map[string]any{"bytes_freed": int64(1024)}},
		{Time: time.Now(), Level: "error", Action: "delete", Path: "/tmp/c.txt", Err: fmt.Errorf("permission denied")},
	}

	for _, evt := range events {
		aud.Record(context.Background(), evt)
	}

	// Query by action
	records, err := aud.Query(context.Background(), QueryFilter{Action: "delete"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 delete records, got %d", len(records))
	}

	// Query by level
	records, err = aud.Query(context.Background(), QueryFilter{Level: "error"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 error record, got %d", len(records))
	}

	// Query by path
	records, err = aud.Query(context.Background(), QueryFilter{Path: "b.txt"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record with path containing 'b.txt', got %d", len(records))
	}

	// Query with limit
	records, err = aud.Query(context.Background(), QueryFilter{Limit: 1})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record with limit, got %d", len(records))
	}
}

func TestSQLiteAuditor_VerifyIntegrity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	aud, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	// Record an event
	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: "plan",
		Path:   "/tmp/test.txt",
	}
	aud.Record(context.Background(), evt)

	// Verify integrity - should pass
	tampered, err := aud.VerifyIntegrity(context.Background())
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if len(tampered) != 0 {
		t.Errorf("expected no tampered records, got %d", len(tampered))
	}

	// Tamper with the record
	_, err = aud.db.Exec("UPDATE audit_log SET path = '/tampered/path' WHERE id = 1")
	if err != nil {
		t.Fatalf("failed to tamper: %v", err)
	}

	// Verify again - should detect tampering
	tampered, err = aud.VerifyIntegrity(context.Background())
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if len(tampered) != 1 {
		t.Errorf("expected 1 tampered record, got %d", len(tampered))
	}

	aud.Close()
}

func TestSQLiteAuditor_Stats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	aud, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	// Record events with proper reason fields for stats tracking
	// - FilesDeleted: action="execute" AND reason="deleted"
	// - FilesTrashed: action="execute" AND reason="trashed"
	// - PlanEvents: action="plan"
	// - ExecuteEvents: action="execute" (all)
	events := []core.AuditEvent{
		{Time: time.Now(), Level: "info", Action: "execute", Fields: map[string]any{"result_reason": "deleted", "bytes_freed": int64(1024)}},
		{Time: time.Now(), Level: "info", Action: "execute", Fields: map[string]any{"result_reason": "trashed", "bytes_freed": int64(2048)}},
		{Time: time.Now(), Level: "info", Action: "plan", Fields: map[string]any{"policy_reason": "age_ok"}},
		{Time: time.Now(), Level: "info", Action: "plan", Fields: map[string]any{"policy_reason": "too_new"}},
		{Time: time.Now(), Level: "error", Action: "execute", Fields: map[string]any{"result_reason": "delete_failed"}},
	}
	for _, evt := range events {
		aud.Record(context.Background(), evt)
	}

	stats, err := aud.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if stats.TotalRecords != 5 {
		t.Errorf("expected 5 total records, got %d", stats.TotalRecords)
	}
	if stats.FilesDeleted != 1 {
		t.Errorf("expected 1 deleted (reason='deleted'), got %d", stats.FilesDeleted)
	}
	if stats.FilesTrashed != 1 {
		t.Errorf("expected 1 trashed (reason='trashed'), got %d", stats.FilesTrashed)
	}
	if stats.FilesProcessed != 2 {
		t.Errorf("expected 2 processed (deleted + trashed), got %d", stats.FilesProcessed)
	}
	if stats.PlanEvents != 2 {
		t.Errorf("expected 2 plan events, got %d", stats.PlanEvents)
	}
	if stats.ExecuteEvents != 3 {
		t.Errorf("expected 3 execute events, got %d", stats.ExecuteEvents)
	}
	if stats.Errors != 1 {
		t.Errorf("expected 1 error, got %d", stats.Errors)
	}
	if stats.TotalBytesFreed != 3072 {
		t.Errorf("expected 3072 bytes freed, got %d", stats.TotalBytesFreed)
	}
}

func TestSQLiteAuditor_Prune(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	aud, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}
	defer aud.Close()

	// Record old and new events
	oldEvt := core.AuditEvent{Time: time.Now().Add(-48 * time.Hour), Level: "info", Action: "plan"}
	newEvt := core.AuditEvent{Time: time.Now(), Level: "info", Action: "plan"}

	aud.Record(context.Background(), oldEvt)
	aud.Record(context.Background(), newEvt)

	// Prune records older than 24 hours
	deleted, err := aud.Prune(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Verify only new record remains
	records, _ := aud.Query(context.Background(), QueryFilter{})
	if len(records) != 1 {
		t.Errorf("expected 1 remaining record, got %d", len(records))
	}
}

func TestSQLiteAuditor_Persistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_audit.db")

	// Create and write
	aud1, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create auditor: %v", err)
	}

	evt := core.AuditEvent{Time: time.Now(), Level: "info", Action: "test"}
	aud1.Record(context.Background(), evt)
	aud1.Close()

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file should exist")
	}

	// Reopen and read
	aud2, err := NewSQLite(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to reopen auditor: %v", err)
	}
	defer aud2.Close()

	records, err := aud2.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 persisted record, got %d", len(records))
	}
}
