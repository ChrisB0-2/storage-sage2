package safety

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAncestorSymlinkContainment_CoreCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies; this suite is intended for Linux (your target).")
	}

	root := t.TempDir()
	outside := t.TempDir()

	mkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	writeFile := func(p string, b []byte) {
		t.Helper()
		mkdir(filepath.Dir(p))
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("writeFile %s: %v", p, err)
		}
	}

	symlink := func(target, linkPath string) {
		t.Helper()
		mkdir(filepath.Dir(linkPath))
		if err := os.Symlink(target, linkPath); err != nil {
			t.Fatalf("symlink %s -> %s: %v", linkPath, target, err)
		}
	}

	t.Run("Happy path: root/data/a.txt allowed", func(t *testing.T) {
		p := filepath.Join(root, "data", "a.txt")
		writeFile(p, []byte("ok"))

		v := AncestorSymlinkContainment(root, p, AncestorSymlinkOptions{AllowRootSymlink: true})
		if !v.Allowed {
			t.Fatalf("expected allowed, got denied: %s", v.Reason)
		}
	})

	t.Run("Symlink is the file: root/data/link.txt denied", func(t *testing.T) {
		real := filepath.Join(root, "data", "real.txt")
		link := filepath.Join(root, "data", "link.txt")

		writeFile(real, []byte("real"))
		symlink(real, link)

		v := AncestorSymlinkContainment(root, link, AncestorSymlinkOptions{AllowRootSymlink: true})
		if v.Allowed {
			t.Fatalf("expected denied, got allowed")
		}
		if !hasPrefix(v.Reason, ReasonSymlinkSelf) && !hasPrefix(v.Reason, ReasonSymlinkAncestor) {
			t.Fatalf("expected symlink reason, got: %s", v.Reason)
		}
	})

	t.Run("Symlink in ancestor dir: root/dirlink -> outside, candidate denied", func(t *testing.T) {
		secret := filepath.Join(outside, "secret.txt")
		writeFile(secret, []byte("secret"))

		dirlink := filepath.Join(root, "dirlink")
		symlink(outside, dirlink)

		cand := filepath.Join(root, "dirlink", "secret.txt")

		v := AncestorSymlinkContainment(root, cand, AncestorSymlinkOptions{AllowRootSymlink: true})
		if v.Allowed {
			t.Fatalf("expected denied, got allowed")
		}
		if !hasPrefix(v.Reason, ReasonSymlinkAncestor) && !hasPrefix(v.Reason, ReasonSymlinkSelf) {
			t.Fatalf("expected symlink reason, got: %s", v.Reason)
		}
	})

	t.Run("Symlink hop that still looks inside: replace root/a/b with symlink -> outside", func(t *testing.T) {
		real := filepath.Join(root, "a", "b", "c.txt")
		writeFile(real, []byte("real"))

		// Replace root/a/b with a symlink to outside
		bDir := filepath.Join(root, "a", "b")
		if err := os.RemoveAll(bDir); err != nil {
			t.Fatalf("removeAll %s: %v", bDir, err)
		}
		symlink(outside, bDir)

		cand := filepath.Join(root, "a", "b", "c.txt") // still begins with root/a/b/...
		v := AncestorSymlinkContainment(root, cand, AncestorSymlinkOptions{AllowRootSymlink: true})

		if v.Allowed {
			t.Fatalf("expected denied, got allowed")
		}
		if !hasPrefix(v.Reason, ReasonSymlinkAncestor) && !hasPrefix(v.Reason, ReasonSymlinkSelf) {
			t.Fatalf("expected symlink reason, got: %s", v.Reason)
		}
	})

	t.Run("Outside root by relative traversal: Join(root, .., outside, x) denied", func(t *testing.T) {
		cand := filepath.Join(root, "..", "outside", "x")

		v := AncestorSymlinkContainment(root, cand, AncestorSymlinkOptions{AllowRootSymlink: true})
		if v.Allowed {
			t.Fatalf("expected denied, got allowed")
		}
		if v.Reason != ReasonOutsideRoot {
			t.Fatalf("expected %s, got: %s", ReasonOutsideRoot, v.Reason)
		}
	})

	t.Run("TOCTOU: safe at scan, then swap dir for symlink -> denied at execute", func(t *testing.T) {
		// Initial real path
		file := filepath.Join(root, "work", "cache", "file.bin")
		writeFile(file, []byte("bin"))

		// Scan-time check says safe
		scan := AncestorSymlinkContainment(root, file, AncestorSymlinkOptions{AllowRootSymlink: true})
		if !scan.Allowed {
			t.Fatalf("expected scan allowed, got denied: %s", scan.Reason)
		}

		// Swap cache dir for a symlink
		cacheDir := filepath.Join(root, "work", "cache")
		if err := os.RemoveAll(cacheDir); err != nil {
			t.Fatalf("removeAll %s: %v", cacheDir, err)
		}
		symlink(outside, cacheDir)

		// Execute-time re-check must deny
		exec := AncestorSymlinkContainment(root, file, AncestorSymlinkOptions{AllowRootSymlink: true})
		if exec.Allowed {
			t.Fatalf("expected execute denied, got allowed")
		}
		if !hasPrefix(exec.Reason, ReasonSymlinkAncestor) && !hasPrefix(exec.Reason, ReasonSymlinkSelf) {
			t.Fatalf("expected symlink reason, got: %s", exec.Reason)
		}
	})
}

func hasPrefix(got, wantPrefix string) bool {
	return len(got) >= len(wantPrefix) && got[:len(wantPrefix)] == wantPrefix
}

func TestAncestorSymlinkContainment_InvalidArguments(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		candidate string
	}{
		{"empty root", "", "/data/file.txt"},
		{"empty candidate", "/data", ""},
		{"both empty", "", ""},
		{"whitespace root", "   ", "/data/file.txt"},
		{"whitespace candidate", "/data", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := AncestorSymlinkContainment(tt.root, tt.candidate, AncestorSymlinkOptions{})
			if v.Allowed {
				t.Errorf("expected denied for invalid args, got allowed")
			}
			if v.Reason != ReasonInvalidArguments {
				t.Errorf("expected reason %s, got %s", ReasonInvalidArguments, v.Reason)
			}
		})
	}
}

func TestAncestorSymlinkContainment_RootEqualsCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	root := t.TempDir()

	// When candidate == root, should be allowed
	v := AncestorSymlinkContainment(root, root, AncestorSymlinkOptions{AllowRootSymlink: true})
	if !v.Allowed {
		t.Fatalf("expected allowed when candidate equals root, got denied: %s", v.Reason)
	}
	if v.Reason != ReasonOK {
		t.Fatalf("expected reason %s, got %s", ReasonOK, v.Reason)
	}
}

func TestAncestorSymlinkContainment_RootSymlinkBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	realRoot := t.TempDir()
	symlinkRoot := filepath.Join(t.TempDir(), "symlink_root")

	// Create a symlink as root
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create a file inside
	testFile := filepath.Join(realRoot, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	// With AllowRootSymlink=false, should deny
	v := AncestorSymlinkContainment(symlinkRoot, filepath.Join(symlinkRoot, "file.txt"), AncestorSymlinkOptions{AllowRootSymlink: false})
	if v.Allowed {
		t.Fatalf("expected denied when root is symlink and AllowRootSymlink=false, got allowed")
	}
	if !hasPrefix(v.Reason, ReasonSymlinkAncestor) {
		t.Fatalf("expected reason starting with %s, got %s", ReasonSymlinkAncestor, v.Reason)
	}

	// With AllowRootSymlink=true, should allow
	v = AncestorSymlinkContainment(symlinkRoot, filepath.Join(symlinkRoot, "file.txt"), AncestorSymlinkOptions{AllowRootSymlink: true})
	if !v.Allowed {
		t.Fatalf("expected allowed when root is symlink and AllowRootSymlink=true, got denied: %s", v.Reason)
	}
}

func TestAncestorSymlinkContainment_NonExistentPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	root := t.TempDir()
	nonExistent := filepath.Join(root, "does", "not", "exist.txt")

	// Non-existent path should result in stat error
	v := AncestorSymlinkContainment(root, nonExistent, AncestorSymlinkOptions{AllowRootSymlink: true})
	if v.Allowed {
		t.Fatalf("expected denied for non-existent path, got allowed")
	}
	if !hasPrefix(v.Reason, ReasonStatError) {
		t.Fatalf("expected reason starting with %s, got %s", ReasonStatError, v.Reason)
	}
}

func TestRelIsOutside(t *testing.T) {
	tests := []struct {
		rel      string
		expected bool
	}{
		{"..", true},
		{"../foo", true},
		{"..\\foo", runtime.GOOS == "windows"}, // Windows separator
		{"foo/..", false},                      // Not starting with ..
		{"foo", false},
		{".", false},
		{"foo/bar", false},
	}

	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			result := relIsOutside(tt.rel)
			if result != tt.expected {
				t.Errorf("relIsOutside(%q) = %v, want %v", tt.rel, result, tt.expected)
			}
		})
	}
}

func TestSplitRel(t *testing.T) {
	tests := []struct {
		rel      string
		expected []string
	}{
		{".", nil},
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo/bar", []string{"foo", "bar"}},
		{"foo/bar/baz", []string{"foo", "bar", "baz"}},
	}

	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			// Normalize path separator for the OS
			rel := filepath.FromSlash(tt.rel)
			result := splitRel(rel)

			if len(result) != len(tt.expected) {
				t.Errorf("splitRel(%q) = %v, want %v", tt.rel, result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitRel(%q)[%d] = %q, want %q", tt.rel, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestIsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	dir := t.TempDir()

	// Regular file
	regularFile := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	isLink, err := isSymlink(regularFile)
	if err != nil {
		t.Fatalf("isSymlink error: %v", err)
	}
	if isLink {
		t.Error("expected regular file to not be symlink")
	}

	// Symlink
	linkFile := filepath.Join(dir, "link.txt")
	if err := os.Symlink(regularFile, linkFile); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	isLink, err = isSymlink(linkFile)
	if err != nil {
		t.Fatalf("isSymlink error: %v", err)
	}
	if !isLink {
		t.Error("expected symlink to be detected")
	}

	// Non-existent path
	nonExistent := filepath.Join(dir, "nonexistent")
	_, err = isSymlink(nonExistent)
	if err == nil {
		t.Error("expected error for non-existent path")
	}

	// Directory
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	isLink, err = isSymlink(subdir)
	if err != nil {
		t.Fatalf("isSymlink error: %v", err)
	}
	if isLink {
		t.Error("expected directory to not be symlink")
	}
}

func TestAbsClean(t *testing.T) {
	// Test that absClean properly cleans and makes absolute
	result, err := absClean("./foo/../bar")
	if err != nil {
		t.Fatalf("absClean error: %v", err)
	}

	// Should be absolute
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %s", result)
	}

	// Should be cleaned (no . or ..)
	if filepath.Clean(result) != result {
		t.Errorf("expected clean path, got %s", result)
	}
}

func TestAncestorSymlinkContainment_SymlinkAsCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	root := t.TempDir()

	// Create real file
	realFile := filepath.Join(root, "real.txt")
	if err := os.WriteFile(realFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	// Create symlink to real file
	linkFile := filepath.Join(root, "link.txt")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Symlink as candidate (final node) should be detected as symlink_self
	v := AncestorSymlinkContainment(root, linkFile, AncestorSymlinkOptions{AllowRootSymlink: true})
	if v.Allowed {
		t.Fatalf("expected denied for symlink candidate, got allowed")
	}
	if !hasPrefix(v.Reason, ReasonSymlinkSelf) {
		t.Fatalf("expected reason starting with %s, got %s", ReasonSymlinkSelf, v.Reason)
	}
}

func TestAncestorSymlinkContainment_DeepNesting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	root := t.TempDir()

	// Create deep nested structure
	deepPath := filepath.Join(root, "a", "b", "c", "d", "e", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(deepPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	// Should be allowed
	v := AncestorSymlinkContainment(root, deepPath, AncestorSymlinkOptions{AllowRootSymlink: true})
	if !v.Allowed {
		t.Fatalf("expected allowed for deep nested path, got denied: %s", v.Reason)
	}
}

func TestAncestorSymlinkContainment_SymlinkInMiddle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior on Windows varies")
	}

	root := t.TempDir()
	outside := t.TempDir()

	// Create target outside
	outsideDir := filepath.Join(outside, "target")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	// Create symlink in middle of path
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	middleLink := filepath.Join(root, "a", "b")
	if err := os.Symlink(outsideDir, middleLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Candidate path traverses the symlink
	candidate := filepath.Join(root, "a", "b", "secret.txt")

	v := AncestorSymlinkContainment(root, candidate, AncestorSymlinkOptions{AllowRootSymlink: true})
	if v.Allowed {
		t.Fatalf("expected denied for path with symlink in middle, got allowed")
	}
	if !hasPrefix(v.Reason, ReasonSymlinkAncestor) {
		t.Fatalf("expected reason starting with %s, got %s", ReasonSymlinkAncestor, v.Reason)
	}
}
