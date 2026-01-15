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
