package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete configuration for storage-sage.
type Config struct {
	Version       int                 `yaml:"version" json:"version"`
	Scan          ScanConfig          `yaml:"scan" json:"scan"`
	Policy        PolicyConfig        `yaml:"policy" json:"policy"`
	Safety        SafetyConfig        `yaml:"safety" json:"safety"`
	Execution     ExecutionConfig     `yaml:"execution" json:"execution"`
	Logging       LoggingConfig       `yaml:"logging" json:"logging"`
	Daemon        DaemonConfig        `yaml:"daemon" json:"daemon"`
	Metrics       MetricsConfig       `yaml:"metrics" json:"metrics"`
	Notifications NotificationsConfig `yaml:"notifications,omitempty" json:"notifications,omitempty"`
	Auth          *AuthConfig         `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// ScanConfig configures the filesystem scanning behavior.
type ScanConfig struct {
	Roots     []string `yaml:"roots" json:"roots"`
	Recursive bool     `yaml:"recursive" json:"recursive"`
	MaxDepth  int      `yaml:"max_depth" json:"max_depth"`
	// FollowSymlinks is accepted for configuration compatibility but intentionally
	// ignored. The scanner always uses lstat (not stat) to prevent symlink-based
	// attacks. Following symlinks would allow deletion of files outside allowed
	// roots via malicious symlink placement. This is a safety-critical design decision.
	FollowSymlinks bool `yaml:"follow_symlinks" json:"follow_symlinks"`
	IncludeDirs    bool `yaml:"include_dirs" json:"include_dirs"`
	IncludeFiles   bool `yaml:"include_files" json:"include_files"`
}

// PolicyConfig configures the file selection policy.
type PolicyConfig struct {
	MinAgeDays    int      `yaml:"min_age_days" json:"min_age_days"`
	MinSizeMB     int      `yaml:"min_size_mb" json:"min_size_mb"`
	Extensions    []string `yaml:"extensions" json:"extensions"`
	Exclusions    []string `yaml:"exclusions" json:"exclusions"`         // glob patterns to exclude from deletion
	CompositeMode string   `yaml:"composite_mode" json:"composite_mode"` // "and" or "or"
}

// SafetyConfig configures safety boundaries.
type SafetyConfig struct {
	ProtectedPaths       []string `yaml:"protected_paths" json:"protected_paths"`
	AllowDirDelete       bool     `yaml:"allow_dir_delete" json:"allow_dir_delete"`
	EnforceMountBoundary bool     `yaml:"enforce_mount_boundary" json:"enforce_mount_boundary"`
}

// ExecutionConfig configures execution behavior.
type ExecutionConfig struct {
	Mode               string        `yaml:"mode" json:"mode"` // "dry-run" or "execute"
	Timeout            time.Duration `yaml:"timeout" json:"timeout"`
	AuditPath          string        `yaml:"audit_path" json:"audit_path"`       // JSONL file path
	AuditDBPath        string        `yaml:"audit_db_path" json:"audit_db_path"` // SQLite database path
	MaxItems           int           `yaml:"max_items" json:"max_items"`
	MaxDeletionsPerRun int           `yaml:"max_deletions_per_run" json:"max_deletions_per_run"` // Stop after N deletions (0 = unlimited)
	TrashPath          string        `yaml:"trash_path" json:"trash_path"`                       // Soft-delete: move files here instead of deleting
	TrashMaxAge        time.Duration `yaml:"trash_max_age" json:"trash_max_age"`                 // Max age before trash is permanently deleted (0 = keep forever)
	TrashSigningKeyPath string       `yaml:"trash_signing_key_path" json:"trash_signing_key_path"` // Path to HMAC signing key for trash metadata
}

// LoggingConfig configures logging behavior.
type LoggingConfig struct {
	Level  string      `yaml:"level" json:"level"`   // "debug", "info", "warn", "error"
	Format string      `yaml:"format" json:"format"` // "json" or "text"
	Output string      `yaml:"output" json:"output"` // "stderr", "stdout", or file path
	Loki   *LokiConfig `yaml:"loki,omitempty" json:"loki,omitempty"`
}

// LokiConfig configures Loki log shipping.
type LokiConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	URL       string            `yaml:"url" json:"url"`               // e.g., http://localhost:3100
	BatchSize int               `yaml:"batch_size" json:"batch_size"` // Number of log entries before flush
	BatchWait time.Duration     `yaml:"batch_wait" json:"batch_wait"` // Max time before flush
	Labels    map[string]string `yaml:"labels" json:"labels"`         // Static labels for all log streams
	TenantID  string            `yaml:"tenant_id" json:"tenant_id"`   // X-Scope-OrgID header for multi-tenancy
}

// DaemonConfig configures daemon mode.
type DaemonConfig struct {
	Enabled        bool          `yaml:"enabled" json:"enabled"`
	HTTPAddr       string        `yaml:"http_addr" json:"http_addr"`
	MetricsAddr    string        `yaml:"metrics_addr" json:"metrics_addr"`
	Schedule       string        `yaml:"schedule" json:"schedule"`               // cron expression
	TriggerTimeout time.Duration `yaml:"trigger_timeout" json:"trigger_timeout"` // timeout for manual /trigger requests
	PIDFile        string        `yaml:"pid_file" json:"pid_file"`               // PID file path for single-instance enforcement

	// Disk usage thresholds for auto-cleanup behavior
	DiskThresholdCleanupTrash float64 `yaml:"disk_threshold_cleanup_trash" json:"disk_threshold_cleanup_trash"` // % usage to trigger pre-run trash cleanup (default: 90)
	DiskThresholdBypassTrash  float64 `yaml:"disk_threshold_bypass_trash" json:"disk_threshold_bypass_trash"`   // % usage to bypass trash entirely (default: 95)
}

// MetricsConfig configures Prometheus metrics.
type MetricsConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Namespace string `yaml:"namespace" json:"namespace"`
}

// NotificationsConfig configures notification webhooks.
type NotificationsConfig struct {
	Webhooks []WebhookConfig `yaml:"webhooks,omitempty" json:"webhooks,omitempty"`
}

// WebhookConfig configures a single webhook endpoint.
type WebhookConfig struct {
	URL     string            `yaml:"url" json:"url"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Events  []string          `yaml:"events,omitempty" json:"events,omitempty"` // cleanup_started, cleanup_completed, cleanup_failed
	Timeout time.Duration     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// AuthConfig configures authentication for the HTTP API.
type AuthConfig struct {
	// Enabled enables authentication. When false, all endpoints are accessible without authentication.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// APIKeys configures API key authentication.
	APIKeys *APIKeyConfig `yaml:"api_keys,omitempty" json:"api_keys,omitempty"`
	// PublicPaths are paths that don't require authentication (e.g., /health).
	PublicPaths []string `yaml:"public_paths,omitempty" json:"public_paths,omitempty"`
}

// APIKeyConfig configures API key authentication.
type APIKeyConfig struct {
	// Enabled enables API key authentication.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Key is a single API key for simple setups. Hidden from /api/config endpoint.
	Key string `yaml:"key,omitempty" json:"-"`
	// KeyEnv is the name of an environment variable containing the API key.
	KeyEnv string `yaml:"key_env,omitempty" json:"key_env,omitempty"`
	// KeysFile is the path to a file containing multiple keys.
	KeysFile string `yaml:"keys_file,omitempty" json:"keys_file,omitempty"`
	// HeaderName is the header name for API key authentication (default: X-API-Key).
	HeaderName string `yaml:"header_name,omitempty" json:"header_name,omitempty"`
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	return &Config{
		Version: 1,
		Scan: ScanConfig{
			Roots:          []string{},
			Recursive:      true,
			MaxDepth:       0,
			FollowSymlinks: false,
			IncludeDirs:    false,
			IncludeFiles:   true,
		},
		Policy: PolicyConfig{
			MinAgeDays:    30,
			MinSizeMB:     0,
			Extensions:    []string{},
			Exclusions:    []string{},
			CompositeMode: "and",
		},
		Safety: SafetyConfig{
			ProtectedPaths: []string{
				"/boot", "/etc", "/usr", "/var",
				"/sys", "/proc", "/dev",
			},
			AllowDirDelete:       false,
			EnforceMountBoundary: false,
		},
		Execution: ExecutionConfig{
			Mode:               "dry-run",
			Timeout:            30 * time.Second,
			AuditPath:          "",
			MaxItems:           25,
			MaxDeletionsPerRun: 10000,              // Safety limit: stop after 10k deletions per run
			TrashPath:          "",                 // Empty = permanent delete (no soft-delete)
			TrashMaxAge:        7 * 24 * time.Hour, // 7 days default if trash is enabled
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
			Loki: &LokiConfig{
				Enabled:   false,
				URL:       "http://localhost:3100",
				BatchSize: 100,
				BatchWait: 5 * time.Second,
				Labels: map[string]string{
					"service": "storage-sage",
				},
				TenantID: "",
			},
		},
		Daemon: DaemonConfig{
			Enabled:                   false,
			HTTPAddr:                  "127.0.0.1:8080", // Localhost only by default for security
			MetricsAddr:               "127.0.0.1:9090", // Localhost only by default for security
			Schedule:                  "",
			TriggerTimeout:            30 * time.Minute,
			PIDFile:                   "",   // Empty = no PID file
			DiskThresholdCleanupTrash: 90.0, // Trigger trash cleanup at 90% disk usage
			DiskThresholdBypassTrash:  95.0, // Bypass trash entirely at 95% disk usage
		},
		Metrics: MetricsConfig{
			Enabled:   false,
			Namespace: "storage_sage",
		},
		Notifications: NotificationsConfig{
			Webhooks: []WebhookConfig{},
		},
		Auth: &AuthConfig{
			Enabled:     false, // Backwards compatible - disabled by default
			PublicPaths: []string{"/health"},
		},
	}
}

// Load reads a config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

// LoadOrDefault loads config from path if it exists, otherwise returns defaults.
func LoadOrDefault(path string) (*Config, error) {
	if path == "" {
		return Default(), nil
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Default(), nil
	}

	return Load(path)
}

// FindConfigFile searches for a config file in standard locations.
func FindConfigFile() string {
	candidates := []string{
		"storage-sage.yaml",
		"storage-sage.yml",
		filepath.Join(os.Getenv("HOME"), ".config", "storage-sage", "config.yaml"),
		"/etc/storage-sage/config.yaml",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// Save writes the config to the given path.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
