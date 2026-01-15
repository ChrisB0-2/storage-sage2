package auditor

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// JSONLAuditor appends one JSON object per line (JSONL).
// It is simple, durable, and easy to ingest later.
type JSONLAuditor struct {
	mu       sync.Mutex
	f        *os.File
	writeErr error // first write error encountered (fail-open: doesn't block operations)
}

func NewJSONL(path string) (*JSONLAuditor, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &JSONLAuditor{f: f}, nil
}

func (a *JSONLAuditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}

// Err returns the first write error encountered, if any.
// Auditing is fail-open: errors don't block operations, but callers can check afterward.
func (a *JSONLAuditor) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writeErr
}

func (a *JSONLAuditor) Record(_ context.Context, evt core.AuditEvent) {
	// Make sure Time is always set.
	if evt.Time.IsZero() {
		evt.Time = time.Now()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}

	// Keep Err JSON-safe (string).
	type wire struct {
		Time   time.Time      `json:"time"`
		Level  string         `json:"level"`
		Action string         `json:"action"`
		Path   string         `json:"path"`
		Fields map[string]any `json:"fields,omitempty"`
		Err    string         `json:"err,omitempty"`
	}

	w := wire{
		Time:   evt.Time,
		Level:  evt.Level,
		Action: evt.Action,
		Path:   evt.Path,
		Fields: evt.Fields,
	}
	if evt.Err != nil {
		w.Err = evt.Err.Error()
	}

	b, err := json.Marshal(w)
	if err != nil {
		if a.writeErr == nil {
			a.writeErr = err
		}
		return
	}
	if _, err := a.f.Write(append(b, '\n')); err != nil && a.writeErr == nil {
		a.writeErr = err
	}
}
