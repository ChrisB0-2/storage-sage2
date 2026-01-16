package planner

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// BenchmarkBuildPlan_SmallSet benchmarks plan building with ~100 candidates
func BenchmarkBuildPlan_SmallSet(b *testing.B) {
	p := NewSimple()
	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands := make(chan core.Candidate, 100)
		go func() {
			defer close(cands)
			for j := 0; j < 100; j++ {
				cands <- core.Candidate{
					Path:      "/data/file_" + string(rune('0'+j/10)) + string(rune('0'+j%10)) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// BenchmarkBuildPlan_MediumSet benchmarks plan building with ~1000 candidates
func BenchmarkBuildPlan_MediumSet(b *testing.B) {
	p := NewSimple()
	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands := make(chan core.Candidate, 1000)
		go func() {
			defer close(cands)
			for j := 0; j < 1000; j++ {
				cands <- core.Candidate{
					Path:      "/data/file_" + formatNumber(j) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// BenchmarkBuildPlan_LargeSet benchmarks plan building with ~10000 candidates
func BenchmarkBuildPlan_LargeSet(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping large set benchmark in short mode")
	}

	p := NewSimple()
	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands := make(chan core.Candidate, 10000)
		go func() {
			defer close(cands)
			for j := 0; j < 10000; j++ {
				cands <- core.Candidate{
					Path:      "/data/file_" + formatNumber(j) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// BenchmarkBuildPlan_MixedDecisions benchmarks with mixed allow/deny decisions
func BenchmarkBuildPlan_MixedDecisions(b *testing.B) {
	p := NewSimple()
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	// Alternating policy that allows every other candidate
	alternatingPolicy := &alternatingMockPolicy{}
	safe := &mockSafety{allowed: true, reason: "ok"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alternatingPolicy.count = 0 // Reset counter
		cands := make(chan core.Candidate, 1000)
		go func() {
			defer close(cands)
			for j := 0; j < 1000; j++ {
				cands <- core.Candidate{
					Path:      "/data/file_" + formatNumber(j) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, alternatingPolicy, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// BenchmarkBuildPlan_WithMetrics benchmarks with metrics collection enabled
func BenchmarkBuildPlan_WithMetrics(b *testing.B) {
	p := NewSimpleWithMetrics(nil, &noopMetrics{})
	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands := make(chan core.Candidate, 1000)
		go func() {
			defer close(cands)
			for j := 0; j < 1000; j++ {
				cands <- core.Candidate{
					Path:      "/data/file_" + formatNumber(j) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// BenchmarkBuildPlan_Sorting benchmarks the sorting overhead
func BenchmarkBuildPlan_Sorting(b *testing.B) {
	p := NewSimple()
	pol := &mockPolicy{allow: true, reason: "age_ok", score: 100}
	safe := &mockSafety{allowed: true, reason: "ok"}
	env := core.EnvSnapshot{Now: time.Now()}
	cfg := core.SafetyConfig{AllowedRoots: []string{"/data"}}

	// Create candidates in reverse order to maximize sorting work
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cands := make(chan core.Candidate, 1000)
		go func() {
			defer close(cands)
			for j := 999; j >= 0; j-- { // Reverse order
				cands <- core.Candidate{
					Path:      "/data/file_" + formatNumber(j) + ".tmp",
					Type:      core.TargetFile,
					SizeBytes: 1024,
					ModTime:   time.Now().Add(-48 * time.Hour),
				}
			}
		}()

		_, err := p.BuildPlan(context.Background(), cands, pol, safe, env, cfg)
		if err != nil {
			b.Fatalf("BuildPlan error: %v", err)
		}
	}
}

// alternatingMockPolicy alternates between allow and deny
type alternatingMockPolicy struct {
	count int
}

func (a *alternatingMockPolicy) Evaluate(_ context.Context, _ core.Candidate, _ core.EnvSnapshot) core.Decision {
	a.count++
	if a.count%2 == 0 {
		return core.Decision{Allow: true, Reason: "age_ok", Score: 100}
	}
	return core.Decision{Allow: false, Reason: "too_new", Score: 0}
}

// noopMetrics implements core.Metrics for benchmarking
type noopMetrics struct{}

func (n *noopMetrics) IncFilesScanned(root string)                      {}
func (n *noopMetrics) IncDirsScanned(root string)                       {}
func (n *noopMetrics) ObserveScanDuration(root string, d time.Duration) {}
func (n *noopMetrics) IncPolicyDecision(reason string, allowed bool)    {}
func (n *noopMetrics) IncSafetyVerdict(reason string, allowed bool)     {}
func (n *noopMetrics) SetBytesEligible(bytes int64)                     {}
func (n *noopMetrics) SetFilesEligible(count int)                       {}
func (n *noopMetrics) IncFilesDeleted(root string)                      {}
func (n *noopMetrics) IncDirsDeleted(root string)                       {}
func (n *noopMetrics) AddBytesFreed(bytes int64)                        {}
func (n *noopMetrics) IncDeleteErrors(reason string)                    {}
func (n *noopMetrics) SetDiskUsage(percent float64)                     {}
func (n *noopMetrics) SetCPUUsage(percent float64)                      {}

// formatNumber formats a number as a zero-padded string
func formatNumber(n int) string {
	return string(rune('0'+n/1000)) + string(rune('0'+(n/100)%10)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}
