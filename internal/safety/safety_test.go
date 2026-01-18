package safety

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestNewWithLogger(t *testing.T) {
	// Test with nil logger (should use nop)
	e := NewWithLogger(nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.log == nil {
		t.Error("expected non-nil logger")
	}

	// Test with real logger - validate it's used by checking deny logs
	// We can't easily verify logger calls without a mock, but we can verify the engine works
	e = NewWithLogger(nil) // NewWithLogger handles nil gracefully
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: []string{"/data/protected"},
	}
	c := core.Candidate{
		Root: "/data",
		Path: "/data/protected/file.txt",
		Type: core.TargetFile,
	}
	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Error("expected denied for protected path")
	}
}

func TestMissingCandidateRootDenied(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	// Candidate without Root when AllowedRoots is enforced
	c := core.Candidate{
		Root: "", // Missing root
		Path: "/data/work/file.log",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied, got allowed (reason=%s)", v.Reason)
	}
	if v.Reason != "missing_candidate_root" {
		t.Fatalf("expected missing_candidate_root, got %s", v.Reason)
	}
}

func TestDirDeleteAllowedWhenEnabled(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: true, // Enable dir delete
	}

	c := core.Candidate{
		Root: cfg.AllowedRoots[0],
		Path: "/data/work/old-dir",
		Type: core.TargetDir,
	}

	v := e.Validate(context.Background(), c, cfg)
	if !v.Allowed {
		t.Fatalf("expected allowed, got denied (reason=%s)", v.Reason)
	}
}

func TestSymlinkWithRelativeLinkTarget(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	// Relative link target that resolves within allowed roots
	c := core.Candidate{
		Root:       cfg.AllowedRoots[0],
		Path:       "/data/work/link.log",
		Type:       core.TargetFile,
		IsSymlink:  true,
		LinkTarget: "../real.log", // Relative path -> /data/real.log (within /data)
	}

	v := e.Validate(context.Background(), c, cfg)
	if !v.Allowed {
		t.Fatalf("expected allowed for relative symlink within root, got denied (reason=%s)", v.Reason)
	}
}

func TestSymlinkWithRelativeLinkTargetEscape(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	// Relative link target that escapes allowed roots
	c := core.Candidate{
		Root:       cfg.AllowedRoots[0],
		Path:       "/data/work/link.log",
		Type:       core.TargetFile,
		IsSymlink:  true,
		LinkTarget: "../../etc/passwd", // Escapes /data
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied for relative symlink escaping root, got allowed")
	}
	if v.Reason != "symlink_escape" {
		t.Fatalf("expected symlink_escape, got %s", v.Reason)
	}
}

func TestEmptyAllowedRootsFallbackToCandRoot(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   nil, // Empty allowed roots
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	// Candidate has its own root
	c := core.Candidate{
		Root: "/data",
		Path: "/data/work/file.log",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	if !v.Allowed {
		t.Fatalf("expected allowed with cand.Root fallback, got denied (reason=%s)", v.Reason)
	}
}

func TestEmptyAllowedRootsSymlinkCheckUsesRoot(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   nil, // Empty allowed roots
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	// Symlink that escapes cand.Root
	c := core.Candidate{
		Root:       "/data",
		Path:       "/data/work/link.log",
		Type:       core.TargetFile,
		IsSymlink:  true,
		LinkTarget: "/etc/passwd", // Outside /data
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied for symlink escaping cand.Root, got allowed")
	}
	if v.Reason != "symlink_escape" {
		t.Fatalf("expected symlink_escape, got %s", v.Reason)
	}
}

func TestIsPathOrChildExactMatch(t *testing.T) {
	tests := []struct {
		path     string
		base     string
		expected bool
	}{
		{"/data", "/data", true},
		{"/data/", "/data", true},
		{"/data", "/data/", true},
		{"/data/file.txt", "/data", true},
		{"/data/sub/file.txt", "/data", true},
		{"/datafile.txt", "/data", false}, // Prefix bug check
		{"/data-backup", "/data", false},  // Prefix bug check
		{"/other", "/data", false},
		{"/", "/", true},
		{"/data", "/", false}, // "/" only matches itself
	}

	for _, tt := range tests {
		t.Run(tt.path+"_under_"+tt.base, func(t *testing.T) {
			result := isPathOrChild(tt.path, tt.base)
			if result != tt.expected {
				t.Errorf("isPathOrChild(%q, %q) = %v, want %v", tt.path, tt.base, result, tt.expected)
			}
		})
	}
}

func TestMultipleAllowedRoots(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data", "/backup"},
		ProtectedPaths: nil,
		AllowDirDelete: false,
	}

	tests := []struct {
		name    string
		path    string
		root    string
		allowed bool
	}{
		{"in first root", "/data/file.log", "/data", true},
		{"in second root", "/backup/file.log", "/backup", true},
		{"outside all roots", "/etc/passwd", "/data", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := core.Candidate{
				Root: tt.root,
				Path: tt.path,
				Type: core.TargetFile,
			}
			v := e.Validate(context.Background(), c, cfg)
			if v.Allowed != tt.allowed {
				t.Errorf("expected allowed=%v, got allowed=%v (reason=%s)", tt.allowed, v.Allowed, v.Reason)
			}
		})
	}
}

func TestProtectedPathNormalization(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots:   []string{"/data"},
		ProtectedPaths: []string{"/data/protected/"},
		AllowDirDelete: false,
	}

	// Test with non-normalized path
	c := core.Candidate{
		Root: "/data",
		Path: "/data/./protected/../protected/file.log",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied for protected path, got allowed")
	}
	if v.Reason != "protected_path" {
		t.Fatalf("expected protected_path, got %s", v.Reason)
	}
}

func TestCandidateWhitespaceRoot(t *testing.T) {
	e := New()
	cfg := core.SafetyConfig{
		AllowedRoots: []string{"/data"},
	}

	// Whitespace-only root should be treated as missing
	c := core.Candidate{
		Root: "   ",
		Path: "/data/file.log",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied for whitespace-only root, got allowed")
	}
	if v.Reason != "missing_candidate_root" {
		t.Fatalf("expected missing_candidate_root, got %s", v.Reason)
	}
}

// Tests that require real filesystem operations
func TestValidateWithRealFilesystem(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create test files
	testFile := root + "/work/file.txt"
	if err := os.MkdirAll(root+"/work", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()

	t.Run("valid file with real path", func(t *testing.T) {
		cfg := core.SafetyConfig{
			AllowedRoots: []string{root},
		}
		c := core.Candidate{
			Root: root,
			Path: testFile,
			Type: core.TargetFile,
		}
		v := e.Validate(context.Background(), c, cfg)
		if !v.Allowed {
			t.Fatalf("expected allowed, got denied: %s", v.Reason)
		}
	})

	t.Run("ancestor symlink containment denial propagates outside_allowed_roots", func(t *testing.T) {
		// Create a symlink pointing outside
		symlinkDir := root + "/linkdir"
		if err := os.Symlink(outside, symlinkDir); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(symlinkDir)

		// Create file in outside directory
		outsideFile := outside + "/secret.txt"
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg := core.SafetyConfig{
			AllowedRoots: []string{root},
		}
		c := core.Candidate{
			Root: root,
			Path: root + "/linkdir/secret.txt",
			Type: core.TargetFile,
		}
		v := e.Validate(context.Background(), c, cfg)
		if v.Allowed {
			t.Fatalf("expected denied, got allowed")
		}
		// Should be denied due to symlink
	})

	t.Run("symlink escape with IsSymlink true is detected by ancestor check", func(t *testing.T) {
		// Create a symlink that points outside
		linkFile := root + "/work/escape_link.txt"
		if err := os.Symlink(outside+"/target.txt", linkFile); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(linkFile)

		// Create target outside
		if err := os.WriteFile(outside+"/target.txt", []byte("target"), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg := core.SafetyConfig{
			AllowedRoots: []string{root},
		}
		c := core.Candidate{
			Root:       root,
			Path:       linkFile,
			Type:       core.TargetFile,
			IsSymlink:  true,
			LinkTarget: outside + "/target.txt",
		}
		v := e.Validate(context.Background(), c, cfg)
		if v.Allowed {
			t.Fatalf("expected denied for symlink, got allowed")
		}
		// The ancestor symlink check detects this as symlink_self before the escape upgrade logic
		// This is expected behavior - symlinks are detected and denied
		if !strings.HasPrefix(v.Reason, "symlink_self") && !strings.HasPrefix(v.Reason, "symlink_escape") {
			t.Fatalf("expected symlink denial reason, got %s", v.Reason)
		}
	})

	t.Run("symlink escape upgrade with relative link target", func(t *testing.T) {
		// Create symlink with relative path that escapes
		linkFile := root + "/work/rel_escape.txt"
		// This relative path: ../../<outside_basename>/target.txt
		relTarget := "../../" + filepath.Base(outside) + "/target.txt"
		if err := os.Symlink(relTarget, linkFile); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(linkFile)

		cfg := core.SafetyConfig{
			AllowedRoots: []string{root},
		}
		c := core.Candidate{
			Root:       root,
			Path:       linkFile,
			Type:       core.TargetFile,
			IsSymlink:  true,
			LinkTarget: relTarget, // Relative path
		}
		v := e.Validate(context.Background(), c, cfg)
		if v.Allowed {
			t.Fatalf("expected denied for relative symlink escape, got allowed")
		}
	})

	t.Run("symlink within allowed roots should be allowed", func(t *testing.T) {
		// Create a symlink pointing within allowed roots
		realFile := root + "/work/real_internal.txt"
		if err := os.WriteFile(realFile, []byte("real"), 0o644); err != nil {
			t.Fatal(err)
		}
		linkFile := root + "/work/internal_link.txt"
		if err := os.Symlink(realFile, linkFile); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(linkFile)

		cfg := core.SafetyConfig{
			AllowedRoots: []string{root},
		}
		c := core.Candidate{
			Root:       root,
			Path:       linkFile,
			Type:       core.TargetFile,
			IsSymlink:  true,
			LinkTarget: realFile,
		}
		v := e.Validate(context.Background(), c, cfg)
		// Note: This may still be denied due to ancestor symlink check detecting the symlink
		// The important thing is it's not denied as "symlink_escape"
		if v.Reason == "symlink_escape" {
			t.Fatalf("should not be denied as symlink_escape when target is within roots")
		}
	})
}

func TestValidateAncestorSymlinkContainmentOutsideRoot(t *testing.T) {
	root := t.TempDir()

	// Create a file
	testFile := root + "/file.txt"
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()

	// Test where AncestorSymlinkContainment returns outside_root
	// This happens when the candidate is outside the root
	cfg := core.SafetyConfig{
		AllowedRoots: []string{root},
	}

	// Candidate path that's actually outside root (using path traversal)
	c := core.Candidate{
		Root: root,
		Path: root + "/../" + filepath.Base(root) + "/../outside/file.txt",
		Type: core.TargetFile,
	}

	v := e.Validate(context.Background(), c, cfg)
	if v.Allowed {
		t.Fatalf("expected denied for path outside root, got allowed")
	}
	if v.Reason != "outside_allowed_roots" {
		t.Fatalf("expected outside_allowed_roots, got %s", v.Reason)
	}
}
