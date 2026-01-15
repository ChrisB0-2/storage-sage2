package policy

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestSizePolicyAllowsLargeFiles(t *testing.T) {
	p := NewSizePolicy(10) // 10 MB minimum

	env := core.EnvSnapshot{Now: time.Now()}

	// 15 MB file should be allowed
	c := core.Candidate{
		Path:      "/data/large.bin",
		SizeBytes: 15 * 1024 * 1024,
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected large file to be allowed, got deny: %s", dec.Reason)
	}
	if dec.Reason != "size_ok" {
		t.Errorf("expected reason 'size_ok', got '%s'", dec.Reason)
	}
	if dec.Score != 15 {
		t.Errorf("expected score 15 (size in MB), got %d", dec.Score)
	}
}

func TestSizePolicyDeniesSmallFiles(t *testing.T) {
	p := NewSizePolicy(10) // 10 MB minimum

	env := core.EnvSnapshot{Now: time.Now()}

	// 5 MB file should be denied
	c := core.Candidate{
		Path:      "/data/small.bin",
		SizeBytes: 5 * 1024 * 1024,
	}

	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Error("expected small file to be denied")
	}
	if dec.Reason != "too_small" {
		t.Errorf("expected reason 'too_small', got '%s'", dec.Reason)
	}
}

func TestSizePolicyExactThreshold(t *testing.T) {
	p := NewSizePolicy(10) // 10 MB minimum

	env := core.EnvSnapshot{Now: time.Now()}

	// Exactly 10 MB should be allowed
	c := core.Candidate{
		Path:      "/data/exact.bin",
		SizeBytes: 10 * 1024 * 1024,
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected exact threshold file to be allowed, got deny: %s", dec.Reason)
	}
}
