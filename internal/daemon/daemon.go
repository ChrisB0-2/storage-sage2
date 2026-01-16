package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
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

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateReady:
		return "ready"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// RunFunc is the function called on each scheduled run.
type RunFunc func(ctx context.Context) error

// Daemon manages the lifecycle of a long-running storage-sage process.
type Daemon struct {
	log      logger.Logger
	runFunc  RunFunc
	schedule string
	httpAddr string

	state      atomic.Int32
	running    atomic.Bool
	lastRun    time.Time
	lastErr    error
	runCount   int64
	mu         sync.RWMutex
	stopCh     chan struct{}
	httpServer *http.Server
}

// Config holds daemon configuration.
type Config struct {
	Schedule string // Cron expression (e.g., "0 */6 * * *" for every 6 hours)
	HTTPAddr string // Address for health/ready endpoints (e.g., ":8080")
}

// New creates a new daemon instance.
func New(log logger.Logger, runFunc RunFunc, cfg Config) *Daemon {
	if log == nil {
		log = logger.NewNop()
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	d := &Daemon{
		log:      log,
		runFunc:  runFunc,
		schedule: cfg.Schedule,
		httpAddr: cfg.HTTPAddr,
		stopCh:   make(chan struct{}),
	}
	d.state.Store(int32(StateStarting))

	return d
}

// Run starts the daemon and blocks until shutdown.
// It handles SIGINT and SIGTERM for graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.log.Info("daemon starting", logger.F("http_addr", d.httpAddr), logger.F("schedule", d.schedule))

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

	d.state.Store(int32(StateStopped))
	d.log.Info("daemon stopped")

	return nil
}

// Stop signals the daemon to shut down.
func (d *Daemon) Stop() {
	close(d.stopCh)
}

// TriggerRun manually triggers a run (for API use).
// Returns error if a run is already in progress.
func (d *Daemon) TriggerRun(ctx context.Context) error {
	if !d.running.CompareAndSwap(false, true) {
		return fmt.Errorf("run already in progress")
	}
	defer d.running.Store(false)

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
func (d *Daemon) runScheduler(ctx context.Context, done chan struct{}) {
	defer close(done)

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
				d.state.Store(int32(StateRunning))
				err := d.executeRun(ctx)
				if err != nil && ctx.Err() == nil {
					d.log.Error("scheduled run failed", logger.F("error", err.Error()))
				}
				d.state.Store(int32(StateReady))
				d.running.Store(false)
			} else {
				d.log.Warn("skipping scheduled run - previous run still in progress")
			}
		}
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

	// Ready endpoint - readiness check (not ready if stopping/stopped)
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		state := d.State()
		w.Header().Set("Content-Type", "application/json")

		if state == StateReady || state == StateRunning {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"ready":true,"state":"%s"}`, state.String())
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"ready":false,"state":"%s"}`, state.String())
		}
	})

	// Status endpoint - detailed status information
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		lastRun, runCount, lastErr := d.LastRun()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		errStr := ""
		if lastErr != nil {
			errStr = lastErr.Error()
		}

		lastRunStr := ""
		if !lastRun.IsZero() {
			lastRunStr = lastRun.Format(time.RFC3339)
		}

		_, _ = fmt.Fprintf(w,
			`{"state":"%s","running":%t,"last_run":"%s","last_error":"%s","run_count":%d,"schedule":"%s"}`,
			d.State().String(),
			d.IsRunning(),
			lastRunStr,
			errStr,
			runCount,
			d.schedule,
		)
	})

	// Trigger endpoint - manually trigger a run (POST only)
	mux.HandleFunc("/trigger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Use request context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		if err := d.TriggerRun(ctx); err != nil {
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprintf(w, `{"triggered":false,"error":"%s"}`, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"triggered":true}`)
	})

	d.httpServer = &http.Server{
		Addr:              d.httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			d.log.Error("HTTP server error", logger.F("error", err.Error()))
		}
	}()

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	return nil
}
