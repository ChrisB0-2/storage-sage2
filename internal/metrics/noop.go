package metrics

import (
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// Noop is a no-op implementation of core.Metrics.
// Use this when metrics collection is disabled.
type Noop struct{}

// NewNoop creates a new no-op metrics collector.
func NewNoop() *Noop {
	return &Noop{}
}

// Scanning metrics
func (Noop) IncFilesScanned(string)                    {}
func (Noop) IncDirsScanned(string)                     {}
func (Noop) ObserveScanDuration(string, time.Duration) {}

// Planning metrics
func (Noop) IncPolicyDecision(string, bool) {}
func (Noop) IncSafetyVerdict(string, bool)  {}
func (Noop) SetBytesEligible(int64)         {}
func (Noop) SetFilesEligible(int)           {}

// Execution metrics
func (Noop) IncFilesDeleted(string) {}
func (Noop) IncDirsDeleted(string)  {}
func (Noop) AddBytesFreed(int64)    {}
func (Noop) IncDeleteErrors(string) {}

// System metrics
func (Noop) SetDiskUsage(float64) {}
func (Noop) SetCPUUsage(float64)  {}

// Daemon metrics
func (Noop) SetLastRunTimestamp(time.Time) {}

// Ensure Noop implements core.Metrics
var _ core.Metrics = (*Noop)(nil)
