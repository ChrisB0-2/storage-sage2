package policy

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestCompositeAndAllAllow(t *testing.T) {
	// Both policies allow: old enough AND large enough
	age := NewAgePolicy(30)
	size := NewSizePolicy(1)

	p := NewCompositePolicy(ModeAnd, age, size)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:      "/data/old-large.bin",
		ModTime:   time.Now().Add(-60 * 24 * time.Hour), // 60 days old
		SizeBytes: 5 * 1024 * 1024,                      // 5 MB
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected AND with all allowing to allow, got deny: %s", dec.Reason)
	}
	if dec.Reason != "and_allow" {
		t.Errorf("expected reason 'and_allow', got '%s'", dec.Reason)
	}
}

func TestCompositeAndOneDenies(t *testing.T) {
	// Age allows, size denies
	age := NewAgePolicy(30)
	size := NewSizePolicy(100) // 100 MB minimum - won't be met

	p := NewCompositePolicy(ModeAnd, age, size)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:      "/data/old-small.bin",
		ModTime:   time.Now().Add(-60 * 24 * time.Hour), // 60 days old
		SizeBytes: 5 * 1024 * 1024,                      // Only 5 MB
	}

	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Error("expected AND with one denying to deny")
	}
	if dec.Reason != "and_deny:too_small" {
		t.Errorf("expected reason 'and_deny:too_small', got '%s'", dec.Reason)
	}
}

func TestCompositeOrOneAllows(t *testing.T) {
	// Age denies (too new), extension allows
	age := NewAgePolicy(30)
	ext := NewExtensionPolicy([]string{".tmp"})

	p := NewCompositePolicy(ModeOr, age, ext)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:    "/data/new.tmp",
		ModTime: time.Now(), // Too new for age policy
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected OR with one allowing to allow, got deny: %s", dec.Reason)
	}
	if dec.Reason != "or_allow:extension_match" {
		t.Errorf("expected reason 'or_allow:extension_match', got '%s'", dec.Reason)
	}
}

func TestCompositeOrAllDeny(t *testing.T) {
	// Both deny
	age := NewAgePolicy(30)
	ext := NewExtensionPolicy([]string{".tmp"})

	p := NewCompositePolicy(ModeOr, age, ext)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:    "/data/new.txt", // Not .tmp
		ModTime: time.Now(),      // Too new
	}

	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Error("expected OR with all denying to deny")
	}
	if dec.Reason != "or_deny:too_new" {
		t.Errorf("expected reason 'or_deny:too_new', got '%s'", dec.Reason)
	}
}

func TestCompositeNoPolicies(t *testing.T) {
	p := NewCompositePolicy(ModeAnd)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{Path: "/data/file.txt"}

	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Error("expected empty policy to deny")
	}
	if dec.Reason != "no_policies" {
		t.Errorf("expected reason 'no_policies', got '%s'", dec.Reason)
	}
}

func TestCompositeAndUsesMinScore(t *testing.T) {
	// Two policies with different scores
	age := NewAgePolicy(1)   // Will give score based on age
	size := NewSizePolicy(0) // 0 MB minimum - will give score based on size

	p := NewCompositePolicy(ModeAnd, age, size)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:      "/data/file.bin",
		ModTime:   time.Now().Add(-10 * 24 * time.Hour), // 10 days old -> score ~100
		SizeBytes: 2 * 1024 * 1024,                      // 2 MB -> score 2
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected allow, got deny: %s", dec.Reason)
	}
	// Should use minimum score (size score = 2)
	if dec.Score != 2 {
		t.Errorf("expected score 2 (min of policies), got %d", dec.Score)
	}
}

func TestCompositeOrUsesMaxScore(t *testing.T) {
	// Two policies both allowing with different scores
	age := NewAgePolicy(1)
	ext := NewExtensionPolicy([]string{".tmp"})

	p := NewCompositePolicy(ModeOr, age, ext)

	env := core.EnvSnapshot{Now: time.Now()}
	c := core.Candidate{
		Path:    "/data/old.tmp",
		ModTime: time.Now().Add(-100 * 24 * time.Hour), // 100 days old -> score ~1000
	}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Errorf("expected allow, got deny: %s", dec.Reason)
	}
	// Should use maximum score (age score > extension score of 100)
	if dec.Score < 100 {
		t.Errorf("expected score >= 100 (max of policies), got %d", dec.Score)
	}
}
