package planner

import (
	"context"
	"sort"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/metrics"
)

type Simple struct {
	log     logger.Logger
	metrics core.Metrics
}

// NewSimple creates a planner with no-op logging and metrics.
func NewSimple() *Simple {
	return &Simple{
		log:     logger.NewNop(),
		metrics: metrics.NewNoop(),
	}
}

// NewSimpleWithLogger creates a planner with the given logger.
func NewSimpleWithLogger(log logger.Logger) *Simple {
	if log == nil {
		log = logger.NewNop()
	}
	return &Simple{
		log:     log,
		metrics: metrics.NewNoop(),
	}
}

// NewSimpleWithMetrics creates a planner with logger and metrics.
func NewSimpleWithMetrics(log logger.Logger, m core.Metrics) *Simple {
	if log == nil {
		log = logger.NewNop()
	}
	if m == nil {
		m = metrics.NewNoop()
	}
	return &Simple{
		log:     log,
		metrics: m,
	}
}

func (p *Simple) BuildPlan(
	ctx context.Context,
	in <-chan core.Candidate,
	pol core.Policy,
	safe core.Safety,
	env core.EnvSnapshot,
	cfg core.SafetyConfig,
) ([]core.PlanItem, error) {
	p.log.Debug("building plan")
	var items []core.PlanItem

	for cand := range in {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		dec := pol.Evaluate(ctx, cand, env)
		verdict := safe.Validate(ctx, cand, cfg)

		// Record metrics
		p.metrics.IncPolicyDecision(dec.Reason, dec.Allow)
		p.metrics.IncSafetyVerdict(verdict.Reason, verdict.Allowed)

		items = append(items, core.PlanItem{
			Candidate: cand,
			Decision:  dec,
			Safety:    verdict,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Candidate.Path < items[j].Candidate.Path
	})

	// Calculate and record eligible files/bytes
	var eligibleFiles int
	var eligibleBytes int64
	for _, item := range items {
		if item.Decision.Allow && item.Safety.Allowed && item.Candidate.Type == core.TargetFile {
			eligibleFiles++
			eligibleBytes += item.Candidate.SizeBytes
		}
	}
	p.metrics.SetFilesEligible(eligibleFiles)
	p.metrics.SetBytesEligible(eligibleBytes)

	p.log.Info("plan built", logger.F("items", len(items)))
	return items, nil
}
