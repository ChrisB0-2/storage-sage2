package auditor

import (
	"context"
	"errors"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// Multi writes audit events to multiple auditors.
type Multi struct {
	auditors []core.Auditor
}

// NewMulti creates an auditor that writes to multiple backends.
func NewMulti(auditors ...core.Auditor) *Multi {
	return &Multi{auditors: auditors}
}

// Record writes the event to all configured auditors.
// Returns the first error encountered (if any).
func (m *Multi) Record(ctx context.Context, evt core.AuditEvent) error {
	var errs []error
	for _, a := range m.auditors {
		if err := a.Record(ctx, evt); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Ensure Multi implements core.Auditor
var _ core.Auditor = (*Multi)(nil)
