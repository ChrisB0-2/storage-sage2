package auditor

import (
	"context"

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
func (m *Multi) Record(ctx context.Context, evt core.AuditEvent) {
	for _, a := range m.auditors {
		a.Record(ctx, evt)
	}
}

// Ensure Multi implements core.Auditor
var _ core.Auditor = (*Multi)(nil)
