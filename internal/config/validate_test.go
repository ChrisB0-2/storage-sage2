package config

import (
	"strings"
	"testing"
)

func TestValidateRoots_AbsolutePath(t *testing.T) {
	errs := ValidateRoots([]string{"/data", "/home/user"})
	if len(errs) > 0 {
		t.Fatalf("expected no errors for absolute paths, got: %v", errs)
	}
}

func TestValidateRoots_RelativePath(t *testing.T) {
	errs := ValidateRoots([]string{"./relative", "data"})
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors for relative paths, got: %d", len(errs))
	}
	for _, err := range errs {
		if !strings.Contains(err.Message, "absolute") {
			t.Errorf("expected absolute path error, got: %s", err.Message)
		}
	}
}

func TestValidateRoots_UncleanPath(t *testing.T) {
	errs := ValidateRoots([]string{"/data/../data/./work"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for unclean path, got: %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "clean") {
		t.Errorf("expected clean path error, got: %s", errs[0].Message)
	}
}

func TestValidateRoots_EmptyPath(t *testing.T) {
	errs := ValidateRoots([]string{""})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty path, got: %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "empty") {
		t.Errorf("expected empty path error, got: %s", errs[0].Message)
	}
}

func TestValidateRoots_EmptySlice(t *testing.T) {
	errs := ValidateRoots([]string{})
	if len(errs) > 0 {
		t.Fatalf("expected no errors for empty slice, got: %v", errs)
	}
}

func TestValidatePolicy_NegativeMinAgeDays(t *testing.T) {
	pol := PolicyConfig{MinAgeDays: -1}
	errs := ValidatePolicy(pol)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for negative min_age_days, got: %d", len(errs))
	}
	if errs[0].Field != "policy.min_age_days" {
		t.Errorf("expected field policy.min_age_days, got: %s", errs[0].Field)
	}
}

func TestValidatePolicy_NegativeMinSizeMB(t *testing.T) {
	pol := PolicyConfig{MinSizeMB: -5}
	errs := ValidatePolicy(pol)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for negative min_size_mb, got: %d", len(errs))
	}
	if errs[0].Field != "policy.min_size_mb" {
		t.Errorf("expected field policy.min_size_mb, got: %s", errs[0].Field)
	}
}

func TestValidatePolicy_ValidMinAgeDays(t *testing.T) {
	pol := PolicyConfig{MinAgeDays: 0, CompositeMode: "and"}
	errs := ValidatePolicy(pol)
	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}
}

func TestValidatePolicy_InvalidCompositeMode(t *testing.T) {
	pol := PolicyConfig{CompositeMode: "invalid"}
	errs := ValidatePolicy(pol)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid composite_mode, got: %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "and") || !strings.Contains(errs[0].Message, "or") {
		t.Errorf("expected error to mention valid modes, got: %s", errs[0].Message)
	}
}

func TestValidatePolicy_EmptyCompositeMode(t *testing.T) {
	pol := PolicyConfig{CompositeMode: ""}
	errs := ValidatePolicy(pol)
	if len(errs) > 0 {
		t.Fatalf("expected no errors for empty composite_mode (uses default), got: %v", errs)
	}
}

func TestValidateSafety_MissingRequiredPaths(t *testing.T) {
	safe := SafetyConfig{
		ProtectedPaths: []string{"/boot", "/etc"}, // missing 5 others
	}
	errs := ValidateSafety(safe)
	// Should have errors for missing /usr, /var, /sys, /proc, /dev
	if len(errs) != 5 {
		t.Fatalf("expected 5 errors for missing paths, got: %d", len(errs))
	}
}

func TestValidateSafety_AllRequiredPaths(t *testing.T) {
	safe := SafetyConfig{
		ProtectedPaths: []string{"/boot", "/etc", "/usr", "/var", "/sys", "/proc", "/dev"},
	}
	errs := ValidateSafety(safe)
	if len(errs) > 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}
}

func TestValidateSafety_ExtraPathsAllowed(t *testing.T) {
	safe := SafetyConfig{
		ProtectedPaths: []string{
			"/boot", "/etc", "/usr", "/var", "/sys", "/proc", "/dev",
			"/home", "/opt", "/custom",
		},
	}
	errs := ValidateSafety(safe)
	if len(errs) > 0 {
		t.Fatalf("expected no errors for extra protected paths, got: %v", errs)
	}
}

func TestValidateSafety_NormalizedPaths(t *testing.T) {
	// Paths with trailing slashes should still match
	safe := SafetyConfig{
		ProtectedPaths: []string{"/boot/", "/etc/", "/usr/", "/var/", "/sys/", "/proc/", "/dev/"},
	}
	errs := ValidateSafety(safe)
	if len(errs) > 0 {
		t.Fatalf("expected no errors for paths with trailing slashes, got: %v", errs)
	}
}

func TestValidateExecution_InvalidMode(t *testing.T) {
	exec := ExecutionConfig{
		Mode:     "invalid",
		MaxItems: 10,
	}
	errs := ValidateExecution(exec)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid mode, got: %d", len(errs))
	}
	if errs[0].Field != "execution.mode" {
		t.Errorf("expected field execution.mode, got: %s", errs[0].Field)
	}
}

func TestValidateExecution_ValidModes(t *testing.T) {
	for _, mode := range []string{"dry-run", "execute"} {
		exec := ExecutionConfig{
			Mode:     mode,
			MaxItems: 25,
		}
		errs := ValidateExecution(exec)
		if len(errs) > 0 {
			t.Fatalf("expected no errors for mode %q, got: %v", mode, errs)
		}
	}
}

func TestValidateExecution_ZeroMaxItems(t *testing.T) {
	exec := ExecutionConfig{
		Mode:     "dry-run",
		MaxItems: 0,
	}
	errs := ValidateExecution(exec)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for zero max_items, got: %d", len(errs))
	}
	if errs[0].Field != "execution.max_items" {
		t.Errorf("expected field execution.max_items, got: %s", errs[0].Field)
	}
}

func TestValidateExecution_NegativeMaxItems(t *testing.T) {
	exec := ExecutionConfig{
		Mode:     "dry-run",
		MaxItems: -5,
	}
	errs := ValidateExecution(exec)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for negative max_items, got: %d", len(errs))
	}
}

func TestValidateLogging_InvalidLevel(t *testing.T) {
	log := LoggingConfig{
		Level: "verbose",
	}
	errs := ValidateLogging(log)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid level, got: %d", len(errs))
	}
	if errs[0].Field != "logging.level" {
		t.Errorf("expected field logging.level, got: %s", errs[0].Field)
	}
}

func TestValidateLogging_ValidLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		log := LoggingConfig{Level: level}
		errs := ValidateLogging(log)
		if len(errs) > 0 {
			t.Fatalf("expected no errors for level %q, got: %v", level, errs)
		}
	}
}

func TestValidateLogging_EmptyLevel(t *testing.T) {
	log := LoggingConfig{Level: ""}
	errs := ValidateLogging(log)
	if len(errs) > 0 {
		t.Fatalf("expected no errors for empty level (uses default), got: %v", errs)
	}
}

func TestValidateLogging_InvalidFormat(t *testing.T) {
	log := LoggingConfig{
		Format: "xml",
	}
	errs := ValidateLogging(log)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid format, got: %d", len(errs))
	}
	if errs[0].Field != "logging.format" {
		t.Errorf("expected field logging.format, got: %s", errs[0].Field)
	}
}

func TestValidateLogging_ValidFormats(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		log := LoggingConfig{Format: format}
		errs := ValidateLogging(log)
		if len(errs) > 0 {
			t.Fatalf("expected no errors for format %q, got: %v", format, errs)
		}
	}
}

func TestValidate_FullValidConfig(t *testing.T) {
	cfg := Default()
	cfg.Scan.Roots = []string{"/data"}
	// Default already has valid values

	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Scan: ScanConfig{
			Roots: []string{"relative/path"}, // invalid
		},
		Policy: PolicyConfig{
			MinAgeDays:    -1,       // invalid
			CompositeMode: "badval", // invalid
		},
		Safety: SafetyConfig{
			ProtectedPaths: []string{}, // missing required
		},
		Execution: ExecutionConfig{
			Mode:     "badmode", // invalid
			MaxItems: 0,         // invalid
		},
		Logging: LoggingConfig{
			Level:  "badlevel",  // invalid
			Format: "badformat", // invalid
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation errors, got nil")
	}

	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got: %T", err)
	}

	// Should have multiple errors
	if len(verrs) < 5 {
		t.Errorf("expected at least 5 errors, got: %d", len(verrs))
	}
}

func TestValidateFinal_NoRoots(t *testing.T) {
	cfg := Default()
	cfg.Scan.Roots = []string{}

	err := ValidateFinal(cfg)
	if err == nil {
		t.Fatal("expected error for missing roots")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("expected error about roots, got: %v", err)
	}
}

func TestValidateFinal_WithRoots(t *testing.T) {
	cfg := Default()
	cfg.Scan.Roots = []string{"/data"}

	err := ValidateFinal(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := ValidationError{
		Field:   "test.field",
		Message: "test message",
	}
	expected := "config validation failed: test.field: test message"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		{Field: "field1", Message: "message1"},
		{Field: "field2", Message: "message2"},
	}
	result := errs.Error()
	if !strings.Contains(result, "field1") || !strings.Contains(result, "field2") {
		t.Errorf("expected both fields in error, got: %s", result)
	}
}

func TestValidationErrors_Empty(t *testing.T) {
	errs := ValidationErrors{}
	if errs.Error() != "" {
		t.Errorf("expected empty string for empty errors, got: %q", errs.Error())
	}
}
