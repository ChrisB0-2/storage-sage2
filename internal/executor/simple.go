package executor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/metrics"
)

// Action result reason constants.
const (
	reasonWouldDelete  = "would_delete"
	reasonAlreadyGone  = "already_gone"
	reasonDeleted      = "deleted"
	reasonDeleteFailed = "delete_failed"
)

// Simple is a safe-by-default deleter.
// It enforces an execute-time safety re-check (TOCTOU hard gate) immediately before mutation.
// If an Auditor is provided, it records an AuditEvent for each executed item outcome.
type Simple struct {
	safe    core.Safety
	aud     core.Auditor
	cfg     core.SafetyConfig
	now     func() time.Time
	log     logger.Logger
	metrics core.Metrics
}

// NewSimple creates an executor with no-op logging and metrics.
func NewSimple(safe core.Safety, cfg core.SafetyConfig) *Simple {
	return &Simple{
		safe:    safe,
		cfg:     cfg,
		now:     time.Now,
		log:     logger.NewNop(),
		metrics: metrics.NewNoop(),
	}
}

// NewSimpleWithLogger creates an executor with the given logger.
func NewSimpleWithLogger(safe core.Safety, cfg core.SafetyConfig, log logger.Logger) *Simple {
	if log == nil {
		log = logger.NewNop()
	}
	return &Simple{
		safe:    safe,
		cfg:     cfg,
		now:     time.Now,
		log:     log,
		metrics: metrics.NewNoop(),
	}
}

// NewSimpleWithMetrics creates an executor with logger and metrics.
func NewSimpleWithMetrics(safe core.Safety, cfg core.SafetyConfig, log logger.Logger, m core.Metrics) *Simple {
	if log == nil {
		log = logger.NewNop()
	}
	if m == nil {
		m = metrics.NewNoop()
	}
	return &Simple{
		safe:    safe,
		cfg:     cfg,
		now:     time.Now,
		log:     log,
		metrics: m,
	}
}

// WithAuditor attaches an auditor (optional). Safe to pass nil.
func (e *Simple) WithAuditor(aud core.Auditor) *Simple {
	e.aud = aud
	return e
}

// Execute performs the action for one PlanItem.
//
// Hard gates in order:
//  1. policy allow (item.Decision.Allow)
//  2. scan-time safety allow (item.Safety.Allowed)
//  3. execute-time safety re-check (safe.Validate) to prevent TOCTOU
//  4. dry-run: report would-delete
//  5. execute: delete (file/dir), fail-closed
func (e *Simple) Execute(ctx context.Context, item core.PlanItem, mode core.Mode) (res core.ActionResult) {
	start := e.now()

	e.log.Debug("executing action", logger.F("path", item.Candidate.Path), logger.F("mode", string(mode)))

	res = core.ActionResult{
		Path:      item.Candidate.Path,
		Type:      item.Candidate.Type,
		Mode:      mode,
		Score:     item.Decision.Score,
		StartedAt: start,
	}

	// Always finalize + audit on return.
	// Uses named return value so defer modifications are visible to caller.
	defer func() {
		if res.FinishedAt.IsZero() {
			res.FinishedAt = e.now()
		}
		e.record(ctx, item, res)
	}()

	// Cancellation check early.
	select {
	case <-ctx.Done():
		res.Reason = "ctx_canceled"
		res.Err = ctx.Err()
		return res
	default:
	}

	// Gate 1: Policy
	if !item.Decision.Allow {
		res.Reason = "policy_deny:" + item.Decision.Reason
		return res
	}

	// Gate 2: Scan-time safety verdict
	if !item.Safety.Allowed {
		res.Reason = "safety_deny_scan:" + item.Safety.Reason
		return res
	}

	// Gate 3: Execute-time safety re-check (TOCTOU hard gate)
	// MUST happen immediately before any mutation.
	v := e.safe.Validate(ctx, item.Candidate, e.cfg)
	if !v.Allowed {
		res.Reason = "safety_deny_execute:" + v.Reason
		return res
	}

	// Gate 4: Dry run
	if mode == core.ModeDryRun {
		res.Reason = reasonWouldDelete
		if item.Candidate.Type == core.TargetFile {
			res.BytesFreed = item.Candidate.SizeBytes
		}
		return res
	}

	// Execute mode required to mutate.
	if mode != core.ModeExecute {
		res.Reason = "invalid_mode"
		res.Err = errors.New("invalid mode")
		return res
	}

	// Gate 5: Perform deletion (fail-closed)
	switch item.Candidate.Type {
	case core.TargetFile:
		if err := os.Remove(item.Candidate.Path); err != nil {
			// Idempotent behavior: already removed is not fatal.
			if errors.Is(err, os.ErrNotExist) {
				res.Reason = reasonAlreadyGone
				return res
			}
			e.log.Warn("delete failed", logger.F("path", item.Candidate.Path), logger.F("error", err.Error()))
			e.metrics.IncDeleteErrors(reasonDeleteFailed)
			res.Reason = reasonDeleteFailed
			res.Err = err
			return res
		}

		e.log.Info("deleted", logger.F("path", item.Candidate.Path), logger.F("bytes_freed", item.Candidate.SizeBytes))
		e.metrics.IncFilesDeleted(item.Candidate.Root)
		e.metrics.AddBytesFreed(item.Candidate.SizeBytes)
		res.Deleted = true
		res.BytesFreed = item.Candidate.SizeBytes
		res.Reason = reasonDeleted
		return res

	case core.TargetDir:
		// Even in execute, dir deletion must be explicitly enabled.
		if !e.cfg.AllowDirDelete {
			res.Reason = "dir_delete_disabled"
			res.Err = core.ErrNotAllowed
			return res
		}

		// Calculate directory size before deletion.
		var dirSize int64
		_ = filepath.WalkDir(item.Candidate.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil {
				dirSize += info.Size()
			}
			return nil
		})

		if err := os.RemoveAll(item.Candidate.Path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				res.Reason = reasonAlreadyGone
				return res
			}
			e.log.Warn("delete failed", logger.F("path", item.Candidate.Path), logger.F("error", err.Error()))
			e.metrics.IncDeleteErrors(reasonDeleteFailed)
			res.Reason = reasonDeleteFailed
			res.Err = err
			return res
		}

		e.log.Info("deleted", logger.F("path", item.Candidate.Path), logger.F("bytes_freed", dirSize), logger.F("type", "dir"))
		e.metrics.IncDirsDeleted(item.Candidate.Root)
		e.metrics.AddBytesFreed(dirSize)
		res.Deleted = true
		res.BytesFreed = dirSize
		res.Reason = reasonDeleted
		return res

	default:
		res.Reason = "unknown_target_type"
		res.Err = errors.New("unknown target type")
		return res
	}
}

// record writes one audit event if an auditor is configured.
// It intentionally never panics and never blocks deletes if auditing fails.
func (e *Simple) record(ctx context.Context, item core.PlanItem, res core.ActionResult) {
	if e.aud == nil {
		return
	}

	evt := core.AuditEvent{
		Time:  res.FinishedAt,
		Level: "info",
		Action: func() string {
			switch res.Reason {
			case reasonDeleted:
				return "delete"
			case reasonWouldDelete:
				return reasonWouldDelete
			default:
				return "skip"
			}
		}(),
		Path: res.Path,
		Fields: map[string]any{
			"mode":           string(res.Mode),
			"type":           string(res.Type),
			"deleted":        res.Deleted,
			"bytes_freed":    res.BytesFreed,
			"reason":         res.Reason,
			"policy_reason":  item.Decision.Reason,
			"safety_reason":  item.Safety.Reason,
			"priority_score": item.Decision.Score, // <-- the priority you asked for
			"root":           item.Candidate.Root,
		},
		Err: res.Err,
	}

	// Best-effort: auditing must never break deletion.
	defer func() { _ = recover() }()
	e.aud.Record(ctx, evt)
}
