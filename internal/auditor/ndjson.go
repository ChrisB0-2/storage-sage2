package auditor

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

type NDJSON struct {
	mu       sync.Mutex
	f        *os.File
	w        *bufio.Writer
	writeErr error // first write error encountered (fail-open: doesn't block operations)
}

func NewNDJSON(path string) (*NDJSON, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &NDJSON{f: f, w: bufio.NewWriterSize(f, 64*1024)}, nil
}

func (a *NDJSON) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.w.Flush()
	return a.f.Close()
}

// Err returns the first write error encountered, if any.
// Auditing is fail-open: errors don't block operations, but callers can check afterward.
func (a *NDJSON) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writeErr
}

func (a *NDJSON) Record(_ context.Context, evt core.AuditEvent) {
	// Make sure Time is always set.
	if evt.Time.IsZero() {
		evt.Time = time.Now()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	enc := json.NewEncoder(a.w)
	if err := enc.Encode(evt); err != nil && a.writeErr == nil {
		a.writeErr = err
	}
	if err := a.w.Flush(); err != nil && a.writeErr == nil {
		a.writeErr = err
	}
}
