package planner

import (
	"context"
	"sort"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

type Simple struct {
	log logger.Logger
}

// NewSimple creates a planner with no-op logging.
func NewSimple() *Simple {
	return &Simple{log: logger.NewNop()}
}

// NewSimpleWithLogger creates a planner with the given logger.
func NewSimpleWithLogger(log logger.Logger) *Simple {
	if log == nil {
		log = logger.NewNop()
	}
	return &Simple{log: log}
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

		items = append(items, core.PlanItem{
			Candidate: cand,
			Decision:  dec,
			Safety:    verdict,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Candidate.Path < items[j].Candidate.Path
	})

	p.log.Info("plan built", logger.F("items", len(items)))
	return items, nil
}
