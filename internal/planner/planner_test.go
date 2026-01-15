package planner

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// mockPolicy implements core.Policy for testing
type mockPolicy struct {
	allow  bool
	reason string
	score  int
}

func (m *mockPolicy) Evaluate(_ context.Context, _ core.Candidate, _ core.EnvSnapshot) core.Decision {
	return core.Decision{Allow: m.allow, Reason: m.reason, Score: m.score}
}

// mockSafety implements core.Safety for testing
type mockSafety struct {
	allowed bool
	reason  string
}

func (m *mockSafety) Validate(_ context.Context, _ core.Candidate, _ core.SafetyConfig) core.SafetyVerdict {
	return core.SafetyVerdict{Allowed: m.allowed, Reason: m.reason}
}

func TestBuildPlanCombinesPolicyAndSafety(t *testing.T) {
	p := NewSimple()

	// Create a channel with one candidate
	cands := make(chan core.Candidate, 1)
	cands <- core.Candidate{
		Path:    "/data/test.txt",
		Type:    core.TargetFile,
		ModTime: time.Now().Add(-48 * time.Hour),
	}
	close(cands)

	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	plan, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(plan))
	}

	item := plan[0]
	if !item.Decision.Allow {
		t.Error("expected policy to allow")
	}
	if item.Decision.Reason != "age_ok" {
		t.Errorf("expected reason 'age_ok', got '%s'", item.Decision.Reason)
	}
	if !item.Safety.Allowed {
		t.Error("expected safety to allow")
	}
}

func TestBuildPlanSortsByPath(t *testing.T) {
	p := NewSimple()

	// Create candidates out of order
	cands := make(chan core.Candidate, 3)
	cands <- core.Candidate{Path: "/data/c.txt", Type: core.TargetFile}
	cands <- core.Candidate{Path: "/data/a.txt", Type: core.TargetFile}
	cands <- core.Candidate{Path: "/data/b.txt", Type: core.TargetFile}
	close(cands)

	pol := &mockPolicy{allow: true, reason: "ok", score: 1}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	plan, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan) != 3 {
		t.Fatalf("expected 3 plan items, got %d", len(plan))
	}

	// Verify sorted by path
	if plan[0].Candidate.Path != "/data/a.txt" {
		t.Errorf("expected first item path '/data/a.txt', got '%s'", plan[0].Candidate.Path)
	}
	if plan[1].Candidate.Path != "/data/b.txt" {
		t.Errorf("expected second item path '/data/b.txt', got '%s'", plan[1].Candidate.Path)
	}
	if plan[2].Candidate.Path != "/data/c.txt" {
		t.Errorf("expected third item path '/data/c.txt', got '%s'", plan[2].Candidate.Path)
	}
}

func TestBuildPlanContextCancellation(t *testing.T) {
	p := NewSimple()

	// Create a channel that won't be closed immediately
	cands := make(chan core.Candidate)

	pol := &mockPolicy{allow: true, reason: "ok", score: 1}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	ctx, cancel := context.WithCancel(context.Background())

	// Send one candidate then cancel
	go func() {
		cands <- core.Candidate{Path: "/data/test.txt", Type: core.TargetFile}
		cancel()
		close(cands)
	}()

	plan, err := p.BuildPlan(ctx, cands, pol, safe, env, cfg)

	// Should get context error or partial results
	if err != nil && err != context.Canceled {
		t.Logf("got error: %v", err)
	}
	t.Logf("got %d items before cancellation", len(plan))
}

func TestBuildPlanPolicyDeny(t *testing.T) {
	p := NewSimple()

	cands := make(chan core.Candidate, 1)
	cands <- core.Candidate{
		Path:    "/data/new.txt",
		Type:    core.TargetFile,
		ModTime: time.Now(), // Very recent
	}
	close(cands)

	pol := &mockPolicy{allow: false, reason: "too_new", score: 0}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	plan, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(plan))
	}

	if plan[0].Decision.Allow {
		t.Error("expected policy to deny")
	}
	if plan[0].Decision.Reason != "too_new" {
		t.Errorf("expected reason 'too_new', got '%s'", plan[0].Decision.Reason)
	}
}

func TestBuildPlanSafetyDeny(t *testing.T) {
	p := NewSimple()

	cands := make(chan core.Candidate, 1)
	cands <- core.Candidate{
		Path: "/etc/passwd",
		Type: core.TargetFile,
	}
	close(cands)

	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: false, reason: "protected_path"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}, ProtectedPaths: []string{"/etc"}}

	plan, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(plan))
	}

	if plan[0].Safety.Allowed {
		t.Error("expected safety to deny")
	}
	if plan[0].Safety.Reason != "protected_path" {
		t.Errorf("expected reason 'protected_path', got '%s'", plan[0].Safety.Reason)
	}
}
