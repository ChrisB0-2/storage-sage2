package safety

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestProtectedPathDenied(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: []string{"/data/protected"},
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root:    cfg.AllowedRoots[0],
		Path:    "/data/protected/secret.log",
		Type:    core.TargetFile,
		FoundAt: time.Now(),
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied, got allowed (reason=%s)", v.Reason)
	}
	if v.Reason != "protected_path" {
		t.Fatalf("expected protected_path, got %s", v.Reason)
	}
}

func TestOutsideAllowedRootsDenied(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: []string{"/data/protected"},
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root:    cfg.AllowedRoots[0],
		Path:    "/etc/passwd",
		Type:    core.TargetFile,
		FoundAt: time.Now(),
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied, got allowed (reason=%s)", v.Reason)
	}
	if v.Reason != "outside_allowed_roots" {
		t.Fatalf("expected outside_allowed_roots, got %s", v.Reason)
	}
}

func TestSymlinkEscapeDenied(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root:       cfg.AllowedRoots[0],
		Path:       "/data/work/link.log",
		Type:       core.TargetFile,
		IsSymlink:  true,
		LinkTarget: "/etc/shadow",
		FoundAt:    time.Now(),
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied, got allowed (reason=%s)", v.Reason)
	}
	if v.Reason != "symlink_escape" {
		t.Fatalf("expected symlink_escape, got %s", v.Reason)
	}
}

func TestSymlinkWithinAllowedRootsAllowed(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root:       cfg.AllowedRoots[0],
		Path:       "/data/work/link.log",
		Type:       core.TargetFile,
		IsSymlink:  true,
		LinkTarget: "/data/work/real.log",
		FoundAt:    time.Now(),
	}

	v := e.Validate(context.Background(), c, cfg)
	if !v.Allowed {
		t.Fatalf("expected allowed, got denied (reason=%s)", v.Reason)
	}
}

func TestDirDeleteBlockedByDefault(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root:    cfg.AllowedRoots[0],
		Path:    "/data/work/old-dir",
		Type:    core.TargetDir,
		FoundAt: time.Now(),
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied, got allowed (reason=%s)", v.Reason)
	}
	if v.Reason != "dir_delete_disabled" {
		t.Fatalf("expected dir_delete_disabled, got %s", v.Reason)
	}
}
func TestProtectedRootSlashDoesNotMatchAll(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: []string{"/"},
		AllowDirDelete: false,
	}

	c := core.Candidate{
		Root: cfg.AllowedRoots[0],
		Path: "/data/file.log",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	// "/" should NOT cause everything to be protected.
	if !v.Allowed {
		t.Fatalf("expected allowed, got denied (reason=%s)", v.Reason)
	}
}
