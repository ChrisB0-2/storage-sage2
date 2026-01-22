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

	// Daemon mode flags
	daemonMode = flag.Bool("daemon", false, "run as long-running daemon")
	schedule   = flag.String("schedule", "", "run schedule (e.g., '1h', '30m', '@every 6h')")
	daemonAddr = flag.String("daemon-addr", ":8080", "daemon health endpoint address")

	// Loki flags
	enableLoki = flag.Bool("loki", false, "enable Loki log shipping")
	lokiURL    = flag.String("loki-url", "", "Loki server URL (default http://localhost:3100)")
)

func main() {
	// Check for subcommands before parsing flags
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "query":
			runQueryCmd(os.Args[2:])
			return
		case "stats":
			runStatsCmd(os.Args[2:])
			return
		case "verify":
			runVerifyCmd(os.Args[2:])
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
		_ = enc.Encode(records)
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
		_ = enc.Encode(stats)
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
		}

		_ = notify.Notify(ctx, payload)

		return err
	}

	// Create and run daemon with config and auditor for API endpoints
	d := daemon.New(log, runFunc, daemon.Config{
		Schedule:       sched,
		HTTPAddr:       addr,
		TriggerTimeout: cfg.Daemon.TriggerTimeout,
		AppConfig:      cfg,
		Auditor:        sqlAud,
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

	// Print plan summary
	printPlanSummary(plan, runMode, cfg.Scan.Roots)

	// Execute pass (only in execute mode)
	if runMode == core.ModeExecute {
		del := executor.NewSimpleWithMetrics(safe, safetyCfg, log, m)

		var (
			actionsAttempted int
			deletedCount     int
			executeDenied    int
			alreadyGone      int
			deleteFailed     int
			bytesFreed       int64
		)

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

		fmt.Printf("actions attempted: %d\n", actionsAttempted)
		fmt.Printf("deleted: %d\n", deletedCount)
		fmt.Printf("bytes freed: %d\n", bytesFreed)
		fmt.Printf("execute denies: %d\n", executeDenied)
		fmt.Printf("already gone: %d\n", alreadyGone)
		fmt.Printf("delete failed: %d\n", deleteFailed)
		fmt.Println()

		log.Info("execution complete",
			logger.F("deleted", deletedCount),
			logger.F("bytes_freed", bytesFreed),
		)
	}

	limit := cfg.Execution.MaxItems
	if limit > len(plan) {
		limit = len(plan)
	}

	fmt.Printf("First %d plan items:\n", limit)

	for i := 0; i < limit; i++ {
		it := plan[i]
		fmt.Printf("- %s | score=%d | policy=%s | safety=%s\n",
			it.Candidate.Path,
			it.Decision.Score,
			it.Decision.Reason,
			it.Safety.Reason,
		)
	}

	return nil
}

// reasonKey collapses reasons like "symlink_self:/path/to/file" -> "symlink_self"
func reasonKey(s string) string {
	if i := strings.IndexByte(s, ':'); i > 0 {
		return s[:i]
	}
	return s
}

// printPlanSummary calculates and prints a summary of the cleanup plan.
func printPlanSummary(plan []core.PlanItem, runMode core.Mode, roots []string) {
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

	if runMode == core.ModeExecute {
		fmt.Printf("StorageSage (EXECUTE PIPELINE)\n")
	} else {
		fmt.Printf("StorageSage (DRY PIPELINE)\n")
	}

	fmt.Printf("roots: %v\n", roots)
	fmt.Printf("candidates: %d\n", total)
	fmt.Printf("policy allowed: %d\n", policyAllowed)
	fmt.Printf("safety allowed: %d\n", safetyAllowed)
	fmt.Printf("eligible bytes (policy+safe): %d\n", eligibleBytes)
	fmt.Printf("safety blocked: %d\n", total-safetyAllowed)
	if len(reasonCounts) > 0 {
		keys := make([]string, 0, len(reasonCounts))
		for k := range reasonCounts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println("safety reasons:")
		for _, k := range keys {
			fmt.Printf("  - %s: %d\n", k, reasonCounts[k])
		}
		fmt.Println()
	}
	fmt.Println()
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
