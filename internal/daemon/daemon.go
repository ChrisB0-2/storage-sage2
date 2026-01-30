package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/auditor"
	"github.com/ChrisB0-2/storage-sage/internal/auth"
	"github.com/ChrisB0-2/storage-sage/internal/config"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/pidfile"
	"github.com/ChrisB0-2/storage-sage/internal/trash"
	"github.com/ChrisB0-2/storage-sage/internal/web"
)

// State represents the current daemon state.
type State int32

const (
	StateStarting State = iota
	StateReady
	StateRunning
	StateStopping
	StateStopped
)

// State string constants.
const (
	stateStrStarting = "starting"
	stateStrReady    = "ready"
	stateStrRunning  = "running"
	stateStrStopping = "stopping"
	stateStrStopped  = "stopped"
	stateStrUnknown  = "unknown"
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return stateStrStarting
	case StateReady:
		return stateStrReady
	case StateRunning:
		return stateStrRunning
	case StateStopping:
		return stateStrStopping
	case StateStopped:
		return stateStrStopped
	default:
		return stateStrUnknown
	}
}

// RunFunc is the function called on each scheduled run.
type RunFunc func(ctx context.Context) error

// Daemon manages the lifecycle of a long-running storage-sage process.
type Daemon struct {
	log            logger.Logger
	runFunc        RunFunc
	schedule       string
	httpAddr       string
	triggerTimeout time.Duration
	pidFilePath    string
	runWaitTimeout time.Duration // timeout for waiting on in-flight runs during shutdown

	// Optional references for API endpoints
	cfg     *config.Config
	auditor *auditor.SQLiteAuditor
	trash   *trash.Manager

	// Optional authentication middleware
	authMiddleware *auth.Middleware
	rbacMiddleware *auth.RBACMiddleware

	state       atomic.Int32
	running     atomic.Bool
	lastRun     time.Time
	lastErr     error
	runCount    int64
	mu          sync.RWMutex
	stopCh      chan struct{}
	stopOnce    sync.Once
	auditorOnce sync.Once      // ensures auditor Close() is called exactly once
	runsWG      sync.WaitGroup // tracks in-flight runs (scheduled + API-triggered)
	httpServer  *http.Server
	pidFile     *pidfile.PIDFile
}

// Config holds daemon configuration.
type Config struct {
	Schedule       string        // Cron expression (e.g., "0 */6 * * *" for every 6 hours)
	HTTPAddr       string        // Address for health/ready endpoints (e.g., ":8080")
	TriggerTimeout time.Duration // Timeout for manual trigger requests (default: 30m)
	PIDFile        string        // Path to PID file for single-instance enforcement
	RunWaitTimeout time.Duration // Timeout for waiting on in-flight runs during shutdown (default: 10s)

	// Optional: references for API endpoints
	AppConfig *config.Config         // Application config to expose via /api/config
	Auditor   *auditor.SQLiteAuditor // Auditor for /api/audit/* endpoints
	Trash     *trash.Manager         // Trash manager for /api/trash/* endpoints

	// Optional: authentication middleware
	AuthMiddleware *auth.Middleware     // Authentication middleware
	RBACMiddleware *auth.RBACMiddleware // Role-based access control middleware
}

// New creates a new daemon instance.
func New(log logger.Logger, runFunc RunFunc, cfg Config) *Daemon {
	if log == nil {
		log = logger.NewNop()
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.TriggerTimeout <= 0 {
		cfg.TriggerTimeout = 30 * time.Minute
	}
	if cfg.RunWaitTimeout <= 0 {
		cfg.RunWaitTimeout = 10 * time.Second
	}

	d := &Daemon{
		log:            log,
		runFunc:        runFunc,
		schedule:       cfg.Schedule,
		httpAddr:       cfg.HTTPAddr,
		triggerTimeout: cfg.TriggerTimeout,
		runWaitTimeout: cfg.RunWaitTimeout,
		pidFilePath:    cfg.PIDFile,
		cfg:            cfg.AppConfig,
		auditor:        cfg.Auditor,
		trash:          cfg.Trash,
		authMiddleware: cfg.AuthMiddleware,
		rbacMiddleware: cfg.RBACMiddleware,
		stopCh:         make(chan struct{}),
	}
	d.state.Store(int32(StateStarting))

	return d
}

// Run starts the daemon and blocks until shutdown.
// It handles SIGINT and SIGTERM for graceful shutdown.
// The daemon takes ownership of the configured auditor and will close it on shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.log.Info("daemon starting", logger.F("http_addr", d.httpAddr), logger.F("schedule", d.schedule))

	// Ensure auditor is closed on any exit path (normal, panic, early return)
	defer d.closeAuditor()

	// Acquire PID file lock (prevents multiple instances)
	if d.pidFilePath != "" {
		pf, err := pidfile.New(d.pidFilePath)
		if err != nil {
			return fmt.Errorf("failed to acquire pid file lock: %w", err)
		}
		d.pidFile = pf
		d.log.Info("pid file acquired", logger.F("path", d.pidFilePath))

		// Ensure PID file is released on exit
		defer func() {
			if err := d.pidFile.Close(); err != nil {
				d.log.Warn("failed to release pid file", logger.F("error", err.Error()))
			} else {
				d.log.Debug("pid file released")
			}
		}()
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server for health endpoints
	if err := d.startHTTP(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Mark as ready
	d.state.Store(int32(StateReady))
	d.log.Info("daemon ready")

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start scheduler if schedule is configured
	var schedulerDone chan struct{}
	if d.schedule != "" {
		schedulerDone = make(chan struct{})
		go d.runScheduler(ctx, schedulerDone)
	}

	// Wait for shutdown signal
	select {
	case sig := <-sigCh:
		d.log.Info("received signal", logger.F("signal", sig.String()))
	case <-ctx.Done():
		d.log.Info("context canceled")
	case <-d.stopCh:
		d.log.Info("stop requested")
	}

	// Begin shutdown
	d.state.Store(int32(StateStopping))
	d.log.Info("daemon stopping")

	// Cancel context to stop scheduler
	cancel()

	// Wait for scheduler to finish
	if schedulerDone != nil {
		<-schedulerDone
	}

	// Stop HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := d.httpServer.Shutdown(shutdownCtx); err != nil {
		d.log.Warn("HTTP server shutdown error", logger.F("error", err.Error()))
	}

	// Wait for any in-flight runs to complete (or timeout)
	d.log.Debug("waiting for in-flight runs to complete")
	if !d.waitForRuns(d.runWaitTimeout) {
		d.log.Warn("timed out waiting for in-flight runs", logger.F("timeout", d.runWaitTimeout.String()))
	}

	// Close auditor (also called via defer, but explicit call makes shutdown order clear)
	d.closeAuditor()

	d.state.Store(int32(StateStopped))
	d.log.Info("daemon stopped")

	return nil
}

// Stop signals the daemon to shut down.
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
}

// closeAuditor closes the auditor if configured, exactly once.
// Safe to call multiple times; subsequent calls are no-ops.
// Errors are logged but do not fail the shutdown.
func (d *Daemon) closeAuditor() {
	d.auditorOnce.Do(func() {
		if d.auditor == nil {
			return
		}
		d.log.Debug("closing auditor")
		if err := d.auditor.Close(); err != nil {
			d.log.Warn("failed to close auditor", logger.F("error", err.Error()))
		} else {
			d.log.Debug("auditor closed")
		}
	})
}

// waitForRuns waits for all in-flight runs to complete with a timeout.
// Returns true if all runs completed, false if timed out.
func (d *Daemon) waitForRuns(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		d.runsWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TriggerRun manually triggers a run (for API use).
// Returns error if a run is already in progress.
// Includes panic recovery to prevent API handler crashes.
func (d *Daemon) TriggerRun(ctx context.Context) (err error) {
	if !d.running.CompareAndSwap(false, true) {
		return fmt.Errorf("run already in progress")
	}

	// Track this run for graceful shutdown (must defer Done before running.Store(false))
	d.runsWG.Add(1)
	defer d.runsWG.Done()
	defer d.running.Store(false)

	// Panic recovery for API-triggered runs
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.log.Error("trigger run panic recovered",
				logger.F("panic", fmt.Sprintf("%v", r)),
				logger.F("stack", string(stack)))

			// Record the panic as an error
			d.mu.Lock()
			d.lastErr = fmt.Errorf("trigger panic: %v", r)
			d.runCount++
			d.lastRun = time.Now()
			d.mu.Unlock()

			// Return error to caller instead of crashing
			err = fmt.Errorf("run panicked: %v", r)
		}
	}()

	return d.executeRun(ctx)
}

// State returns the current daemon state.
func (d *Daemon) State() State {
	return State(d.state.Load())
}

// IsRunning returns true if a cleanup run is currently in progress.
func (d *Daemon) IsRunning() bool {
	return d.running.Load()
}

// LastRun returns info about the last run.
func (d *Daemon) LastRun() (time.Time, int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastRun, d.runCount, d.lastErr
}

// runScheduler runs the cleanup on the configured schedule.
// It includes panic recovery to prevent the daemon from crashing on unhandled panics.
func (d *Daemon) runScheduler(ctx context.Context, done chan struct{}) {
	defer close(done)

	// Panic recovery: log stack trace and mark daemon as stopped.
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.log.Error("scheduler panic recovered",
				logger.F("panic", fmt.Sprintf("%v", r)),
				logger.F("stack", string(stack)))

			// Record the panic as an error in lastErr for visibility
			d.mu.Lock()
			d.lastErr = fmt.Errorf("scheduler panic: %v", r)
			d.mu.Unlock()

			// Transition to stopped state - the daemon is no longer functional
			d.state.Store(int32(StateStopped))
			d.running.Store(false)

			// Signal stop to allow graceful cleanup
			d.Stop()
		}
	}()

	interval, err := parseSchedule(d.schedule)
	if err != nil {
		d.log.Error("invalid schedule", logger.F("schedule", d.schedule), logger.F("error", err.Error()))
		return
	}

	d.log.Info("scheduler started", logger.F("interval", interval.String()))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Debug("scheduler stopping")
			return
		case <-ticker.C:
			if d.running.CompareAndSwap(false, true) {
				// Track this run for graceful shutdown
				d.runsWG.Add(1)
				func() {
					defer d.runsWG.Done()
					defer d.running.Store(false)
					d.state.Store(int32(StateRunning))
					d.safeExecuteRun(ctx)
					d.state.Store(int32(StateReady))
				}()
			} else {
				d.log.Warn("skipping scheduled run - previous run still in progress")
			}
		}
	}
}

// safeExecuteRun wraps executeRun with panic recovery.
// This ensures a panic in the run function doesn't crash the scheduler goroutine.
func (d *Daemon) safeExecuteRun(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.log.Error("run panic recovered",
				logger.F("panic", fmt.Sprintf("%v", r)),
				logger.F("stack", string(stack)))

			// Record the panic as an error
			d.mu.Lock()
			d.lastErr = fmt.Errorf("run panic: %v", r)
			d.runCount++
			d.lastRun = time.Now()
			d.mu.Unlock()
		}
	}()

	err := d.executeRun(ctx)
	if err != nil && ctx.Err() == nil {
		d.log.Error("scheduled run failed", logger.F("error", err.Error()))
	}
}

// executeRun performs a single cleanup run.
func (d *Daemon) executeRun(ctx context.Context) error {
	d.log.Info("starting cleanup run")
	start := time.Now()

	err := d.runFunc(ctx)

	d.mu.Lock()
	d.lastRun = start
	d.lastErr = err
	d.runCount++
	d.mu.Unlock()

	duration := time.Since(start)
	if err != nil {
		d.log.Error("cleanup run failed",
			logger.F("duration", duration.String()),
			logger.F("error", err.Error()))
	} else {
		d.log.Info("cleanup run completed", logger.F("duration", duration.String()))
	}

	return err
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

// startHTTP initializes and starts the HTTP server for health endpoints.
func (d *Daemon) startHTTP() error {
	mux := http.NewServeMux()

	// Health endpoint - basic liveness check
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ok","state":"%s"}`, d.State().String())
	})

	// Ready endpoint - readiness check (not ready if stopping/stopped or disk critically full)
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		state := d.State()
		w.Header().Set("Content-Type", "application/json")

		// Check if daemon is in a ready state
		if state != StateReady && state != StateRunning {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"ready":false,"state":"%s","reason":"daemon not ready"}`, state.String())
			return
		}

		// Check disk space on scan roots if config is available
		if d.cfg != nil && len(d.cfg.Scan.Roots) > 0 {
			for _, root := range d.cfg.Scan.Roots {
				usedPct, err := getDiskUsagePercent(root)
				if err != nil {
					// Log but don't fail readiness for inaccessible paths
					d.log.Warn("disk check failed", logger.F("path", root), logger.F("error", err.Error()))
					continue
				}
				// Fail readiness if disk is critically full (>95%)
				if usedPct > 95.0 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = fmt.Fprintf(w, `{"ready":false,"state":"%s","reason":"disk critically full","path":"%s","disk_used_percent":%.1f}`,
						state.String(), root, usedPct)
					return
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"ready":true,"state":"%s"}`, state.String())
	})

	// Status endpoint - detailed status information
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		lastRun, runCount, lastErr := d.LastRun()
		w.Header().Set("Content-Type", "application/json")

		errStr := ""
		if lastErr != nil {
			errStr = lastErr.Error()
		}

		lastRunStr := ""
		if !lastRun.IsZero() {
			lastRunStr = lastRun.Format(time.RFC3339)
		}

		d.writeJSONResponse(w, http.StatusOK, map[string]any{
			"state":      d.State().String(),
			"running":    d.IsRunning(),
			"last_run":   lastRunStr,
			"last_error": errStr,
			"run_count":  runCount,
			"schedule":   d.schedule,
		})
	})

	// Trigger endpoint - manually trigger a run (POST only)
	mux.HandleFunc("/trigger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Use request context with configurable timeout
		ctx, cancel := context.WithTimeout(r.Context(), d.triggerTimeout)
		defer cancel()

		if err := d.TriggerRun(ctx); err != nil {
			d.writeJSONResponse(w, http.StatusConflict, map[string]any{
				"triggered": false,
				"error":     err.Error(),
			})
			return
		}

		d.writeJSONResponse(w, http.StatusOK, map[string]any{"triggered": true})
	})

	// API endpoints for frontend
	mux.HandleFunc("/api/config", d.handleAPIConfig)
	mux.HandleFunc("/api/audit/query", d.handleAuditQuery)
	mux.HandleFunc("/api/audit/stats", d.handleAuditStats)
	mux.HandleFunc("/api/trash", d.handleTrash)
	mux.HandleFunc("/api/trash/restore", d.handleTrashRestore)

	// Serve embedded frontend (SPA with fallback to index.html)
	d.setupStaticFileServer(mux)

	// Wrap handler with middleware (order matters: auth runs first, then RBAC)
	var handler http.Handler = mux
	if d.rbacMiddleware != nil {
		handler = d.rbacMiddleware.Wrap(handler)
	}
	if d.authMiddleware != nil {
		// Auth must wrap outermost so it runs first and sets Identity in context
		handler = d.authMiddleware.Wrap(handler)
	}

	d.httpServer = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Create listener first to ensure port is available before returning
	ln, err := net.Listen("tcp", d.httpAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.httpAddr, err)
	}

	// Start server in goroutine with the already-bound listener
	go func() {
		if err := d.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			d.log.Error("HTTP server error", logger.F("error", err.Error()))
		}
	}()

	return nil
}

// handleAPIConfig returns the current running configuration as JSON.
func (d *Daemon) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if d.cfg == nil {
		d.writeJSONError(w, http.StatusNotFound, "config not available")
		return
	}

	// Return config as JSON
	d.writeJSONResponse(w, http.StatusOK, d.cfg)
}

// Valid values for audit query filters.
var (
	validActions = map[string]bool{"": true, "plan": true, "execute": true, "error": true}
	validLevels  = map[string]bool{"": true, "info": true, "warn": true, "error": true, "debug": true}
)

const maxQueryLimit = 1000

// handleAuditQuery queries audit records with optional filters.
// Query params: since, until, action, level, path, limit
func (d *Daemon) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if d.auditor == nil {
		d.writeJSONError(w, http.StatusNotFound, "auditor not available")
		return
	}

	// Parse query parameters
	q := r.URL.Query()

	// Validate action parameter
	action := q.Get("action")
	if !validActions[action] {
		d.writeJSONError(w, http.StatusBadRequest, "invalid action: must be one of plan, execute, error")
		return
	}

	// Validate level parameter
	level := q.Get("level")
	if !validLevels[level] {
		d.writeJSONError(w, http.StatusBadRequest, "invalid level: must be one of debug, info, warn, error")
		return
	}

	filter := auditor.QueryFilter{
		Action: action,
		Level:  level,
		Path:   q.Get("path"),
	}

	// Parse time filters
	if since := q.Get("since"); since != "" {
		if t, err := parseTimeParam(since); err == nil {
			filter.Since = t
		}
	}
	if until := q.Get("until"); until != "" {
		if t, err := parseTimeParam(until); err == nil {
			filter.Until = t
		}
	}

	// Parse and validate limit (default 100, max 1000)
	filter.Limit = 100
	if limitStr := q.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			d.writeJSONError(w, http.StatusBadRequest, "invalid limit: must be a positive integer")
			return
		}
		if limit > maxQueryLimit {
			limit = maxQueryLimit
		}
		filter.Limit = limit
	}

	// Query audit records
	records, err := d.auditor.Query(r.Context(), filter)
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}

	// Return records as JSON
	d.writeJSONResponse(w, http.StatusOK, records)
}

// handleAuditStats returns audit statistics summary.
func (d *Daemon) handleAuditStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if d.auditor == nil {
		d.writeJSONError(w, http.StatusNotFound, "auditor not available")
		return
	}

	stats, err := d.auditor.Stats(r.Context())
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "stats failed: "+err.Error())
		return
	}

	// Return stats as JSON
	d.writeJSONResponse(w, http.StatusOK, stats)
}

// TrashItemResponse is the JSON representation of a trash item.
type TrashItemResponse struct {
	Name         string `json:"name"`
	OriginalPath string `json:"original_path"`
	Size         int64  `json:"size"`
	TrashedAt    string `json:"trashed_at"`
	IsDir        bool   `json:"is_dir"`
}

// handleTrash handles GET (list) and DELETE (empty) for /api/trash.
func (d *Daemon) handleTrash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if d.trash == nil {
		d.writeJSONError(w, http.StatusNotFound, "trash not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		d.handleTrashList(w)
	case http.MethodDelete:
		d.handleTrashEmpty(w, r)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTrashList returns all items in trash.
func (d *Daemon) handleTrashList(w http.ResponseWriter) {
	items, err := d.trash.List()
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "failed to list trash: "+err.Error())
		return
	}

	// Convert to JSON response format
	response := make([]TrashItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, TrashItemResponse{
			Name:         item.Name,
			OriginalPath: item.OriginalPath,
			Size:         item.Size,
			TrashedAt:    item.TrashedAt.Format(time.RFC3339),
			IsDir:        item.IsDir,
		})
	}

	d.writeJSONResponse(w, http.StatusOK, response)
}

// handleTrashEmpty permanently deletes items from trash.
// Query params: older_than (duration string like "7d", "24h"), all (boolean)
func (d *Daemon) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Check for "all" parameter
	if q.Get("all") == "true" {
		items, err := d.trash.List()
		if err != nil {
			d.writeJSONError(w, http.StatusInternalServerError, "failed to list trash: "+err.Error())
			return
		}

		var deleted int
		var bytesFreed int64
		for _, item := range items {
			if err := os.RemoveAll(item.TrashPath); err != nil {
				d.log.Warn("failed to delete trash item", logger.F("path", item.TrashPath), logger.F("error", err.Error()))
				continue
			}
			_ = os.Remove(item.TrashPath + ".meta")
			deleted++
			bytesFreed += item.Size
		}

		d.writeJSONResponse(w, http.StatusOK, map[string]any{
			"deleted":     deleted,
			"bytes_freed": bytesFreed,
		})
		return
	}

	// Check for "older_than" parameter
	olderThan := q.Get("older_than")
	if olderThan == "" {
		d.writeJSONError(w, http.StatusBadRequest, "must specify 'older_than' duration (e.g., '7d', '24h') or 'all=true'")
		return
	}

	// Parse duration
	duration, err := parseDurationWithDays(olderThan)
	if err != nil {
		d.writeJSONError(w, http.StatusBadRequest, "invalid duration: "+err.Error())
		return
	}

	items, err := d.trash.List()
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "failed to list trash: "+err.Error())
		return
	}

	cutoff := time.Now().Add(-duration)
	var deleted int
	var bytesFreed int64

	for _, item := range items {
		if item.TrashedAt.Before(cutoff) {
			if err := os.RemoveAll(item.TrashPath); err != nil {
				d.log.Warn("failed to delete trash item", logger.F("path", item.TrashPath), logger.F("error", err.Error()))
				continue
			}
			_ = os.Remove(item.TrashPath + ".meta")
			deleted++
			bytesFreed += item.Size
		}
	}

	d.writeJSONResponse(w, http.StatusOK, map[string]any{
		"deleted":     deleted,
		"bytes_freed": bytesFreed,
	})
}

// TrashRestoreRequest is the JSON request body for restore.
type TrashRestoreRequest struct {
	Name string `json:"name"`
}

// handleTrashRestore restores an item from trash.
func (d *Daemon) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if d.trash == nil {
		d.writeJSONError(w, http.StatusNotFound, "trash not configured")
		return
	}

	// Parse request body
	var req TrashRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		d.writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Find the item in trash
	items, err := d.trash.List()
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "failed to list trash: "+err.Error())
		return
	}

	var targetItem *trash.TrashItem
	for i := range items {
		if items[i].Name == req.Name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		d.writeJSONError(w, http.StatusNotFound, "item not found in trash: "+req.Name)
		return
	}

	// Restore the item
	originalPath, err := d.trash.Restore(targetItem.TrashPath)
	if err != nil {
		d.writeJSONError(w, http.StatusInternalServerError, "failed to restore: "+err.Error())
		return
	}

	d.writeJSONResponse(w, http.StatusOK, map[string]any{
		"restored":      true,
		"original_path": originalPath,
	})
}

// parseDurationWithDays parses a duration string that may include days (e.g., "7d", "24h").
func parseDurationWithDays(s string) (time.Duration, error) {
	// Handle day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		numStr := s[:len(s)-1]
		var n int
		if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
		return 0, fmt.Errorf("invalid day duration: %s", s)
	}

	// Fall back to standard duration parsing
	return time.ParseDuration(s)
}

// setupStaticFileServer configures the mux to serve embedded frontend files.
// Uses SPA-style routing: serves index.html for any path that doesn't match a static file.
func (d *Daemon) setupStaticFileServer(mux *http.ServeMux) {
	distFS, err := web.DistFS()
	if err != nil {
		d.log.Warn("frontend not available", logger.F("error", err.Error()))
		return
	}

	// Check if frontend is built
	if !web.HasDist() {
		d.log.Info("frontend not built, UI disabled")
		return
	}

	// Create file server for static assets
	fileServer := http.FileServer(http.FS(distFS))

	// Serve static files with SPA fallback
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Skip API and health endpoints
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") ||
			path == "/health" ||
			path == "/ready" ||
			path == "/status" ||
			path == "/trigger" {
			http.NotFound(w, r)
			return
		}

		// Try to serve the actual file
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		// Check if file exists in embedded FS
		if _, err := fs.Stat(distFS, cleanPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for all other routes
		indexFile, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexFile)
	})

	d.log.Info("frontend UI enabled")
}

// writeJSONError writes a JSON error response with properly escaped message.
func (d *Daemon) writeJSONError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	resp := map[string]string{"error": message}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.log.Error("failed to encode JSON error response", logger.F("error", err.Error()))
	}
}

// writeJSONResponse writes a JSON response with the given data.
func (d *Daemon) writeJSONResponse(w http.ResponseWriter, status int, data any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		d.log.Error("failed to encode JSON response", logger.F("error", err.Error()))
	}
}

// parseTimeParam parses a time parameter from various formats.
// Supports: RFC3339, date (2006-01-02), and duration strings (24h, 7d).
func parseTimeParam(s string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try date format
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	// Try duration format (e.g., "24h", "7d")
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
				return time.Now().Add(-time.Duration(n) * multiplier), nil
			}
		}
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s", s)
}
