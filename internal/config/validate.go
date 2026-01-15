package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidationError contains details about a single validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation failed: %s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("config validation failed:\n")
	for _, err := range e {
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", err.Field, err.Message))
	}
	return sb.String()
}

// RequiredProtectedPaths are the minimum paths that MUST be protected.
var RequiredProtectedPaths = []string{
	"/boot", "/etc", "/usr", "/var", "/sys", "/proc", "/dev",
}

// ValidModes are the allowed execution modes.
var ValidModes = []string{"dry-run", "execute"}

// ValidLogLevels are the allowed log levels.
var ValidLogLevels = []string{"debug", "info", "warn", "error"}

// ValidLogFormats are the allowed log formats.
var ValidLogFormats = []string{"json", "text"}

// ValidCompositeModes are the allowed composite policy modes.
var ValidCompositeModes = []string{"and", "or"}

// Validate performs comprehensive validation of the configuration.
// It returns all validation errors found (not just the first).
// Returns nil if the configuration is valid.
func Validate(cfg *Config) error {
	var errs ValidationErrors

	errs = append(errs, ValidateRoots(cfg.Scan.Roots)...)
	errs = append(errs, ValidatePolicy(cfg.Policy)...)
	errs = append(errs, ValidateSafety(cfg.Safety)...)
	errs = append(errs, ValidateExecution(cfg.Execution)...)
	errs = append(errs, ValidateLogging(cfg.Logging)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateFinal performs validation after CLI flags have been merged.
// This catches errors that depend on the final merged state.
func ValidateFinal(cfg *Config) error {
	var errs ValidationErrors

	// After merge, at least one root MUST be provided
	if len(cfg.Scan.Roots) == 0 {
		errs = append(errs, ValidationError{
			Field:   "scan.roots",
			Message: "at least one root directory is required (via config or -root flag)",
		})
	}

	// Re-validate roots in final state
	errs = append(errs, ValidateRoots(cfg.Scan.Roots)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateRoots checks that scan.roots are valid (absolute, clean paths).
func ValidateRoots(roots []string) []ValidationError {
	var errs []ValidationError

	for i, root := range roots {
		if root == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("scan.roots[%d]", i),
				Message: "path must not be empty",
			})
			continue
		}

		// Must be absolute
		if !filepath.IsAbs(root) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("scan.roots[%d]", i),
				Message: fmt.Sprintf("path must be absolute: %q", root),
			})
			continue
		}

		// Must be cleaned (no . or ..)
		cleaned := filepath.Clean(root)
		if root != cleaned {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("scan.roots[%d]", i),
				Message: fmt.Sprintf("path must be clean (use %q not %q)", cleaned, root),
			})
		}
	}

	return errs
}

// ValidatePolicy checks policy constraints.
func ValidatePolicy(pol PolicyConfig) []ValidationError {
	var errs []ValidationError

	// min_age_days >= 0
	if pol.MinAgeDays < 0 {
		errs = append(errs, ValidationError{
			Field:   "policy.min_age_days",
			Message: "must be >= 0",
		})
	}

	// min_size_mb >= 0
	if pol.MinSizeMB < 0 {
		errs = append(errs, ValidationError{
			Field:   "policy.min_size_mb",
			Message: "must be >= 0",
		})
	}

	// composite_mode must be "and" or "or" (or empty for default)
	if pol.CompositeMode != "" && !contains(ValidCompositeModes, pol.CompositeMode) {
		errs = append(errs, ValidationError{
			Field:   "policy.composite_mode",
			Message: fmt.Sprintf("must be one of %v, got %q", ValidCompositeModes, pol.CompositeMode),
		})
	}

	return errs
}

// ValidateSafety checks safety configuration for required protected paths.
func ValidateSafety(safe SafetyConfig) []ValidationError {
	var errs []ValidationError

	// Build set of configured protected paths (normalized)
	protectedSet := make(map[string]bool)
	for _, p := range safe.ProtectedPaths {
		protectedSet[filepath.Clean(p)] = true
	}

	// Check that all required protected paths are present
	for _, required := range RequiredProtectedPaths {
		if !protectedSet[required] {
			errs = append(errs, ValidationError{
				Field:   "safety.protected_paths",
				Message: fmt.Sprintf("must include required path: %s", required),
			})
		}
	}

	return errs
}

// ValidateExecution checks execution mode and audit path.
func ValidateExecution(exec ExecutionConfig) []ValidationError {
	var errs []ValidationError

	// mode must be "dry-run" or "execute"
	if !contains(ValidModes, exec.Mode) {
		errs = append(errs, ValidationError{
			Field:   "execution.mode",
			Message: fmt.Sprintf("must be one of %v, got %q", ValidModes, exec.Mode),
		})
	}

	// max_items must be > 0
	if exec.MaxItems <= 0 {
		errs = append(errs, ValidationError{
			Field:   "execution.max_items",
			Message: "must be > 0",
		})
	}

	// Note: audit_path validation is intentionally relaxed for CLI-only mode
	// It will be empty by default and that's acceptable

	return errs
}

// ValidateLogging checks logging configuration.
func ValidateLogging(log LoggingConfig) []ValidationError {
	var errs []ValidationError

	// level must be valid (or empty for default)
	if log.Level != "" && !contains(ValidLogLevels, log.Level) {
		errs = append(errs, ValidationError{
			Field:   "logging.level",
			Message: fmt.Sprintf("must be one of %v, got %q", ValidLogLevels, log.Level),
		})
	}

	// format must be "json" or "text" (or empty for default)
	if log.Format != "" && !contains(ValidLogFormats, log.Format) {
		errs = append(errs, ValidationError{
			Field:   "logging.format",
			Message: fmt.Sprintf("must be one of %v, got %q", ValidLogFormats, log.Format),
		})
	}

	return errs
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
