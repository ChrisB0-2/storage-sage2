package policy

import (
	"context"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

func TestExtensionPolicyAllowsMatchingExtension(t *testing.T) {
	p := NewExtensionPolicy([]string{".tmp", ".log", ".bak"})

	env := core.EnvSnapshot{Now: time.Now()}

	tests := []struct {
		path string
		want bool
	}{
		{"/data/cache.tmp", true},
		{"/data/app.log", true},
		{"/data/config.bak", true},
		{"/data/important.txt", false},
		{"/data/script.sh", false},
	}

	for _, tt := range tests {
		c := core.Candidate{Path: tt.path}
		dec := p.Evaluate(context.Background(), c, env)
		if dec.Allow != tt.want {
			t.Errorf("path %s: expected Allow=%v, got %v (reason: %s)", tt.path, tt.want, dec.Allow, dec.Reason)
		}
	}
}

func TestExtensionPolicyCaseInsensitive(t *testing.T) {
	p := NewExtensionPolicy([]string{".TMP", ".LOG"})

	env := core.EnvSnapshot{Now: time.Now()}

	// Should match regardless of case
	tests := []struct {
		path string
		want bool
	}{
		{"/data/file.tmp", true},
		{"/data/file.TMP", true},
		{"/data/file.Tmp", true},
		{"/data/file.log", true},
		{"/data/file.LOG", true},
	}

	for _, tt := range tests {
		c := core.Candidate{Path: tt.path}
		dec := p.Evaluate(context.Background(), c, env)
		if dec.Allow != tt.want {
			t.Errorf("path %s: expected Allow=%v, got %v", tt.path, tt.want, dec.Allow)
		}
	}
}

func TestExtensionPolicyNoExtension(t *testing.T) {
	p := NewExtensionPolicy([]string{".tmp"})

	env := core.EnvSnapshot{Now: time.Now()}

	// File with no extension should not match
	c := core.Candidate{Path: "/data/README"}
	dec := p.Evaluate(context.Background(), c, env)
	if dec.Allow {
		t.Error("expected file without extension to be denied")
	}
}
