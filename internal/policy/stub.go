package policy

import (
	"context"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

type DenyAll struct{}

func NewDenyAll() *DenyAll { return &DenyAll{} }

func (p *DenyAll) Evaluate(_ context.Context, _ core.Candidate, _ core.EnvSnapshot) core.Decision {
	return core.Decision{
		Allow:  false,
		Reason: "policy_deny_all",
	}
}
