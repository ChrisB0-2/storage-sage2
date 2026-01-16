package policy

import (
	"context"
	"path/filepath"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// ExclusionPolicy denies deletion of files matching any exclusion pattern.
// Patterns use filepath.Match syntax (e.g., "*.important", "keep-*", "backup/**").
type ExclusionPolicy struct {
	patterns []string
}

// NewExclusionPolicy creates a policy that blocks files matching any pattern.
// Empty patterns slice means nothing is excluded (all files allowed).
func NewExclusionPolicy(patterns []string) *ExclusionPolicy {
	return &ExclusionPolicy{patterns: patterns}
}

func (p *ExclusionPolicy) Evaluate(ctx context.Context, c core.Candidate, env core.EnvSnapshot) core.Decision {
	if len(p.patterns) == 0 {
		return core.Decision{Allow: true, Reason: "no_exclusions", Score: 0}
	}

	baseName := filepath.Base(c.Path)

	for _, pattern := range p.patterns {
		// Try matching against base name first (most common case)
		if matched, err := filepath.Match(pattern, baseName); err == nil && matched {
			return core.Decision{
				Allow:  false,
				Reason: "excluded:" + pattern,
				Score:  0,
			}
		}

		// Also try matching against the full path for directory patterns
		if matched, err := filepath.Match(pattern, c.Path); err == nil && matched {
			return core.Decision{
				Allow:  false,
				Reason: "excluded:" + pattern,
				Score:  0,
			}
		}

		// Handle ** glob patterns (recursive match)
		if matchRecursive(pattern, c.Path) {
			return core.Decision{
				Allow:  false,
				Reason: "excluded:" + pattern,
				Score:  0,
			}
		}
	}

	return core.Decision{Allow: true, Reason: "not_excluded", Score: 0}
}

// matchRecursive handles ** patterns for recursive directory matching.
// Pattern "backup/**" matches any file under a "backup" directory.
func matchRecursive(pattern, path string) bool {
	// Check if pattern contains **
	if !containsDoubleStar(pattern) {
		return false
	}

	// Split pattern at **
	parts := splitAtDoubleStar(pattern)
	if len(parts) != 2 {
		return false
	}

	prefix := parts[0]
	suffix := parts[1]

	// Remove trailing slash from prefix if present
	prefix = filepath.Clean(prefix)
	if prefix == "." {
		prefix = ""
	}

	// Check if path starts with prefix
	if prefix != "" {
		// The path should contain the prefix as a directory component
		if !hasPathPrefix(path, prefix) {
			return false
		}
	}

	// If suffix is empty or just "/", any file under prefix matches
	if suffix == "" || suffix == "/" {
		return true
	}

	// Check if the remainder matches the suffix pattern
	suffix = filepath.Clean(suffix)
	baseName := filepath.Base(path)
	matched, _ := filepath.Match(suffix, baseName)
	return matched
}

func containsDoubleStar(pattern string) bool {
	for i := 0; i < len(pattern)-1; i++ {
		if pattern[i] == '*' && pattern[i+1] == '*' {
			return true
		}
	}
	return false
}

func splitAtDoubleStar(pattern string) []string {
	for i := 0; i < len(pattern)-1; i++ {
		if pattern[i] == '*' && pattern[i+1] == '*' {
			return []string{pattern[:i], pattern[i+2:]}
		}
	}
	return []string{pattern}
}

// hasPathPrefix checks if path contains prefix as a directory component.
func hasPathPrefix(path, prefix string) bool {
	// Clean both paths
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)

	// Check if prefix appears as a directory name in path
	pathParts := splitPath(path)
	prefixParts := splitPath(prefix)

	if len(prefixParts) > len(pathParts) {
		return false
	}

	// Look for prefix parts as consecutive components anywhere in path
	for i := 0; i <= len(pathParts)-len(prefixParts); i++ {
		match := true
		for j, pp := range prefixParts {
			if pathParts[i+j] != pp {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}

func splitPath(path string) []string {
	var parts []string
	for path != "" && path != "/" && path != "." {
		dir, file := filepath.Split(path)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		path = filepath.Clean(dir)
		if path == "/" || path == "." {
			break
		}
	}
	return parts
}
