package safety

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

type Engine struct {
	log logger.Logger
}

// New creates a safety engine with no-op logging.
func New() *Engine {
	return &Engine{log: logger.NewNop()}
}

// NewWithLogger creates a safety engine with the given logger.
func NewWithLogger(log logger.Logger) *Engine {
	if log == nil {
		log = logger.NewNop()
	}
	return &Engine{log: log}
}

//nolint:gocyclo // Safety validation requires comprehensive checks; refactoring would reduce clarity
func (e *Engine) Validate(_ context.Context, cand core.Candidate, cfg core.SafetyConfig) core.SafetyVerdict {
	// Normalize candidate path.
	candPath := filepath.Clean(cand.Path)

	roots := cfg.AllowedRoots
	if len(roots) == 0 && strings.TrimSpace(cand.Root) != "" {
		roots = []string{cand.Root}
	}
	// Fail-closed: if roots are enforced, candidate must carry the discovering root.
	if len(cfg.AllowedRoots) > 0 && strings.TrimSpace(cand.Root) == "" {
		return e.denyWithLog(candPath, "missing_candidate_root")
	}

	// 0a) Ancestor symlink containment (fail-closed when roots are configured).
	if _, err := os.Lstat(candPath); err == nil {
		// Prefer scanner-provided cand.Root; otherwise derive from AllowedRoots.
		rootForContainment := strings.TrimSpace(cand.Root)
		if rootForContainment != "" {
			v := AncestorSymlinkContainment(rootForContainment, cand.Path, AncestorSymlinkOptions{
				AllowRootSymlink: true,
			})
			if !v.Allowed {
				// Normalize internal containment reasons into public engine reasons.
				if v.Reason == ReasonOutsideRoot {
					return e.denyWithLog(candPath, "outside_allowed_roots")
				}

				// Upgrade symlink_self / symlink_ancestor to symlink_escape when LinkTarget escapes allowed roots.
				if (v.Reason == ReasonSymlinkSelf || v.Reason == ReasonSymlinkAncestor) &&
					cand.IsSymlink && cand.LinkTarget != "" && len(cfg.AllowedRoots) > 0 {
					linkTarget := cand.LinkTarget
					if !filepath.IsAbs(linkTarget) {
						linkTarget = filepath.Join(filepath.Dir(candPath), linkTarget)
					}
					resolved := filepath.Clean(linkTarget)

					allowedResolved := false
					for _, r := range roots {
						root := filepath.Clean(r)
						if isPathOrChild(resolved, root) {
							allowedResolved = true
							break
						}
					}
					if !allowedResolved {
						return e.denyWithLog(candPath, "symlink_escape")
					}
				}
				e.log.Debug("safety denied", logger.F("path", candPath), logger.F("reason", v.Reason))
				return v
			}
		}

	}

	// 0b) Mount boundary enforcement
	if cfg.EnforceMountBoundary && cand.RootDeviceID != 0 && cand.DeviceID != 0 {
		if cand.DeviceID != cand.RootDeviceID {
			return e.denyWithLog(candPath, "mount_boundary")
		}
	}

	// 0) Type gate: dir deletion must be explicitly allowed.
	if cand.Type == core.TargetDir && !cfg.AllowDirDelete {
		return e.denyWithLog(candPath, "dir_delete_disabled")
	}

	// 1) Protected paths: hard deny if cand is or is under any protected path.
	for _, p := range cfg.ProtectedPaths {
		pp := filepath.Clean(p)
		if isPathOrChild(candPath, pp) {
			return e.denyWithLog(candPath, "protected_path")
		}
	}

	// 2) Allowed roots: candidate path must be under at least one allowed root.
	if len(cfg.AllowedRoots) > 0 {
		allowed := false
		for _, r := range roots {
			root := filepath.Clean(r)
			if isPathOrChild(candPath, root) {
				allowed = true
				break
			}
		}
		if !allowed {
			return e.denyWithLog(candPath, "outside_allowed_roots")
		}
	}

	// 3) Symlink escape check: if candidate is a symlink and we know link target,
	// ensure resolved path still sits under allowed roots.
	//
	// IMPORTANT: This is a "deny on known escape" check.
	// We do not attempt filesystem reads here; scanner can provide LinkTarget.
	roots = cfg.AllowedRoots
	if len(roots) == 0 && cand.Root != "" {
		roots = []string{cand.Root}
	}
	if cand.IsSymlink && cand.LinkTarget != "" && len(roots) > 0 {
		// LinkTarget may be relative; resolve relative to the symlink's directory.
		linkTarget := cand.LinkTarget
		if !filepath.IsAbs(linkTarget) {
			linkTarget = filepath.Join(filepath.Dir(candPath), linkTarget)
		}
		resolved := filepath.Clean(linkTarget)

		allowed := false
		for _, r := range roots {
			root := filepath.Clean(r)
			if isPathOrChild(resolved, root) {
				allowed = true
				break
			}
		}
		if !allowed {
			return e.denyWithLog(candPath, "symlink_escape")
		}
	}

	return allow("ok")
}

func allow(reason string) core.SafetyVerdict {
	return core.SafetyVerdict{Allowed: true, Reason: reason}
}

func deny(reason string) core.SafetyVerdict {
	return core.SafetyVerdict{Allowed: false, Reason: reason}
}

// denyWithLog creates a deny verdict and logs it.
func (e *Engine) denyWithLog(path, reason string) core.SafetyVerdict {
	e.log.Debug("safety denied", logger.F("path", path), logger.F("reason", reason))
	return deny(reason)
}

// isPathOrChild returns true if path == base OR path is a child of base.
// This avoids prefix bugs like "/data/a" matching "/data/abc".
func isPathOrChild(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)

	// Special case: "/" should only match "/" exactly.
	if base == string(filepath.Separator) {
		return path == base
	}

	if path == base {
		return true
	}

	baseWithSep := base
	if !strings.HasSuffix(baseWithSep, string(filepath.Separator)) {
		baseWithSep += string(filepath.Separator)
	}
	return strings.HasPrefix(path, baseWithSep)
}
