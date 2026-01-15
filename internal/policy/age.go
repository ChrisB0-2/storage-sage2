package policy

import (
	"context"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

type AgePolicy struct {
	MinAge time.Duration
}

func NewAgePolicy(minAgeDays int) *AgePolicy {
	return &AgePolicy{MinAge: time.Duration(minAgeDays) * 24 * time.Hour}
}

func (p *AgePolicy) Evaluate(_ context.Context, c core.Candidate, env core.EnvSnapshot) core.Decision {
	age := env.Now.Sub(c.ModTime)
	if age < 0 {
		age = 0
	}

	ageDays := int(age / (24 * time.Hour))
	if ageDays < 0 {
		ageDays = 0
	}
	if ageDays > 3650 {
		ageDays = 3650
	}

	sizeMiB := int(c.SizeBytes / (1024 * 1024))
	if sizeMiB < 0 {
		sizeMiB = 0
	}
	if sizeMiB > 1024 {
		sizeMiB = 1024
	}

	// Priority score: age dominates; size is a small tie-breaker.
	score := ageDays*10 + sizeMiB

	if age >= p.MinAge {
		return core.Decision{Allow: true, Reason: "age_ok", Score: score}
	}
	return core.Decision{Allow: false, Reason: "too_new", Score: 0}
}
