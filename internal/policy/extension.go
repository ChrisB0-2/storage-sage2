package policy

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// ExtensionPolicy allows candidates with specific file extensions.
type ExtensionPolicy struct {
	Extensions []string // e.g., [".tmp", ".log", ".bak"]
}

// NewExtensionPolicy creates a policy that allows files matching any of the given extensions.
// Extensions should include the dot (e.g., ".tmp", ".log").
func NewExtensionPolicy(extensions []string) *ExtensionPolicy {
	// Normalize extensions to lowercase
	normalized := make([]string, len(extensions))
	for i, ext := range extensions {
		normalized[i] = strings.ToLower(strings.TrimSpace(ext))
	}
	return &ExtensionPolicy{Extensions: normalized}
}

func (p *ExtensionPolicy) Evaluate(_ context.Context, c core.Candidate, _ core.EnvSnapshot) core.Decision {
	ext := strings.ToLower(filepath.Ext(c.Path))
	for _, allowed := range p.Extensions {
		if ext == allowed {
			return core.Decision{Allow: true, Reason: "extension_match", Score: 100}
		}
	}
	return core.Decision{Allow: false, Reason: "extension_mismatch", Score: 0}
}
