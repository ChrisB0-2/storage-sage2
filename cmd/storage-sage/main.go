package main

import (
	"context"
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
	"github.com/ChrisB0-2/storage-sage/internal/executor"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
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
	protectedPaths = flag.String("protected", "", "comma-separated additional protected paths")
	allowDirDelete = flag.Bool("allow-dir-delete", false, "allow deletion of directories")
	minSizeMB      = flag.Int("min-size-mb", -1, "minimum file size in MB (-1 = use config default)")
	extensions     = flag.String("extensions", "", "comma-separated extensions to match")
)

func main() {
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
	log, err := initLogger(cfg.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log.Info("storage-sage starting",
		logger.F("mode", cfg.Execution.Mode),
		logger.F("roots", cfg.Scan.Roots),
	)

	// 5. Run main logic with logger-aware components
	if err := run(cfg, log); err != nil {
		log.Error("execution failed", logger.F("error", err.Error()))
		os.Exit(1)
	}
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
}

// initLogger creates a logger based on configuration.
func initLogger(cfg config.LoggingConfig) (logger.Logger, error) {
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
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = f
	}

	return logger.New(level, output), nil
}

// run executes the main storage-sage logic.
func run(cfg *config.Config, log logger.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Execution.Timeout)
	defer cancel()

	runMode := core.Mode(cfg.Execution.Mode)

	// Auditor (optional)
	var aud core.Auditor
	if cfg.Execution.AuditPath != "" {
		a, aerr := auditor.NewJSONL(cfg.Execution.AuditPath)
		if aerr != nil {
			return fmt.Errorf("audit init failed: %w", aerr)
		}
		aud = a
		defer func() {
			if err := a.Err(); err != nil {
				log.Warn("audit write error", logger.F("error", err.Error()))
			}
			_ = a.Close()
		}()
	}

	// Components with logger injection
	sc := scanner.NewWalkDirWithLogger(log)
	pl := planner.NewSimpleWithLogger(log)
	safe := safety.NewWithLogger(log)

	// Build policy: start with age, optionally add size/extension filters
	var pol core.Policy = policy.NewAgePolicy(cfg.Policy.MinAgeDays)

	// If additional filters are specified, build a composite policy
	var additionalPolicies []core.Policy
	if cfg.Policy.MinSizeMB > 0 {
		additionalPolicies = append(additionalPolicies, policy.NewSizePolicy(cfg.Policy.MinSizeMB))
	}
	if len(cfg.Policy.Extensions) > 0 {
		additionalPolicies = append(additionalPolicies, policy.NewExtensionPolicy(cfg.Policy.Extensions))
	}

	// Combine with AND: must match age AND any additional filters
	if len(additionalPolicies) > 0 {
		allPolicies := append([]core.Policy{pol}, additionalPolicies...)
		pol = policy.NewCompositePolicy(policy.ModeAnd, allPolicies...)
	}

	// Environment snapshot
	env := core.EnvSnapshot{
		Now:         time.Now(),
		DiskUsedPct: 0,
		CPUUsedPct:  0,
	}

	// Safety config
	safetyCfg := core.SafetyConfig{
		AllowedRoots:   cfg.Scan.Roots,
		ProtectedPaths: cfg.Safety.ProtectedPaths,
		AllowDirDelete: cfg.Safety.AllowDirDelete,
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

	// Summaries (plan-time)
	var (
		total               = len(plan)
		policyAllowed       = 0
		safetyAllowed       = 0
		reasonCounts        = map[string]int{}
		eligibleBytes int64 = 0
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

	fmt.Printf("roots: %v\n", cfg.Scan.Roots)
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

	// Execute pass (only in execute mode)
	if runMode == core.ModeExecute {
		del := executor.NewSimpleWithLogger(safe, safetyCfg, log)

		var (
			actionsAttempted       = 0
			deletedCount           = 0
			executeDenied          = 0
			alreadyGone            = 0
			deleteFailed           = 0
			bytesFreed       int64 = 0
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
	if limit > total {
		limit = total
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
