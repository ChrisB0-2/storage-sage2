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
	Version   int             `yaml:"version"`
	Scan      ScanConfig      `yaml:"scan"`
	Policy    PolicyConfig    `yaml:"policy"`
	Safety    SafetyConfig    `yaml:"safety"`
	Execution ExecutionConfig `yaml:"execution"`
	Logging   LoggingConfig   `yaml:"logging"`
	Daemon    DaemonConfig    `yaml:"daemon"`
	Metrics   MetricsConfig   `yaml:"metrics"`
}

// ScanConfig configures the filesystem scanning behavior.
type ScanConfig struct {
	Roots          []string `yaml:"roots"`
	Recursive      bool     `yaml:"recursive"`
	MaxDepth       int      `yaml:"max_depth"`
	FollowSymlinks bool     `yaml:"follow_symlinks"`
	IncludeDirs    bool     `yaml:"include_dirs"`
	IncludeFiles   bool     `yaml:"include_files"`
}

// PolicyConfig configures the file selection policy.
type PolicyConfig struct {
	MinAgeDays    int      `yaml:"min_age_days"`
	MinSizeMB     int      `yaml:"min_size_mb"`
	Extensions    []string `yaml:"extensions"`
	CompositeMode string   `yaml:"composite_mode"` // "and" or "or"
}

// SafetyConfig configures safety boundaries.
type SafetyConfig struct {
	ProtectedPaths       []string `yaml:"protected_paths"`
	AllowDirDelete       bool     `yaml:"allow_dir_delete"`
	EnforceMountBoundary bool     `yaml:"enforce_mount_boundary"`
}

// ExecutionConfig configures execution behavior.
type ExecutionConfig struct {
	Mode      string        `yaml:"mode"` // "dry-run" or "execute"
	Timeout   time.Duration `yaml:"timeout"`
	AuditPath string        `yaml:"audit_path"`
	MaxItems  int           `yaml:"max_items"`
}

// LoggingConfig configures logging behavior.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // "debug", "info", "warn", "error"
	Format string `yaml:"format"` // "json" or "text"
	Output string `yaml:"output"` // "stderr", "stdout", or file path
}

// DaemonConfig configures daemon mode.
type DaemonConfig struct {
	Enabled     bool   `yaml:"enabled"`
	HTTPAddr    string `yaml:"http_addr"`
	MetricsAddr string `yaml:"metrics_addr"`
	Schedule    string `yaml:"schedule"` // cron expression
}

// MetricsConfig configures Prometheus metrics.
type MetricsConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Namespace string `yaml:"namespace"`
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
			Mode:      "dry-run",
			Timeout:   30 * time.Second,
			AuditPath: "",
			MaxItems:  25,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		},
		Daemon: DaemonConfig{
			Enabled:     false,
			HTTPAddr:    ":8080",
			MetricsAddr: ":9090",
			Schedule:    "",
		},
		Metrics: MetricsConfig{
			Enabled:   false,
			Namespace: "storage_sage",
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
