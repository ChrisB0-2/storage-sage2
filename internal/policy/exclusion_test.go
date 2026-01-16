package policy

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestExclusionPolicy_NoPatterns(t *testing.T) {
	p := NewExclusionPolicy(nil)
	c := core.Candidate{Path: "/tmp/test.txt"}
	env := core.EnvSnapshot{Now: time.Now()}

	dec := p.Evaluate(context.Background(), c, env)
	if !dec.Allow {
		t.Error("expected allow with no patterns")
	}
	if dec.Reason != "no_exclusions" {
		t.Errorf("expected reason 'no_exclusions', got %q", dec.Reason)
	}
}

func TestExclusionPolicy_BasicGlob(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool // true = allowed (not excluded), false = blocked (excluded)
	}{
		{
			name:     "match extension",
			patterns: []string{"*.important"},
			path:     "/tmp/data.important",
			want:     false,
		},
		{
			name:     "no match extension",
			patterns: []string{"*.important"},
			path:     "/tmp/data.txt",
			want:     true,
		},
		{
			name:     "match prefix",
			patterns: []string{"keep-*"},
			path:     "/var/log/keep-this.log",
			want:     false,
		},
		{
			name:     "no match prefix",
			patterns: []string{"keep-*"},
			path:     "/var/log/delete-this.log",
			want:     true,
		},
		{
			name:     "match exact filename",
			patterns: []string{".gitignore"},
			path:     "/project/.gitignore",
			want:     false,
		},
		{
			name:     "multiple patterns - first matches",
			patterns: []string{"*.bak", "*.important", "keep-*"},
			path:     "/tmp/file.bak",
			want:     false,
		},
		{
			name:     "multiple patterns - second matches",
			patterns: []string{"*.bak", "*.important", "keep-*"},
			path:     "/tmp/data.important",
			want:     false,
		},
		{
			name:     "multiple patterns - none match",
			patterns: []string{"*.bak", "*.important", "keep-*"},
			path:     "/tmp/normal.txt",
			want:     true,
		},
		{
			name:     "match single char wildcard",
			patterns: []string{"log?.txt"},
			path:     "/var/log/log1.txt",
			want:     false,
		},
		{
			name:     "character class",
			patterns: []string{"[0-9]*.log"},
			path:     "/var/log/123error.log",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewExclusionPolicy(tt.patterns)
			c := core.Candidate{Path: tt.path}
			env := core.EnvSnapshot{Now: time.Now()}

			dec := p.Evaluate(context.Background(), c, env)
			if dec.Allow != tt.want {
				t.Errorf("path %q: got Allow=%v, want %v (reason: %s)", tt.path, dec.Allow, tt.want, dec.Reason)
			}
		})
	}
}

func TestExclusionPolicy_RecursiveGlob(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{
			name:     "match backup directory - any file",
			patterns: []string{"backup/**"},
			path:     "/data/backup/file.txt",
			want:     false,
		},
		{
			name:     "match backup directory - nested",
			patterns: []string{"backup/**"},
			path:     "/data/backup/subdir/file.txt",
			want:     false,
		},
		{
			name:     "no match - different directory",
			patterns: []string{"backup/**"},
			path:     "/data/cache/file.txt",
			want:     true,
		},
		{
			name:     "match .git directory",
			patterns: []string{".git/**"},
			path:     "/project/.git/objects/pack/file",
			want:     false,
		},
		{
			name:     "match node_modules",
			patterns: []string{"node_modules/**"},
			path:     "/app/node_modules/lodash/index.js",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewExclusionPolicy(tt.patterns)
			c := core.Candidate{Path: tt.path}
			env := core.EnvSnapshot{Now: time.Now()}

			dec := p.Evaluate(context.Background(), c, env)
			if dec.Allow != tt.want {
				t.Errorf("path %q: got Allow=%v, want %v (reason: %s)", tt.path, dec.Allow, tt.want, dec.Reason)
			}
		})
	}
}

func TestExclusionPolicy_ReasonContainsPattern(t *testing.T) {
	p := NewExclusionPolicy([]string{"*.secret"})
	c := core.Candidate{Path: "/tmp/password.secret"}
	env := core.EnvSnapshot{Now: time.Now()}

	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Fatal("expected file to be excluded")
	}
	if dec.Reason != "excluded:*.secret" {
		t.Errorf("expected reason to contain pattern, got %q", dec.Reason)
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/a/b/c", []string{"a", "b", "c"}},
		{"/var/log/app.log", []string{"var", "log", "app.log"}},
		{"relative/path", []string{"relative", "path"}},
		{"/", nil},
		{".", nil},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := splitPath(tt.path)
			if len(got) != len(tt.want) {
				t.Errorf("splitPath(%q) = %v, want %v", tt.path, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitPath(%q) = %v, want %v", tt.path, got, tt.want)
					break
				}
			}
		})
	}
}

func TestHasPathPrefix(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"/data/backup/file.txt", "backup", true},
		{"/data/backup/sub/file.txt", "backup", true},
		{"/data/cache/file.txt", "backup", false},
		{"/var/log/backup.log", "backup", false}, // backup is filename, not dir
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.prefix, func(t *testing.T) {
			got := hasPathPrefix(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("hasPathPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}
