package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/auditor"
	"github.com/ChrisB0-2/storage-sage/internal/auth"
	"github.com/ChrisB0-2/storage-sage/internal/config"
	"github.com/ChrisB0-2/storage-sage/internal/core"
	"github.com/ChrisB0-2/storage-sage/internal/daemon"
	"github.com/ChrisB0-2/storage-sage/internal/executor"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/metrics"
	"github.com/ChrisB0-2/storage-sage/internal/notifier"
	"github.com/ChrisB0-2/storage-sage/internal/planner"
	"github.com/ChrisB0-2/storage-sage/internal/policy"
	"github.com/ChrisB0-2/storage-sage/internal/safety"
	"github.com/ChrisB0-2/storage-sage/internal/scanner"
	"github.com/ChrisB0-2/storage-sage/internal/trash"
)

// version is set via ldflags at build time.
var version = "dev"

// CLI flags
var (
	showVersion    = flag.Bool("version", false, "print version and exit")
	configPath     = flag.String("config", "", "path to YAML configuration file")
	root           = flag.String("root", "", "root directory to scan")
	mode           = flag.String("mode", "", "mode: dry-run or execute")
	maxItems       = flag.Int("max", 0, "max plan items to print")
	maxDepth       = flag.Int("depth", -1, "max depth (-1 = use config default)")
	minAgeDays     = flag.Int("min-age-days", -1, "minimum age in days (-1 = use config default)")
	auditPath      = flag.String("audit", "", "audit log path (jsonl)")
	auditDBPath    = flag.String("audit-db", "", "audit database path (sqlite)")
	protectedPaths = flag.String("protected", "", "comma-separated additional protected paths")
	allowDirDelete = flag.Bool("allow-dir-delete", false, "allow deletion of directories")
	minSizeMB      = flag.Int("min-size-mb", -1, "minimum file size in MB (-1 = use config default)")
	extensions     = flag.String("extensions", "", "comma-separated extensions to match")
	exclusions     = flag.String("exclude", "", "comma-separated glob patterns to exclude (e.g., '*.important,keep-*')")
	enableMetrics  = flag.Bool("metrics", false, "enable Prometheus metrics endpoint")
	metricsAddr    = flag.String("metrics-addr", "", "metrics server address (default :9090)")
	maxDeletions   = flag.Int("max-deletions", -1, "max deletions per run (-1 = use config default, 0 = unlimited)")

	// Daemon mode flags
	daemonMode = flag.Bool("daemon", false, "run as long-running daemon")
	schedule   = flag.String("schedule", "", "run schedule (e.g., '1h', '30m', '@every 6h')")
	daemonAddr = flag.String("daemon-addr", "127.0.0.1:8080", "daemon HTTP address (use 0.0.0.0:8080 for external access)")
	pidFile    = flag.String("pid-file", "", "PID file path for single-instance enforcement")

	// Soft-delete flags
	trashPath = flag.String("trash-path", "", "move files to trash instead of permanent delete")

	// Loki flags
	enableLoki = flag.Bool("loki", false, "enable Loki log shipping")
	lokiURL    = flag.String("loki-url", "", "Loki server URL (default http://localhost:3100)")

	// Auth flags
	authEnabled = flag.Bool("auth", false, "enable API authentication")
	authKey     = flag.String("auth-key", "", "API key for authentication (format: ss_<32 hex chars>)")
)

func main() {
	// Check for subcommands before parsing flags
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			runInitCmd(os.Args[2:])
			return
		case "query":
			runQueryCmd(os.Args[2:])
			return
		case "stats":
			runStatsCmd(os.Args[2:])
			return
		case "verify":
			runVerifyCmd(os.Args[2:])
			return
		case "validate":
			runValidateCmd(os.Args[2:])
			return
		case "trash":
			runTrashCmd(os.Args[2:])
			return
		}
	}

	flag.Parse()

	if *showVersion {
		fmt.Println("storage-sage", version)
		return
	}

	// 1. Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to load config: %v\n", err)
		os.Exit(2)
	}

	// 2. Merge CLI flags over config values
	mergeFlags(cfg)

	// 3. Validate final configuration
	if err := config.ValidateFinal(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	// 4. Initialize logger from config
	log, lokiCleanup, err := initLogger(cfg.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if lokiCleanup != nil {
		defer lokiCleanup()
	}

	log.Info("storage-sage starting",
		logger.F("mode", cfg.Execution.Mode),
		logger.F("roots", cfg.Scan.Roots),
	)

	// 5. Check for daemon mode
	if *daemonMode {
		if err := runDaemon(cfg, log); err != nil {
			log.Error("daemon failed", logger.F("error", err.Error()))
			os.Exit(1)
		}
		return
	}

	// 6. Run main logic with logger-aware components (one-shot mode)
	if err := run(cfg, log); err != nil {
		log.Error("execution failed", logger.F("error", err.Error()))
		os.Exit(1)
	}
}

// runInitCmd handles the "init" subcommand for first-time setup.
func runInitCmd(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "overwrite existing configuration")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage init [options]\n\nInitialize storage-sage configuration for first-time use.\n\nThis command creates:\n")
		fmt.Fprintf(os.Stderr, "  - ~/.config/storage-sage/config.yaml (configuration file)\n")
		fmt.Fprintf(os.Stderr, "  - ~/.local/share/storage-sage/       (data directory for audit logs and trash)\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nAfter initialization, simply run:\n")
		fmt.Fprintf(os.Stderr, "  storage-sage -daemon\n")
	}

	_ = fs.Parse(args)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not determine home directory: %v\n", err)
		os.Exit(1)
	}

	configDir := filepath.Join(homeDir, ".config", "storage-sage")
	dataDir := filepath.Join(homeDir, ".local", "share", "storage-sage")
	configFile := filepath.Join(configDir, "config.yaml")
	trashDir := filepath.Join(dataDir, "trash")

	// Check if config already exists
	if !*force {
		if _, err := os.Stat(configFile); err == nil {
			fmt.Fprintf(os.Stderr, "Configuration already exists at %s\n", configFile)
			fmt.Fprintf(os.Stderr, "Use -force to overwrite.\n")
			os.Exit(1)
		}
	}

	// Create directories
	dirs := []string{configDir, dataDir, trashDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "error: could not create directory %s: %v\n", dir, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", dir)
	}

	// Write default configuration
	defaultConfig := fmt.Sprintf(`# Storage Sage Configuration
# Generated by: storage-sage init
# Location: %s
#
# Quick start:
#   storage-sage -daemon          # Start with web UI at http://localhost:8080
#   storage-sage -root /tmp       # One-shot dry-run scan
#
# Change mode to "execute" when ready to actually delete files.

version: 1

scan:
  roots:
    - /tmp
    - /var/tmp
  recursive: true
  max_depth: 0
  include_files: true
  include_dirs: false

policy:
  min_age_days: 7
  min_size_mb: 0
  extensions: []
  exclusions:
    - ".gitkeep"
    - "*.socket"
    - "*.sock"
    - "*.lock"
    - "*.pid"

safety:
  protected_paths:
    - /boot
    - /etc
    - /usr
    - /var
    - /sys
    - /proc
    - /dev
    - /home
    - /root
  allow_dir_delete: false
  enforce_mount_boundary: false

execution:
  mode: dry-run
  timeout: 5m
  max_items: 50
  audit_db_path: %s/audit.db
  trash_path: %s
  trash_max_age: 168h

logging:
  level: info
  format: json
  output: stderr

daemon:
  enabled: true
  http_addr: ":8080"
  schedule: "6h"
  trigger_timeout: 30m

metrics:
  enabled: true
  namespace: storage_sage
`, configFile, dataDir, trashDir)

	if err := os.WriteFile(configFile, []byte(defaultConfig), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not write config file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created: %s\n", configFile)

	fmt.Println()
	fmt.Println("Storage-sage initialized successfully!")
	fmt.Println()
	fmt.Println("Configuration: " + configFile)
	fmt.Println("Audit database: " + filepath.Join(dataDir, "audit.db"))
	fmt.Println("Trash directory: " + trashDir)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review/edit the config: " + configFile)
	fmt.Println("  2. Start the daemon:       storage-sage -daemon")
	fmt.Println("  3. Open the web UI:        http://localhost:8080")
	fmt.Println()
	fmt.Println("The default mode is 'dry-run' (no files deleted).")
	fmt.Println("Change execution.mode to 'execute' when ready.")
}

// runQueryCmd handles the "query" subcommand for reviewing audit logs.
func runQueryCmd(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	dbPath := fs.String("db", "", "audit database path (required)")
	since := fs.String("since", "", "show records since (e.g., '24h', '7d', '2024-01-01')")
	until := fs.String("until", "", "show records until (e.g., 'now', '2024-01-15')")
	action := fs.String("action", "", "filter by action (plan, delete, error)")
	level := fs.String("level", "", "filter by level (info, warn, error)")
	path := fs.String("path", "", "filter by path (partial match)")
	limit := fs.Int("limit", 100, "max records to return")
	jsonOut := fs.Bool("json", false, "output as JSON")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage query [options]\n\nQuery audit database for log review.\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  storage-sage query -db audit.db -since 24h\n")
		fmt.Fprintf(os.Stderr, "  storage-sage query -db audit.db -action delete -limit 50\n")
		fmt.Fprintf(os.Stderr, "  storage-sage query -db audit.db -level error -json\n")
	}

	_ = fs.Parse(args)

	if *dbPath == "" {
		fmt.Fprintf(os.Stderr, "error: -db is required\n")
		fs.Usage()
		os.Exit(2)
	}

	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer sqlAud.Close()

	filter := auditor.QueryFilter{
		Action: *action,
		Level:  *level,
		Path:   *path,
		Limit:  *limit,
	}

	if *since != "" {
		filter.Since = parseTimeArg(*since)
	}
	if *until != "" {
		filter.Until = parseTimeArg(*until)
	}

	records, err := sqlAud.Query(context.Background(), filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(records); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to encode JSON: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Found %d records:\n\n", len(records))
		for _, r := range records {
			fmt.Printf("[%s] %s %s", r.Timestamp.Format("2006-01-02 15:04:05"), r.Level, r.Action)
			if r.Path != "" {
				fmt.Printf(" %s", r.Path)
			}
			if r.BytesFreed > 0 {
				fmt.Printf(" (%s freed)", formatBytesHuman(r.BytesFreed))
			}
			if r.Error != "" {
				fmt.Printf(" ERROR: %s", r.Error)
			}
			fmt.Println()
		}
	}
}

// runStatsCmd handles the "stats" subcommand for audit statistics.
func runStatsCmd(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbPath := fs.String("db", "", "audit database path (required)")
	jsonOut := fs.Bool("json", false, "output as JSON")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage stats [options]\n\nShow audit database statistics.\n\nOptions:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(args)

	if *dbPath == "" {
		fmt.Fprintf(os.Stderr, "error: -db is required\n")
		fs.Usage()
		os.Exit(2)
	}

	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer sqlAud.Close()

	stats, err := sqlAud.Stats(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: stats failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(stats); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to encode JSON: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("Audit Database Statistics")
		fmt.Println("=========================")
		fmt.Printf("Total Records:     %d\n", stats.TotalRecords)
		fmt.Printf("First Record:      %s\n", stats.FirstRecord.Format("2006-01-02 15:04:05"))
		fmt.Printf("Last Record:       %s\n", stats.LastRecord.Format("2006-01-02 15:04:05"))
		fmt.Printf("Files Deleted:     %d\n", stats.FilesDeleted)
		fmt.Printf("Total Bytes Freed: %s\n", formatBytesHuman(stats.TotalBytesFreed))
		fmt.Printf("Errors:            %d\n", stats.Errors)
	}
}

// runVerifyCmd handles the "verify" subcommand for integrity checking.
func runVerifyCmd(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dbPath := fs.String("db", "", "audit database path (required)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage verify [options]\n\nVerify audit database integrity (detect tampering).\n\nOptions:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(args)

	if *dbPath == "" {
		fmt.Fprintf(os.Stderr, "error: -db is required\n")
		fs.Usage()
		os.Exit(2)
	}

	sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer sqlAud.Close()

	tampered, err := sqlAud.VerifyIntegrity(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: verification failed: %v\n", err)
		os.Exit(1)
	}

	if len(tampered) == 0 {
		fmt.Println("PASS: All records verified. No tampering detected.")
	} else {
		fmt.Printf("FAIL: %d records have invalid checksums (possible tampering):\n", len(tampered))
		for _, id := range tampered {
			fmt.Printf("  - Record ID: %d\n", id)
		}
		os.Exit(1)
	}
}

// runValidateCmd handles the "validate" subcommand for config validation.
func runValidateCmd(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configFile := fs.String("config", "", "path to configuration file (required)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage validate [options]\n\nValidate a configuration file without running cleanup.\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  storage-sage validate -config /etc/storage-sage/config.yaml\n")
		fmt.Fprintf(os.Stderr, "  storage-sage validate -config ./config.yaml\n")
	}

	_ = fs.Parse(args)

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "error: -config is required\n")
		fs.Usage()
		os.Exit(2)
	}

	// Load the configuration file
	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Validate the configuration
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v", err)
		os.Exit(1)
	}

	fmt.Printf("OK: configuration file %q is valid\n", *configFile)
	fmt.Printf("\nConfiguration summary:\n")
	fmt.Printf("  Roots:         %v\n", cfg.Scan.Roots)
	fmt.Printf("  Mode:          %s\n", cfg.Execution.Mode)
	fmt.Printf("  Min age:       %d days\n", cfg.Policy.MinAgeDays)
	if cfg.Policy.MinSizeMB > 0 {
		fmt.Printf("  Min size:      %d MB\n", cfg.Policy.MinSizeMB)
	}
	if len(cfg.Policy.Extensions) > 0 {
		fmt.Printf("  Extensions:    %v\n", cfg.Policy.Extensions)
	}
	if len(cfg.Policy.Exclusions) > 0 {
		fmt.Printf("  Exclusions:    %v\n", cfg.Policy.Exclusions)
	}
	if cfg.Daemon.Enabled {
		fmt.Printf("  Daemon:        enabled (schedule: %s)\n", cfg.Daemon.Schedule)
	}
	if cfg.Metrics.Enabled {
		fmt.Printf("  Metrics:       enabled\n")
	}
	if cfg.Auth != nil && cfg.Auth.Enabled {
		fmt.Printf("  Auth:          enabled\n")
	}
}

// runTrashCmd handles the "trash" subcommand for managing soft-deleted files.
func runTrashCmd(args []string) {
	if len(args) == 0 {
		printTrashUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "list":
		runTrashList(args[1:])
	case "restore":
		runTrashRestore(args[1:])
	case "empty":
		runTrashEmpty(args[1:])
	case "help", "-h", "--help":
		printTrashUsage()
	default:
		fmt.Fprintf(os.Stderr, "error: unknown trash subcommand: %s\n", args[0])
		printTrashUsage()
		os.Exit(2)
	}
}

func printTrashUsage() {
	fmt.Fprintf(os.Stderr, `Usage: storage-sage trash <command> [options]

Manage soft-deleted files in the trash directory.

Commands:
  list      List all items in trash
  restore   Restore an item from trash to its original location
  empty     Permanently delete items from trash

Examples:
  storage-sage trash list -path /var/lib/storage-sage/trash
  storage-sage trash restore -path /var/lib/storage-sage/trash -item <trash-name>
  storage-sage trash empty -path /var/lib/storage-sage/trash -older-than 7d

Run 'storage-sage trash <command> -h' for more information on a command.
`)
}

// runTrashList lists all items currently in trash.
func runTrashList(args []string) {
	fs := flag.NewFlagSet("trash list", flag.ExitOnError)
	trashDir := fs.String("path", "", "trash directory path (required, or set in config)")
	configFile := fs.String("config", "", "path to config file (to read trash path)")
	jsonOut := fs.Bool("json", false, "output as JSON")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage trash list [options]\n\nList all items in the trash directory.\n\nOptions:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(args)

	path := resolveTrashPath(*trashDir, *configFile)
	if path == "" {
		fmt.Fprintf(os.Stderr, "error: trash path required (use -path or configure execution.trash_path)\n")
		fs.Usage()
		os.Exit(2)
	}

	mgr, err := trash.New(trash.Config{TrashPath: path}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open trash: %v\n", err)
		os.Exit(1)
	}

	items, err := mgr.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to list trash: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to encode JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(items) == 0 {
		fmt.Println("Trash is empty.")
		return
	}

	fmt.Printf("Trash directory: %s\n", path)
	fmt.Printf("Items: %d\n\n", len(items))

	// Calculate total size
	var totalSize int64
	for _, item := range items {
		totalSize += item.Size
	}
	fmt.Printf("Total size: %s\n\n", formatBytesHuman(totalSize))

	// Print header
	fmt.Printf("%-40s  %-10s  %-20s  %s\n", "NAME", "SIZE", "TRASHED AT", "ORIGINAL PATH")
	fmt.Printf("%s\n", strings.Repeat("-", 100))

	for _, item := range items {
		name := item.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}

		typeIndicator := ""
		if item.IsDir {
			typeIndicator = "/"
		}

		fmt.Printf("%-40s  %-10s  %-20s  %s%s\n",
			name+typeIndicator,
			formatBytesHuman(item.Size),
			item.TrashedAt.Format("2006-01-02 15:04:05"),
			item.OriginalPath,
			"",
		)
	}
}

// runTrashRestore restores an item from trash.
func runTrashRestore(args []string) {
	fs := flag.NewFlagSet("trash restore", flag.ExitOnError)
	trashDir := fs.String("path", "", "trash directory path (required, or set in config)")
	configFile := fs.String("config", "", "path to config file (to read trash path)")
	itemName := fs.String("item", "", "name of the item in trash to restore (required)")
	force := fs.Bool("force", false, "overwrite if destination exists")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage trash restore [options]\n\nRestore an item from trash to its original location.\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  storage-sage trash restore -path /var/lib/storage-sage/trash -item 20240115-103000_abc12345_file.txt\n")
	}

	_ = fs.Parse(args)

	path := resolveTrashPath(*trashDir, *configFile)
	if path == "" {
		fmt.Fprintf(os.Stderr, "error: trash path required (use -path or configure execution.trash_path)\n")
		fs.Usage()
		os.Exit(2)
	}

	if *itemName == "" {
		fmt.Fprintf(os.Stderr, "error: -item is required\n")
		fs.Usage()
		os.Exit(2)
	}

	mgr, err := trash.New(trash.Config{TrashPath: path}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open trash: %v\n", err)
		os.Exit(1)
	}

	// Find the item
	items, err := mgr.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to list trash: %v\n", err)
		os.Exit(1)
	}

	var targetItem *trash.TrashItem
	for i := range items {
		if items[i].Name == *itemName {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		fmt.Fprintf(os.Stderr, "error: item not found in trash: %s\n", *itemName)
		fmt.Fprintf(os.Stderr, "\nUse 'storage-sage trash list -path %s' to see available items.\n", path)
		os.Exit(1)
	}

	// Check if destination exists
	if !*force {
		if _, err := os.Stat(targetItem.OriginalPath); err == nil {
			fmt.Fprintf(os.Stderr, "error: destination already exists: %s\n", targetItem.OriginalPath)
			fmt.Fprintf(os.Stderr, "Use -force to overwrite.\n")
			os.Exit(1)
		}
	} else {
		// Remove existing destination if force is set
		if _, err := os.Stat(targetItem.OriginalPath); err == nil {
			if err := os.RemoveAll(targetItem.OriginalPath); err != nil {
				fmt.Fprintf(os.Stderr, "error: failed to remove existing destination: %v\n", err)
				os.Exit(1)
			}
		}
	}

	originalPath, err := mgr.Restore(targetItem.TrashPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: restore failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Restored: %s -> %s\n", *itemName, originalPath)
}

// trashEmptyOptions holds parsed options for trash empty command.
type trashEmptyOptions struct {
	path      string
	maxAge    time.Duration
	all       bool
	dryRun    bool
	force     bool
	olderThan string
}

// runTrashEmpty permanently deletes items from trash.
func runTrashEmpty(args []string) {
	opts := parseTrashEmptyFlags(args)

	mgr, err := trash.New(trash.Config{
		TrashPath: opts.path,
		MaxAge:    opts.maxAge,
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open trash: %v\n", err)
		os.Exit(1)
	}

	items, err := mgr.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to list trash: %v\n", err)
		os.Exit(1)
	}

	if len(items) == 0 {
		fmt.Println("Trash is already empty.")
		return
	}

	toDelete, totalBytes := filterTrashItems(items, opts)
	if len(toDelete) == 0 {
		fmt.Printf("No items older than %s found in trash.\n", opts.olderThan)
		return
	}

	fmt.Printf("Items to delete: %d\n", len(toDelete))
	fmt.Printf("Space to free: %s\n\n", formatBytesHuman(totalBytes))

	if opts.dryRun {
		printTrashDryRun(toDelete)
		return
	}

	if !opts.force && !confirmTrashEmpty(len(toDelete), totalBytes) {
		fmt.Println("Aborted.")
		return
	}

	executeTrashEmpty(mgr, toDelete, opts.all)
}

// parseTrashEmptyFlags parses and validates flags for trash empty command.
func parseTrashEmptyFlags(args []string) trashEmptyOptions {
	fs := flag.NewFlagSet("trash empty", flag.ExitOnError)
	trashDir := fs.String("path", "", "trash directory path (required, or set in config)")
	configFile := fs.String("config", "", "path to config file (to read trash path)")
	olderThan := fs.String("older-than", "", "only delete items older than this (e.g., '7d', '24h')")
	all := fs.Bool("all", false, "delete ALL items (ignores -older-than)")
	dryRun := fs.Bool("dry-run", false, "show what would be deleted without actually deleting")
	force := fs.Bool("force", false, "skip confirmation prompt")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: storage-sage trash empty [options]\n\nPermanently delete items from trash.\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  storage-sage trash empty -path /var/lib/storage-sage/trash -older-than 7d\n")
		fmt.Fprintf(os.Stderr, "  storage-sage trash empty -path /var/lib/storage-sage/trash -all -force\n")
		fmt.Fprintf(os.Stderr, "  storage-sage trash empty -path /var/lib/storage-sage/trash -all -dry-run\n")
	}

	_ = fs.Parse(args)

	path := resolveTrashPath(*trashDir, *configFile)
	if path == "" {
		fmt.Fprintf(os.Stderr, "error: trash path required (use -path or configure execution.trash_path)\n")
		fs.Usage()
		os.Exit(2)
	}

	if !*all && *olderThan == "" {
		fmt.Fprintf(os.Stderr, "error: must specify -older-than or -all\n")
		fs.Usage()
		os.Exit(2)
	}

	var maxAge time.Duration
	if *olderThan != "" {
		maxAge = parseAgeDuration(*olderThan)
		if maxAge == 0 {
			fmt.Fprintf(os.Stderr, "error: invalid -older-than format: %s (use e.g., '7d', '24h', '30m')\n", *olderThan)
			os.Exit(2)
		}
	}

	return trashEmptyOptions{
		path:      path,
		maxAge:    maxAge,
		all:       *all,
		dryRun:    *dryRun,
		force:     *force,
		olderThan: *olderThan,
	}
}

// filterTrashItems filters items based on age or all flag.
func filterTrashItems(items []trash.TrashItem, opts trashEmptyOptions) ([]trash.TrashItem, int64) {
	cutoff := time.Now().Add(-opts.maxAge)
	var toDelete []trash.TrashItem
	var totalBytes int64

	for _, item := range items {
		if opts.all || item.TrashedAt.Before(cutoff) {
			toDelete = append(toDelete, item)
			totalBytes += item.Size
		}
	}
	return toDelete, totalBytes
}

// printTrashDryRun prints what would be deleted in dry-run mode.
func printTrashDryRun(items []trash.TrashItem) {
	fmt.Println("Items that would be deleted:")
	for _, item := range items {
		age := time.Since(item.TrashedAt).Round(time.Hour)
		fmt.Printf("  - %s (age: %s, size: %s)\n", item.Name, age, formatBytesHuman(item.Size))
	}
	fmt.Println("\n(dry-run mode, nothing was deleted)")
}

// confirmTrashEmpty prompts user for confirmation.
func confirmTrashEmpty(count int, totalBytes int64) bool {
	fmt.Printf("This will permanently delete %d items (%s). Continue? [y/N] ", count, formatBytesHuman(totalBytes))
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y" || response == "yes"
}

// executeTrashEmpty performs the actual deletion.
func executeTrashEmpty(mgr *trash.Manager, toDelete []trash.TrashItem, deleteAll bool) {
	if deleteAll {
		// Delete everything manually since Cleanup() respects maxAge
		var deletedCount int
		var freedBytes int64

		for _, item := range toDelete {
			if err := os.RemoveAll(item.TrashPath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete %s: %v\n", item.Name, err)
				continue
			}
			_ = os.Remove(item.TrashPath + ".meta")
			deletedCount++
			freedBytes += item.Size
		}

		fmt.Printf("Deleted: %d items\n", deletedCount)
		fmt.Printf("Freed: %s\n", formatBytesHuman(freedBytes))
	} else {
		count, bytesFreed, err := mgr.Cleanup(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cleanup failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Deleted: %d items\n", count)
		fmt.Printf("Freed: %s\n", formatBytesHuman(bytesFreed))
	}
}

// resolveTrashPath determines the trash path from flag or config.
func resolveTrashPath(flagPath, configFile string) string {
	if flagPath != "" {
		return flagPath
	}

	// Try to load config
	cfgPath := configFile
	if cfgPath == "" {
		cfgPath = config.FindConfigFile()
	}

	if cfgPath != "" {
		cfg, err := config.Load(cfgPath)
		if err == nil && cfg.Execution.TrashPath != "" {
			return cfg.Execution.TrashPath
		}
	}

	return ""
}

// parseAgeDuration parses age strings like "7d", "24h", "30m"
func parseAgeDuration(s string) time.Duration {
	if len(s) < 2 {
		return 0
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]

	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil || n <= 0 {
		return 0
	}

	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour
	case 'h':
		return time.Duration(n) * time.Hour
	case 'm':
		return time.Duration(n) * time.Minute
	default:
		return 0
	}
}

// parseTimeArg parses a time argument like "24h", "7d", or "2024-01-01"
func parseTimeArg(s string) time.Time {
	// Try duration format first (e.g., "24h", "7d")
	if len(s) > 1 {
		unit := s[len(s)-1]
		numStr := s[:len(s)-1]
		var multiplier time.Duration
		switch unit {
		case 'h':
			multiplier = time.Hour
		case 'd':
			multiplier = 24 * time.Hour
		case 'm':
			multiplier = time.Minute
		}
		if multiplier > 0 {
			var n int
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > 0 {
				return time.Now().Add(-time.Duration(n) * multiplier)
			}
		}
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}

	// Try date format
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}

	return time.Time{}
}

// formatBytesHuman formats bytes in human-readable format
func formatBytesHuman(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// runDaemon starts storage-sage in daemon mode.
func runDaemon(cfg *config.Config, log logger.Logger) error {
	// Get schedule from flag or config
	sched := *schedule
	if sched == "" {
		sched = cfg.Daemon.Schedule
	}
	if sched == "" {
		return fmt.Errorf("daemon mode requires -schedule flag or daemon.schedule in config")
	}

	// Get HTTP address from flag (already has default)
	addr := *daemonAddr

	log.Info("starting daemon mode",
		logger.F("schedule", sched),
		logger.F("http_addr", addr),
	)

	// Initialize metrics (Prometheus or Noop) - persistent for daemon lifetime
	var m core.Metrics
	var metricsServer *metrics.Server
	if cfg.Metrics.Enabled {
		m = metrics.NewPrometheus(nil)
		metricsServer = metrics.NewServer(cfg.Daemon.MetricsAddr)

		// Start metrics server in background (runs for daemon lifetime)
		go func() {
			log.Info("metrics server starting", logger.F("addr", metricsServer.Addr()))
			if err := metricsServer.Start(); err != nil {
				log.Error("metrics server error", logger.F("error", err.Error()))
			}
		}()

		// Shutdown metrics server when daemon exits
		defer func() {
			log.Info("metrics server stopping")
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				log.Warn("metrics server shutdown error", logger.F("error", err.Error()))
			}
		}()
	} else {
		m = metrics.NewNoop()
	}

	// Initialize webhook notifier
	notify := createNotifier(cfg.Notifications, log)

	// Initialize SQLite auditor for API endpoints (query/stats)
	// This is separate from the per-run auditor in runCore, used for reading audit data
	var sqlAud *auditor.SQLiteAuditor
	if cfg.Execution.AuditDBPath != "" {
		var err error
		sqlAud, err = auditor.NewSQLite(auditor.SQLiteConfig{
			Path: cfg.Execution.AuditDBPath,
		})
		if err != nil {
			log.Warn("failed to initialize audit DB for API", logger.F("error", err.Error()))
		} else {
			log.Info("audit API enabled", logger.F("path", cfg.Execution.AuditDBPath))
			defer func() {
				if err := sqlAud.Close(); err != nil {
					log.Warn("audit DB close error", logger.F("error", err.Error()))
				}
			}()
		}
	}

	// Create the run function that executes a single cleanup cycle
	// Uses shared metrics instance for persistent metrics
	// Wraps with webhook notifications
	runFunc := func(ctx context.Context) error {
		startTime := time.Now()
		rootStr := ""
		if len(cfg.Scan.Roots) > 0 {
			rootStr = cfg.Scan.Roots[0]
		}

		// Notify cleanup started (fire-and-forget)
		_ = notify.Notify(ctx, notifier.WebhookPayload{
			Event:     notifier.EventCleanupStarted,
			Timestamp: startTime,
			Message:   fmt.Sprintf("Cleanup started for %s", rootStr),
		})

		// Run cleanup
		err := runCore(cfg, log, m)

		// Build summary and notify
		duration := time.Since(startTime)
		payload := notifier.WebhookPayload{
			Timestamp: time.Now(),
			Summary: &notifier.CleanupSummary{
				Root:        rootStr,
				Mode:        cfg.Execution.Mode,
				Duration:    duration.Round(time.Second).String(),
				StartedAt:   startTime,
				CompletedAt: time.Now(),
			},
		}

		if err != nil {
			payload.Event = notifier.EventCleanupFailed
			payload.Message = fmt.Sprintf("Cleanup failed: %v", err)
			payload.Summary.ErrorMessages = []string{err.Error()}
			payload.Summary.Errors = 1
		} else {
			payload.Event = notifier.EventCleanupCompleted
			payload.Message = "Cleanup completed successfully"
			// Record successful run timestamp for metrics
			m.SetLastRunTimestamp(time.Now())
		}

		_ = notify.Notify(ctx, payload)

		return err
	}

	// Initialize auth middleware if enabled
	var authMW *auth.Middleware
	var rbacMW *auth.RBACMiddleware

	if cfg.Auth != nil && cfg.Auth.Enabled {
		authenticators := []auth.Authenticator{}

		if cfg.Auth.APIKeys != nil && cfg.Auth.APIKeys.Enabled {
			apiKeyAuth, err := auth.NewAPIKeyAuthenticator(auth.APIKeyConfig{
				Enabled:    cfg.Auth.APIKeys.Enabled,
				Key:        cfg.Auth.APIKeys.Key,
				KeyEnv:     cfg.Auth.APIKeys.KeyEnv,
				KeysFile:   cfg.Auth.APIKeys.KeysFile,
				HeaderName: cfg.Auth.APIKeys.HeaderName,
			}, log)
			if err != nil {
				return fmt.Errorf("auth setup failed: %w", err)
			}
			authenticators = append(authenticators, apiKeyAuth)
		}

		if len(authenticators) > 0 {
			publicPaths := cfg.Auth.PublicPaths
			if publicPaths == nil {
				publicPaths = []string{"/health"}
			}
			authMW = auth.NewMiddleware(log, authenticators, publicPaths)
			rbacMW = auth.NewRBACMiddleware(auth.DefaultPermissions(), log)
			log.Info("authentication enabled", logger.F("methods", len(authenticators)))
		}
	}

	// Initialize trash manager for API endpoints
	var trashMgr *trash.Manager
	if cfg.Execution.TrashPath != "" {
		var err error
		trashMgr, err = trash.New(trash.Config{
			TrashPath: cfg.Execution.TrashPath,
			MaxAge:    cfg.Execution.TrashMaxAge,
		}, log)
		if err != nil {
			log.Warn("failed to initialize trash manager for API", logger.F("error", err.Error()))
		} else {
			log.Info("trash API enabled", logger.F("path", cfg.Execution.TrashPath))
		}
	}

	// Create and run daemon with config and auditor for API endpoints
	d := daemon.New(log, runFunc, daemon.Config{
		Schedule:       sched,
		HTTPAddr:       addr,
		TriggerTimeout: cfg.Daemon.TriggerTimeout,
		PIDFile:        cfg.Daemon.PIDFile,
		AppConfig:      cfg,
		Auditor:        sqlAud,
		Trash:          trashMgr,
		AuthMiddleware: authMW,
		RBACMiddleware: rbacMW,
	})

	return d.Run(context.Background())
}

// loadConfig loads configuration from file or returns defaults.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		// Try to find config in standard locations
		path = config.FindConfigFile()
	}

	cfg, err := config.LoadOrDefault(path)
	if err != nil {
		return nil, err
	}

	// Validate loaded config (but not final - CLI may fix issues)
	if path != "" {
		if err := config.Validate(cfg); err != nil {
			return nil, fmt.Errorf("invalid config file: %w", err)
		}
	}

	return cfg, nil
}

// mergeFlags applies CLI flag values over config values.
// CLI flags take precedence (only if explicitly set).
//
//nolint:gocyclo // Flag merging is repetitive but straightforward; splitting would obscure logic
func mergeFlags(cfg *config.Config) {
	// Helper to check if a flag was explicitly set
	flagSet := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		flagSet[f.Name] = true
	})

	// Merge root (-root overrides/replaces scan.roots)
	if flagSet["root"] && *root != "" {
		cfg.Scan.Roots = []string{filepath.Clean(*root)}
	}

	// Merge mode
	if flagSet["mode"] && *mode != "" {
		cfg.Execution.Mode = *mode
	}

	// Merge max-items
	if flagSet["max"] && *maxItems > 0 {
		cfg.Execution.MaxItems = *maxItems
	}

	// Merge max-deletions
	if flagSet["max-deletions"] && *maxDeletions >= 0 {
		cfg.Execution.MaxDeletionsPerRun = *maxDeletions
	}

	// Merge depth
	if flagSet["depth"] && *maxDepth >= 0 {
		cfg.Scan.MaxDepth = *maxDepth
	}

	// Merge min-age-days
	if flagSet["min-age-days"] && *minAgeDays >= 0 {
		cfg.Policy.MinAgeDays = *minAgeDays
	}

	// Merge min-size-mb
	if flagSet["min-size-mb"] && *minSizeMB >= 0 {
		cfg.Policy.MinSizeMB = *minSizeMB
	}

	// Merge audit path
	if flagSet["audit"] {
		cfg.Execution.AuditPath = *auditPath
	}
	if flagSet["audit-db"] {
		cfg.Execution.AuditDBPath = *auditDBPath
	}

	// Merge protected paths (append, don't replace)
	if flagSet["protected"] && *protectedPaths != "" {
		for _, p := range strings.Split(*protectedPaths, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.Safety.ProtectedPaths = append(cfg.Safety.ProtectedPaths, p)
			}
		}
	}

	// Merge allow-dir-delete
	if flagSet["allow-dir-delete"] {
		cfg.Safety.AllowDirDelete = *allowDirDelete
	}

	// Merge extensions
	if flagSet["extensions"] && *extensions != "" {
		var exts []string
		for _, e := range strings.Split(*extensions, ",") {
			if e = strings.TrimSpace(e); e != "" {
				if !strings.HasPrefix(e, ".") {
					e = "." + e
				}
				exts = append(exts, e)
			}
		}
		cfg.Policy.Extensions = exts
	}

	// Merge exclusions
	if flagSet["exclude"] && *exclusions != "" {
		var excl []string
		for _, e := range strings.Split(*exclusions, ",") {
			if e = strings.TrimSpace(e); e != "" {
				excl = append(excl, e)
			}
		}
		cfg.Policy.Exclusions = excl
	}

	// Merge metrics flags
	if flagSet["metrics"] {
		cfg.Metrics.Enabled = *enableMetrics
	}
	if flagSet["metrics-addr"] && *metricsAddr != "" {
		cfg.Daemon.MetricsAddr = *metricsAddr
	}

	// Merge Loki flags
	if flagSet["loki"] {
		if cfg.Logging.Loki == nil {
			cfg.Logging.Loki = &config.LokiConfig{}
		}
		cfg.Logging.Loki.Enabled = *enableLoki
	}
	if flagSet["loki-url"] && *lokiURL != "" {
		if cfg.Logging.Loki == nil {
			cfg.Logging.Loki = &config.LokiConfig{}
		}
		cfg.Logging.Loki.URL = *lokiURL
	}

	// Merge auth flags
	if flagSet["auth"] {
		if cfg.Auth == nil {
			cfg.Auth = &config.AuthConfig{}
		}
		cfg.Auth.Enabled = *authEnabled
	}
	if flagSet["auth-key"] && *authKey != "" {
		if cfg.Auth == nil {
			cfg.Auth = &config.AuthConfig{}
		}
		cfg.Auth.Enabled = true
		if cfg.Auth.APIKeys == nil {
			cfg.Auth.APIKeys = &config.APIKeyConfig{Enabled: true}
		}
		cfg.Auth.APIKeys.Enabled = true
		cfg.Auth.APIKeys.Key = *authKey
	}

	// Merge PID file flag
	if flagSet["pid-file"] && *pidFile != "" {
		cfg.Daemon.PIDFile = *pidFile
	}

	// Merge trash path flag
	if flagSet["trash-path"] && *trashPath != "" {
		cfg.Execution.TrashPath = *trashPath
	}
}

// initLogger creates a logger based on configuration.
// Returns the logger and an optional cleanup function for Loki.
func initLogger(cfg config.LoggingConfig) (logger.Logger, func(), error) {
	level, err := logger.ParseLevel(cfg.Level)
	if err != nil {
		level = logger.LevelInfo
	}

	var output io.Writer
	switch cfg.Output {
	case "", "stderr":
		output = os.Stderr
	case "stdout":
		output = os.Stdout
	default:
		// File output
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = f
	}

	baseLog := logger.New(level, output)

	// Wrap with Loki if enabled
	if cfg.Loki != nil && cfg.Loki.Enabled {
		lokiCfg := logger.LokiConfig{
			URL:       cfg.Loki.URL,
			BatchSize: cfg.Loki.BatchSize,
			BatchWait: cfg.Loki.BatchWait,
			Labels:    cfg.Loki.Labels,
			TenantID:  cfg.Loki.TenantID,
		}
		lokiLog := logger.NewLokiLogger(baseLog, lokiCfg)

		cleanup := func() {
			if err := lokiLog.Close(); err != nil {
				baseLog.Warn("loki shutdown error", logger.F("error", err.Error()))
			}
		}

		return lokiLog, cleanup, nil
	}

	return baseLog, nil, nil
}

// run executes storage-sage in one-shot mode (manages its own metrics lifecycle).
func run(cfg *config.Config, log logger.Logger) error {
	// Initialize metrics (Prometheus or Noop)
	var m core.Metrics
	var metricsServer *metrics.Server
	if cfg.Metrics.Enabled {
		m = metrics.NewPrometheus(nil)
		metricsServer = metrics.NewServer(cfg.Daemon.MetricsAddr)

		// Start metrics server in background
		go func() {
			log.Info("metrics server starting", logger.F("addr", metricsServer.Addr()))
			if err := metricsServer.Start(); err != nil {
				log.Error("metrics server error", logger.F("error", err.Error()))
			}
		}()

		// Shutdown metrics server when done
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				log.Warn("metrics server shutdown error", logger.F("error", err.Error()))
			}
		}()
	} else {
		m = metrics.NewNoop()
	}

	return runCore(cfg, log, m)
}

// runCore executes the main storage-sage cleanup logic with provided metrics.
//
//nolint:gocyclo // Main orchestration function; complexity reflects feature breadth
func runCore(cfg *config.Config, log logger.Logger, m core.Metrics) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Execution.Timeout)
	defer cancel()

	runMode := core.Mode(cfg.Execution.Mode)

	// Auditor (optional) - supports both JSONL and SQLite
	var aud core.Auditor
	var auditors []core.Auditor

	// JSONL auditor
	if cfg.Execution.AuditPath != "" {
		a, aerr := auditor.NewJSONL(cfg.Execution.AuditPath)
		if aerr != nil {
			return fmt.Errorf("audit jsonl init failed: %w", aerr)
		}
		auditors = append(auditors, a)
		defer func() {
			if err := a.Err(); err != nil {
				log.Warn("audit write error", logger.F("error", err.Error()))
			}
			_ = a.Close()
		}()
	}

	// SQLite auditor (for long-term storage)
	if cfg.Execution.AuditDBPath != "" {
		sqlAud, err := auditor.NewSQLite(auditor.SQLiteConfig{
			Path: cfg.Execution.AuditDBPath,
		})
		if err != nil {
			return fmt.Errorf("audit sqlite init failed: %w", err)
		}
		auditors = append(auditors, sqlAud)
		log.Info("sqlite audit enabled", logger.F("path", cfg.Execution.AuditDBPath))
		defer func() {
			if err := sqlAud.Close(); err != nil {
				log.Warn("audit db close error", logger.F("error", err.Error()))
			}
		}()
	}

	// Combine auditors if multiple configured
	if len(auditors) == 1 {
		aud = auditors[0]
	} else if len(auditors) > 1 {
		aud = auditor.NewMulti(auditors...)
	}

	// Components with logger and metrics injection
	sc := scanner.NewWalkDirWithMetrics(log, m)
	pl := planner.NewSimpleWithMetrics(log, m)
	safe := safety.NewWithLogger(log)

	// Build policy from config
	pol := buildPolicy(cfg.Policy, log)

	// Environment snapshot
	env := core.EnvSnapshot{
		Now:         time.Now(),
		DiskUsedPct: 0,
		CPUUsedPct:  0,
	}

	// Safety config
	safetyCfg := core.SafetyConfig{
		AllowedRoots:         cfg.Scan.Roots,
		ProtectedPaths:       cfg.Safety.ProtectedPaths,
		AllowDirDelete:       cfg.Safety.AllowDirDelete,
		EnforceMountBoundary: cfg.Safety.EnforceMountBoundary,
	}

	req := core.ScanRequest{
		Roots:        cfg.Scan.Roots,
		Recursive:    cfg.Scan.Recursive,
		MaxDepth:     cfg.Scan.MaxDepth,
		IncludeDirs:  cfg.Safety.AllowDirDelete,
		IncludeFiles: cfg.Scan.IncludeFiles,
	}

	log.Debug("starting scan", logger.F("roots", cfg.Scan.Roots))

	cands, errc := sc.Scan(ctx, req)

	plan, err := pl.BuildPlan(ctx, cands, pol, safe, env, safetyCfg)
	if err != nil {
		return fmt.Errorf("build plan failed: %w", err)
	}

	// Priority ordering: allowed+safe first, then higher score first (stable, deterministic).
	sortPlan(plan)

	// Drain scanner error channel (non-blocking after scan completes)
	select {
	case scanErr := <-errc:
		if scanErr != nil && scanErr != context.Canceled {
			return fmt.Errorf("scan error: %w", scanErr)
		}
	default:
	}

	// Use first root for audit events (for backward compatibility)
	auditRoot := ""
	if len(cfg.Scan.Roots) > 0 {
		auditRoot = cfg.Scan.Roots[0]
	}

	// Plan-time audit: record the plan (allowed/blocked + reasons) before any execution.
	if aud != nil {
		for _, it := range plan {
			aud.Record(ctx, core.NewPlanAuditEvent(auditRoot, runMode, it))
		}
	}

	// Log plan summary
	printPlanSummary(plan, runMode, cfg.Scan.Roots, log)

	// Execute pass (only in execute mode)
	if runMode == core.ModeExecute {
		del := executor.NewSimpleWithMetrics(safe, safetyCfg, log, m)

		// Configure soft-delete if trash path is set
		if cfg.Execution.TrashPath != "" {
			trashMgr, err := trash.New(trash.Config{
				TrashPath: cfg.Execution.TrashPath,
				MaxAge:    cfg.Execution.TrashMaxAge,
			}, log)
			if err != nil {
				return fmt.Errorf("failed to initialize trash manager: %w", err)
			}
			del.WithTrash(trashMgr)
			log.Info("soft-delete enabled", logger.F("trash_path", cfg.Execution.TrashPath))
		}

		var (
			actionsAttempted int
			deletedCount     int
			executeDenied    int
			alreadyGone      int
			deleteFailed     int
			bytesFreed       int64
			hitLimit         bool
		)

		maxDel := cfg.Execution.MaxDeletionsPerRun

		for _, it := range plan {
			// Only attempt actions for items already allowed by policy + scan-time safety.
			if !it.Decision.Allow || !it.Safety.Allowed {
				continue
			}

			actionsAttempted++
			ar := del.Execute(ctx, it, runMode)
			if aud != nil {
				aud.Record(ctx, core.NewExecuteAuditEvent(auditRoot, runMode, it, ar))
			}

			if ar.Deleted {
				deletedCount++
				bytesFreed += ar.BytesFreed

				// Check batch limit (0 = unlimited)
				if maxDel > 0 && deletedCount >= maxDel {
					hitLimit = true
					break
				}
			}

			// Outcome accounting
			if len(ar.Reason) >= len("safety_deny_execute:") && ar.Reason[:len("safety_deny_execute:")] == "safety_deny_execute:" {
				executeDenied++
			} else if ar.Reason == "already_gone" {
				alreadyGone++
			} else if ar.Reason == "delete_failed" {
				deleteFailed++
			}
		}

		if hitLimit {
			log.Warn("batch limit reached, remaining files will be processed in next run",
				logger.F("limit", maxDel),
				logger.F("deleted", deletedCount),
				logger.F("bytes_freed", bytesFreed),
			)
		}

		log.Info("execution complete",
			logger.F("actions_attempted", actionsAttempted),
			logger.F("deleted", deletedCount),
			logger.F("bytes_freed", bytesFreed),
			logger.F("execute_denies", executeDenied),
			logger.F("already_gone", alreadyGone),
			logger.F("delete_failed", deleteFailed),
			logger.F("hit_limit", hitLimit),
		)
	}

	limit := cfg.Execution.MaxItems
	if limit > len(plan) {
		limit = len(plan)
	}

	// Log plan items as structured data
	planItems := make([]map[string]interface{}, 0, limit)
	for i := 0; i < limit; i++ {
		it := plan[i]
		planItems = append(planItems, map[string]interface{}{
			"path":   it.Candidate.Path,
			"score":  it.Decision.Score,
			"policy": it.Decision.Reason,
			"safety": it.Safety.Reason,
		})
	}
	log.Info("plan items", logger.F("items", planItems))

	return nil
}

// reasonKey collapses reasons like "symlink_self:/path/to/file" -> "symlink_self"
func reasonKey(s string) string {
	if i := strings.IndexByte(s, ':'); i > 0 {
		return s[:i]
	}
	return s
}

// printPlanSummary calculates and logs a summary of the cleanup plan.
func printPlanSummary(plan []core.PlanItem, runMode core.Mode, roots []string, log logger.Logger) {
	var (
		total         = len(plan)
		policyAllowed int
		safetyAllowed int
		reasonCounts  = map[string]int{}
		eligibleBytes int64
	)

	for _, it := range plan {
		if !it.Safety.Allowed {
			reasonCounts[reasonKey(it.Safety.Reason)]++
		}
		if it.Decision.Allow {
			policyAllowed++
		}
		if it.Safety.Allowed {
			safetyAllowed++
		}
		if it.Decision.Allow && it.Safety.Allowed && it.Candidate.Type == core.TargetFile {
			eligibleBytes += it.Candidate.SizeBytes
		}
	}

	pipelineType := "dry-run"
	if runMode == core.ModeExecute {
		pipelineType = "execute"
	}

	log.Info("plan summary",
		logger.F("pipeline", pipelineType),
		logger.F("roots", roots),
		logger.F("candidates", total),
		logger.F("policy_allowed", policyAllowed),
		logger.F("safety_allowed", safetyAllowed),
		logger.F("eligible_bytes", eligibleBytes),
		logger.F("safety_blocked", total-safetyAllowed),
	)

	if len(reasonCounts) > 0 {
		log.Info("safety block reasons", logger.F("reasons", reasonCounts))
	}
}

// buildPolicy constructs a composite policy from configuration.
func buildPolicy(cfg config.PolicyConfig, log logger.Logger) core.Policy {
	// Start with age policy
	var pol core.Policy = policy.NewAgePolicy(cfg.MinAgeDays)

	// If additional filters are specified, build a composite policy
	var additionalPolicies []core.Policy
	if cfg.MinSizeMB > 0 {
		additionalPolicies = append(additionalPolicies, policy.NewSizePolicy(cfg.MinSizeMB))
	}
	if len(cfg.Extensions) > 0 {
		additionalPolicies = append(additionalPolicies, policy.NewExtensionPolicy(cfg.Extensions))
	}

	// Combine with AND: must match age AND any additional filters
	if len(additionalPolicies) > 0 {
		allPolicies := append([]core.Policy{pol}, additionalPolicies...)
		pol = policy.NewCompositePolicy(policy.ModeAnd, allPolicies...)
	}

	// Add exclusion policy (must NOT match any exclusion pattern)
	if len(cfg.Exclusions) > 0 {
		exclusionPolicy := policy.NewExclusionPolicy(cfg.Exclusions)
		pol = policy.NewCompositePolicy(policy.ModeAnd, pol, exclusionPolicy)
		log.Debug("exclusion patterns active", logger.F("patterns", cfg.Exclusions))
	}

	return pol
}

// sortPlan orders plan items: allowed+safe first, then by score, size, modtime, path.
func sortPlan(plan []core.PlanItem) {
	sort.SliceStable(plan, func(i, j int) bool {
		a := plan[i]
		b := plan[j]

		aOK := a.Decision.Allow && a.Safety.Allowed
		bOK := b.Decision.Allow && b.Safety.Allowed
		if aOK != bOK {
			return aOK
		}

		if a.Decision.Score != b.Decision.Score {
			return a.Decision.Score > b.Decision.Score
		}
		if a.Candidate.SizeBytes != b.Candidate.SizeBytes {
			return a.Candidate.SizeBytes > b.Candidate.SizeBytes
		}
		if !a.Candidate.ModTime.Equal(b.Candidate.ModTime) {
			return a.Candidate.ModTime.Before(b.Candidate.ModTime)
		}
		return a.Candidate.Path < b.Candidate.Path
	})
}

// createNotifier creates a notifier from configuration.
func createNotifier(cfg config.NotificationsConfig, log logger.Logger) notifier.Notifier {
	if len(cfg.Webhooks) == 0 {
		return &notifier.NoopNotifier{}
	}

	multi := notifier.NewMultiNotifier()
	for _, whCfg := range cfg.Webhooks {
		// Convert config events to notifier events
		events := make([]notifier.EventType, 0, len(whCfg.Events))
		for _, e := range whCfg.Events {
			events = append(events, notifier.EventType(e))
		}

		wh := notifier.NewWebhook(notifier.WebhookConfig{
			URL:     whCfg.URL,
			Headers: whCfg.Headers,
			Events:  events,
			Timeout: whCfg.Timeout,
		})
		multi.Add(wh)

		log.Info("webhook configured", logger.F("url", whCfg.URL))
	}

	return multi
}
