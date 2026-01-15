package policy

import (
	"context"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// SizePolicy allows candidates larger than MinBytes.
type SizePolicy struct {
	MinBytes int64
}

// NewSizePolicy creates a policy that allows files >= minMB megabytes.
func NewSizePolicy(minMB int) *SizePolicy {
	return &SizePolicy{MinBytes: int64(minMB) * 1024 * 1024}
}

func (p *SizePolicy) Evaluate(_ context.Context, c core.Candidate, _ core.EnvSnapshot) core.Decision {
	if c.SizeBytes >= p.MinBytes {
		// Score based on size in MB (capped at 1024)
		sizeMB := int(c.SizeBytes / (1024 * 1024))
		if sizeMB > 1024 {
			sizeMB = 1024
		}
		return core.Decision{Allow: true, Reason: "size_ok", Score: sizeMB}
	}
	return core.Decision{Allow: false, Reason: "too_small", Score: 0}
}
