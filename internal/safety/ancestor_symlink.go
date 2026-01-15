package safety

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

const (
	ReasonOK               = "ok"
	ReasonOutsideRoot      = "outside_root"
	ReasonSymlinkAncestor  = "symlink_ancestor"
	ReasonSymlinkSelf      = "symlink_self"
	ReasonStatError        = "stat_error"
	ReasonInvalidArguments = "invalid_args"
)

type AncestorSymlinkOptions struct {
	// If true, we will NOT block when the root itself is a symlink.
	// This is often useful if the user intentionally points root at a symlinked mount path.
	AllowRootSymlink bool
}

// AncestorSymlinkContainment blocks if:
// - candidate resolves to a path outside root (string-safe check using Abs+Rel), OR
// - any component from root -> candidate is a symlink (detected via Lstat without following).
//
// It does NOT follow symlinks (that's the point).
func AncestorSymlinkContainment(root, candidate string, opt AncestorSymlinkOptions) core.SafetyVerdict {
	root = strings.TrimSpace(root)
	candidate = strings.TrimSpace(candidate)

	if root == "" || candidate == "" {
		return core.SafetyVerdict{Allowed: false, Reason: ReasonInvalidArguments}
	}

	rootAbs, err := absClean(root)
	if err != nil {
		return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:root:%v", ReasonStatError, err)}
	}
	candAbs, err := absClean(candidate)
	if err != nil {
		return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:candidate:%v", ReasonStatError, err)}
	}

	// 1) Fast “must be under root” check (string-safe, not symlink-safe).
	rel, err := filepath.Rel(rootAbs, candAbs)
	if err != nil {
		return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:rel:%v", ReasonStatError, err)}
	}
	if rel == "." {
		// candidate == root (rare) — treat as allowed by containment; callers can still block dirs separately.
		return core.SafetyVerdict{Allowed: true, Reason: ReasonOK}
	}
	if relIsOutside(rel) {
		return core.SafetyVerdict{Allowed: false, Reason: ReasonOutsideRoot}
	}

	// 2) Symlink ancestor check using Lstat on every component along root->candidate.
	parts := splitRel(rel)

	cur := rootAbs

	// Optional: block if root itself is a symlink (only when not allowed).
	if !opt.AllowRootSymlink {
		if isLink, linkErr := isSymlink(cur); linkErr != nil {
			return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:root:%v", ReasonStatError, linkErr)}
		} else if isLink {
			return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:%s", ReasonSymlinkAncestor, cur)}
		}
	}

	for i, p := range parts {
		cur = filepath.Join(cur, p)

		isLink, linkErr := isSymlink(cur)
		if linkErr != nil {
			// Strict by design: any inability to verify safety => deny.
			return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:%v", ReasonStatError, linkErr)}
		}
		if isLink {
			// If the final node is symlink, label as symlink_self; otherwise symlink_ancestor.
			if i == len(parts)-1 {
				return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:%s", ReasonSymlinkSelf, cur)}
			}
			return core.SafetyVerdict{Allowed: false, Reason: fmt.Sprintf("%s:%s", ReasonSymlinkAncestor, cur)}
		}
	}

	return core.SafetyVerdict{Allowed: true, Reason: ReasonOK}
}

func absClean(p string) (string, error) {
	// Clean first so Abs doesn’t preserve oddities like "a/../b".
	clean := filepath.Clean(p)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func relIsOutside(rel string) bool {
	rel = filepath.Clean(rel)
	// filepath.Rel uses OS separators; outside root appears as ".." or "..<sep>..."
	if rel == ".." {
		return true
	}
	prefix := ".." + string(os.PathSeparator)
	return strings.HasPrefix(rel, prefix)
}

func splitRel(rel string) []string {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return nil
	}
	sep := string(os.PathSeparator)
	return strings.Split(rel, sep)
}

func isSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		// If the path vanished, we treat it as an error so the caller denies.
		// (Also helps TOCTOU: unexpected changes fail closed.)
		if errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("lstat:not_exist:%s", path)
		}
		return false, fmt.Errorf("lstat:%s:%w", path, err)
	}
	return (info.Mode() & os.ModeSymlink) != 0, nil
}
