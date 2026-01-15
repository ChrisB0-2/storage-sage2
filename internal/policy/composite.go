package policy

import (
	"context"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// CompositeMode determines how multiple policies are combined.
type CompositeMode string

const (
	// ModeAnd requires all policies to allow (logical AND).
	ModeAnd CompositeMode = "and"
	// ModeOr requires at least one policy to allow (logical OR).
	ModeOr CompositeMode = "or"
)

// CompositePolicy combines multiple policies with AND or OR logic.
type CompositePolicy struct {
	Policies []core.Policy
	Mode     CompositeMode
}

// NewCompositePolicy creates a policy that combines multiple policies.
// Mode "and" requires all to allow; mode "or" requires at least one to allow.
func NewCompositePolicy(mode CompositeMode, policies ...core.Policy) *CompositePolicy {
	return &CompositePolicy{
		Policies: policies,
		Mode:     mode,
	}
}

func (p *CompositePolicy) Evaluate(ctx context.Context, c core.Candidate, env core.EnvSnapshot) core.Decision {
	if len(p.Policies) == 0 {
		return core.Decision{Allow: false, Reason: "no_policies", Score: 0}
	}

	switch p.Mode {
	case ModeAnd:
		return p.evaluateAnd(ctx, c, env)
	case ModeOr:
		return p.evaluateOr(ctx, c, env)
	default:
		return core.Decision{Allow: false, Reason: "invalid_mode", Score: 0}
	}
}

// evaluateAnd returns allow only if ALL policies allow.
// Returns the minimum score and first deny reason encountered.
func (p *CompositePolicy) evaluateAnd(ctx context.Context, c core.Candidate, env core.EnvSnapshot) core.Decision {
	minScore := int(^uint(0) >> 1) // Max int
	reasons := make([]string, 0, len(p.Policies))

	for _, pol := range p.Policies {
		dec := pol.Evaluate(ctx, c, env)
		if !dec.Allow {
			return core.Decision{
				Allow:  false,
				Reason: "and_deny:" + dec.Reason,
				Score:  0,
			}
		}
		if dec.Score < minScore {
			minScore = dec.Score
		}
		reasons = append(reasons, dec.Reason)
	}

	return core.Decision{
		Allow:  true,
		Reason: "and_allow",
		Score:  minScore,
	}
}

// evaluateOr returns allow if ANY policy allows.
// Returns the maximum score among allowing policies.
func (p *CompositePolicy) evaluateOr(ctx context.Context, c core.Candidate, env core.EnvSnapshot) core.Decision {
	maxScore := 0
	var allowReason string
	denyReasons := make([]string, 0, len(p.Policies))

	for _, pol := range p.Policies {
		dec := pol.Evaluate(ctx, c, env)
		if dec.Allow {
			if dec.Score > maxScore {
				maxScore = dec.Score
				allowReason = dec.Reason
			}
		} else {
			denyReasons = append(denyReasons, dec.Reason)
		}
	}

	if allowReason != "" {
		return core.Decision{
			Allow:  true,
			Reason: "or_allow:" + allowReason,
			Score:  maxScore,
		}
	}

	// All denied - return first deny reason
	reason := "or_deny"
	if len(denyReasons) > 0 {
		reason = "or_deny:" + denyReasons[0]
	}
	return core.Decision{
		Allow:  false,
		Reason: reason,
		Score:  0,
	}
}
