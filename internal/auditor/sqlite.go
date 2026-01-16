package auditor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver registration

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// SQLiteAuditor persists audit events to a SQLite database.
// Designed for government/rugged systems requiring:
// - Offline operation (no external dependencies)
// - Long-term log retention
// - Tamper detection via row checksums
// - Simple backup (single file)
type SQLiteAuditor struct {
	db        *sql.DB
	mu        sync.Mutex
	retention time.Duration // 0 = keep forever
}

// SQLiteConfig configures the SQLite auditor.
type SQLiteConfig struct {
	Path      string        // Database file path
	Retention time.Duration // How long to keep logs (0 = forever)
}

// AuditRecord represents a single audit log entry.
type AuditRecord struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Level      string    `json:"level"`
	Action     string    `json:"action"`
	Path       string    `json:"path,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Decision   string    `json:"decision,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Score      int       `json:"score,omitempty"`
	BytesFreed int64     `json:"bytes_freed,omitempty"`
	Error      string    `json:"error,omitempty"`
	Fields     string    `json:"fields,omitempty"` // JSON-encoded extra fields
	Checksum   string    `json:"checksum"`
}

// NewSQLite creates a new SQLite auditor.
func NewSQLite(cfg SQLiteConfig) (*SQLiteAuditor, error) {
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Create schema
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &SQLiteAuditor{
		db:        db,
		retention: cfg.Retention,
	}, nil
}

func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		level TEXT NOT NULL,
		action TEXT NOT NULL,
		path TEXT,
		mode TEXT,
		decision TEXT,
		reason TEXT,
		score INTEGER,
		bytes_freed INTEGER,
		error TEXT,
		fields TEXT,
		checksum TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
	CREATE INDEX IF NOT EXISTS idx_audit_path ON audit_log(path);
	CREATE INDEX IF NOT EXISTS idx_audit_level ON audit_log(level);

	-- Metadata table for database integrity
	CREATE TABLE IF NOT EXISTS audit_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Set creation timestamp if not exists
	_, err := db.Exec(`
		INSERT OR IGNORE INTO audit_meta (key, value)
		VALUES ('created_at', ?)
	`, time.Now().UTC().Format(time.RFC3339))

	return err
}

// Record persists an audit event to the database.
func (a *SQLiteAuditor) Record(ctx context.Context, evt core.AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Extract common fields
	var path, mode, decision, reason, errStr string
	var score int
	var bytesFreed int64

	if evt.Path != "" {
		path = evt.Path
	}
	if evt.Err != nil {
		errStr = evt.Err.Error()
	}

	// Extract from Fields map
	if evt.Fields != nil {
		if v, ok := evt.Fields["mode"].(string); ok {
			mode = v
		}
		if v, ok := evt.Fields["decision"].(string); ok {
			decision = v
		}
		if v, ok := evt.Fields["reason"].(string); ok {
			reason = v
		}
		if v, ok := evt.Fields["score"].(int); ok {
			score = v
		}
		if v, ok := evt.Fields["bytes_freed"].(int64); ok {
			bytesFreed = v
		}
	}

	// Serialize remaining fields as JSON
	fieldsJSON := ""
	if len(evt.Fields) > 0 {
		if b, err := json.Marshal(evt.Fields); err == nil {
			fieldsJSON = string(b)
		}
	}

	// Generate row checksum for tamper detection
	checksum := a.computeChecksum(evt.Time, evt.Level, evt.Action, path, mode, decision, reason, score, bytesFreed, errStr, fieldsJSON)

	// Insert record
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO audit_log (timestamp, level, action, path, mode, decision, reason, score, bytes_freed, error, fields, checksum)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		evt.Time.UTC().Format(time.RFC3339Nano),
		evt.Level,
		evt.Action,
		path,
		mode,
		decision,
		reason,
		score,
		bytesFreed,
		errStr,
		fieldsJSON,
		checksum,
	)

	if err != nil {
		// Log error but don't fail - audit should not break operations
		fmt.Printf("audit write error: %v\n", err)
	}
}

// computeChecksum generates a SHA256 checksum of the record data.
// This allows detection of any tampering with historical records.
func (a *SQLiteAuditor) computeChecksum(ts time.Time, level, action, path, mode, decision, reason string, score int, bytesFreed int64, errStr, fields string) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d|%d|%s|%s",
		ts.UTC().Format(time.RFC3339Nano),
		level, action, path, mode, decision, reason, score, bytesFreed, errStr, fields)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// Close closes the database connection.
func (a *SQLiteAuditor) Close() error {
	return a.db.Close()
}

// Query retrieves audit records matching the given filters.
func (a *SQLiteAuditor) Query(ctx context.Context, filter QueryFilter) ([]AuditRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	query := `SELECT id, timestamp, level, action, path, mode, decision, reason, score, bytes_freed, error, fields, checksum FROM audit_log WHERE 1=1`
	args := []interface{}{}

	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if !filter.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if filter.Level != "" {
		query += " AND level = ?"
		args = append(args, filter.Level)
	}
	if filter.Path != "" {
		query += " AND path LIKE ?"
		args = append(args, "%"+filter.Path+"%")
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	var records []AuditRecord
	for rows.Next() {
		var r AuditRecord
		var ts string
		var path, mode, decision, reason, errStr, fields sql.NullString
		var score sql.NullInt64
		var bytesFreed sql.NullInt64

		err := rows.Scan(&r.ID, &ts, &r.Level, &r.Action, &path, &mode, &decision, &reason, &score, &bytesFreed, &errStr, &fields, &r.Checksum)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		r.Path = path.String
		r.Mode = mode.String
		r.Decision = decision.String
		r.Reason = reason.String
		r.Score = int(score.Int64)
		r.BytesFreed = bytesFreed.Int64
		r.Error = errStr.String
		r.Fields = fields.String

		records = append(records, r)
	}

	return records, rows.Err()
}

// QueryFilter specifies filters for querying audit records.
type QueryFilter struct {
	Since  time.Time
	Until  time.Time
	Action string // plan, delete, error, etc.
	Level  string // info, warn, error
	Path   string // partial match
	Limit  int
}

// VerifyIntegrity checks all records for tampering.
// Returns list of record IDs with invalid checksums.
func (a *SQLiteAuditor) VerifyIntegrity(ctx context.Context) ([]int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	rows, err := a.db.QueryContext(ctx, `
		SELECT id, timestamp, level, action, path, mode, decision, reason, score, bytes_freed, error, fields, checksum
		FROM audit_log ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query for integrity check: %w", err)
	}
	defer rows.Close()

	var tampered []int64
	for rows.Next() {
		var id int64
		var ts, level, action, checksum string
		var path, mode, decision, reason, errStr, fields sql.NullString
		var score, bytesFreed sql.NullInt64

		err := rows.Scan(&id, &ts, &level, &action, &path, &mode, &decision, &reason, &score, &bytesFreed, &errStr, &fields, &checksum)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		timestamp, _ := time.Parse(time.RFC3339Nano, ts)
		expected := a.computeChecksum(timestamp, level, action, path.String, mode.String, decision.String, reason.String, int(score.Int64), bytesFreed.Int64, errStr.String, fields.String)

		if checksum != expected {
			tampered = append(tampered, id)
		}
	}

	return tampered, rows.Err()
}

// Stats returns summary statistics from the audit log.
func (a *SQLiteAuditor) Stats(ctx context.Context) (*AuditStats, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats := &AuditStats{}

	// Total records
	err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log").Scan(&stats.TotalRecords)
	if err != nil {
		return nil, err
	}

	// Date range
	var firstTS, lastTS sql.NullString
	if err := a.db.QueryRowContext(ctx, "SELECT MIN(timestamp), MAX(timestamp) FROM audit_log").Scan(&firstTS, &lastTS); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if firstTS.Valid {
		stats.FirstRecord, _ = time.Parse(time.RFC3339Nano, firstTS.String)
	}
	if lastTS.Valid {
		stats.LastRecord, _ = time.Parse(time.RFC3339Nano, lastTS.String)
	}

	// Total bytes freed
	var totalBytes sql.NullInt64
	if err := a.db.QueryRowContext(ctx, "SELECT SUM(bytes_freed) FROM audit_log WHERE action = 'delete'").Scan(&totalBytes); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	stats.TotalBytesFreed = totalBytes.Int64

	// Files deleted
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE action = 'delete'").Scan(&stats.FilesDeleted); err != nil {
		return nil, err
	}

	// Errors
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE level = 'error'").Scan(&stats.Errors); err != nil {
		return nil, err
	}

	return stats, nil
}

// AuditStats contains summary statistics.
type AuditStats struct {
	TotalRecords    int64
	FirstRecord     time.Time
	LastRecord      time.Time
	TotalBytesFreed int64
	FilesDeleted    int64
	Errors          int64
}

// Prune removes records older than the retention period.
func (a *SQLiteAuditor) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339Nano)
	result, err := a.db.ExecContext(ctx, "DELETE FROM audit_log WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Export writes all records to JSON format.
func (a *SQLiteAuditor) Export(ctx context.Context, since time.Time) ([]byte, error) {
	records, err := a.Query(ctx, QueryFilter{Since: since, Limit: 0})
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(records, "", "  ")
}

// Ensure SQLiteAuditor implements core.Auditor
var _ core.Auditor = (*SQLiteAuditor)(nil)
