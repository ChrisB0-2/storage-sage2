package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
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
	errs = append(errs, ValidateDaemon(cfg.Daemon)...)

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

	// Validate Loki config if present
	if log.Loki != nil {
		errs = append(errs, ValidateLoki(*log.Loki)...)
	}

	return errs
}

// ValidateLoki checks Loki configuration.
func ValidateLoki(loki LokiConfig) []ValidationError {
	var errs []ValidationError

	// If Loki is enabled, URL must be provided and valid
	if loki.Enabled {
		if loki.URL == "" {
			errs = append(errs, ValidationError{
				Field:   "logging.loki.url",
				Message: "URL is required when Loki is enabled",
			})
		} else {
			// Validate URL format
			u, err := url.Parse(loki.URL)
			if err != nil {
				errs = append(errs, ValidationError{
					Field:   "logging.loki.url",
					Message: fmt.Sprintf("invalid URL: %v", err),
				})
			} else if u.Scheme != "http" && u.Scheme != "https" {
				errs = append(errs, ValidationError{
					Field:   "logging.loki.url",
					Message: fmt.Sprintf("URL scheme must be http or https, got %q", u.Scheme),
				})
			}
		}
	}

	// BatchSize must be positive if set
	if loki.BatchSize < 0 {
		errs = append(errs, ValidationError{
			Field:   "logging.loki.batch_size",
			Message: "must be >= 0",
		})
	}

	// BatchWait must be positive if set
	if loki.BatchWait < 0 {
		errs = append(errs, ValidationError{
			Field:   "logging.loki.batch_wait",
			Message: "must be >= 0",
		})
	}

	return errs
}

// ValidateDaemon checks daemon configuration.
func ValidateDaemon(d DaemonConfig) []ValidationError {
	var errs []ValidationError

	// If daemon is enabled, validate its settings
	if d.Enabled {
		// Schedule must be provided when daemon is enabled
		if d.Schedule == "" {
			errs = append(errs, ValidationError{
				Field:   "daemon.schedule",
				Message: "schedule is required when daemon mode is enabled",
			})
		} else {
			// Validate schedule is parseable
			if _, err := parseSchedule(d.Schedule); err != nil {
				errs = append(errs, ValidationError{
					Field:   "daemon.schedule",
					Message: fmt.Sprintf("invalid schedule %q: %v", d.Schedule, err),
				})
			}
		}
	}

	// Validate HTTP address format if provided
	if d.HTTPAddr != "" {
		if _, _, err := net.SplitHostPort(d.HTTPAddr); err != nil {
			errs = append(errs, ValidationError{
				Field:   "daemon.http_addr",
				Message: fmt.Sprintf("invalid address %q: %v", d.HTTPAddr, err),
			})
		}
	}

	// Validate metrics address format if provided
	if d.MetricsAddr != "" {
		if _, _, err := net.SplitHostPort(d.MetricsAddr); err != nil {
			errs = append(errs, ValidationError{
				Field:   "daemon.metrics_addr",
				Message: fmt.Sprintf("invalid address %q: %v", d.MetricsAddr, err),
			})
		}
	}

	return errs
}

// parseSchedule parses a simple schedule string into a duration.
// Supports: "1h", "30m", "6h", etc. or cron-like "@every 1h".
func parseSchedule(s string) (time.Duration, error) {
	// Handle @every syntax
	if len(s) > 7 && s[:7] == "@every " {
		s = s[7:]
	}
	return time.ParseDuration(s)
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
