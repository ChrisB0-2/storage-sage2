package policy

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestAgePolicy(t *testing.T) {
	p := NewAgePolicy(30)

	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	env := core.EnvSnapshot{Now: now}

	old := core.Candidate{Root: "/tmp", ModTime: now.Add(-45 * 24 * time.Hour)}
	newer := core.Candidate{Root: "/tmp", ModTime: now.Add(-10 * 24 * time.Hour)}

	d1 := p.Evaluate(context.Background(), old, env)
	if !d1.Allow || d1.Reason != "age_ok" {
		t.Fatalf("expected age_ok allow, got allow=%v reason=%s", d1.Allow, d1.Reason)
	}

	d2 := p.Evaluate(context.Background(), newer, env)
	if d2.Allow || d2.Reason != "too_new" {
		t.Fatalf("expected too_new deny, got allow=%v reason=%s", d2.Allow, d2.Reason)
	}
}
