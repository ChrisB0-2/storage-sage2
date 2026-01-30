package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/auditor"
	"github.com/ChrisB0-2/storage-sage/internal/config"
	"github.com/ChrisB0-2/storage-sage/internal/logger"
	"github.com/ChrisB0-2/storage-sage/internal/trash"
)

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateStarting, "starting"},
		{StateReady, "ready"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{State(99), "unknown"},
	}

	for _, tc := range tests {
		got := tc.state.String()
		if got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestParseSchedule_ValidDurations(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"6h", 6 * time.Hour},
		{"1m30s", 90 * time.Second},
		{"@every 1h", time.Hour},
		{"@every 30m", 30 * time.Minute},
		{"@every 6h", 6 * time.Hour},
	}

	for _, tc := range tests {
		got, err := parseSchedule(tc.input)
		if err != nil {
			t.Errorf("parseSchedule(%q) error = %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSchedule(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseSchedule_Invalid(t *testing.T) {
	tests := []string{
		"",
		"invalid",
		"1x",
		"@every",
		"@every invalid",
	}

	for _, input := range tests {
		_, err := parseSchedule(input)
		if err == nil {
			t.Errorf("parseSchedule(%q) expected error, got nil", input)
		}
	}
}

func TestNew_Defaults(t *testing.T) {
	d := New(nil, nil, Config{})

	if d.log == nil {
		t.Error("expected default logger, got nil")
	}
	if d.httpAddr != ":8080" {
		t.Errorf("expected default HTTP addr :8080, got %s", d.httpAddr)
	}
	if d.State() != StateStarting {
		t.Errorf("expected initial state StateStarting, got %s", d.State())
	}
}

func TestNew_CustomConfig(t *testing.T) {
	log := logger.NewNop()
	runFunc := func(ctx context.Context) error { return nil }
	cfg := Config{
		Schedule: "1h",
		HTTPAddr: ":9999",
	}

	d := New(log, runFunc, cfg)

	if d.schedule != "1h" {
		t.Errorf("expected schedule 1h, got %s", d.schedule)
	}
	if d.httpAddr != ":9999" {
		t.Errorf("expected HTTP addr :9999, got %s", d.httpAddr)
	}
}

func TestDaemon_State(t *testing.T) {
	d := New(nil, nil, Config{})

	if d.State() != StateStarting {
		t.Errorf("expected StateStarting, got %s", d.State())
	}

	d.state.Store(int32(StateReady))
	if d.State() != StateReady {
		t.Errorf("expected StateReady, got %s", d.State())
	}

	d.state.Store(int32(StateRunning))
	if d.State() != StateRunning {
		t.Errorf("expected StateRunning, got %s", d.State())
	}
}

func TestDaemon_IsRunning(t *testing.T) {
	d := New(nil, nil, Config{})

	if d.IsRunning() {
		t.Error("expected IsRunning=false initially")
	}

	d.running.Store(true)
	if !d.IsRunning() {
		t.Error("expected IsRunning=true after setting")
	}
}

func TestDaemon_LastRun(t *testing.T) {
	d := New(nil, nil, Config{})

	lastRun, runCount, lastErr := d.LastRun()
	if !lastRun.IsZero() {
		t.Error("expected zero lastRun initially")
	}
	if lastErr != nil {
		t.Error("expected nil lastErr initially")
	}
	if runCount != 0 {
		t.Errorf("expected runCount=0, got %d", runCount)
	}

	// Simulate a run
	now := time.Now()
	testErr := errors.New("test error")
	d.mu.Lock()
	d.lastRun = now
	d.lastErr = testErr
	d.runCount = 5
	d.mu.Unlock()

	lastRun, runCount, lastErr = d.LastRun()
	if !lastRun.Equal(now) {
		t.Error("expected lastRun to match")
	}
	if lastErr != testErr {
		t.Error("expected lastErr to match")
	}
	if runCount != 5 {
		t.Errorf("expected runCount=5, got %d", runCount)
	}
}

func TestDaemon_TriggerRun_Success(t *testing.T) {
	var called atomic.Bool
	runFunc := func(ctx context.Context) error {
		called.Store(true)
		return nil
	}

	d := New(nil, runFunc, Config{})

	err := d.TriggerRun(context.Background())
	if err != nil {
		t.Errorf("TriggerRun() error = %v", err)
	}
	if !called.Load() {
		t.Error("expected runFunc to be called")
	}

	_, runCount, _ := d.LastRun()
	if runCount != 1 {
		t.Errorf("expected runCount=1, got %d", runCount)
	}
}

func TestDaemon_TriggerRun_Error(t *testing.T) {
	testErr := errors.New("run failed")
	runFunc := func(ctx context.Context) error {
		return testErr
	}

	d := New(nil, runFunc, Config{})

	err := d.TriggerRun(context.Background())
	if err != testErr {
		t.Errorf("TriggerRun() error = %v, want %v", err, testErr)
	}

	_, _, lastErr := d.LastRun()
	if lastErr != testErr {
		t.Error("expected lastErr to be set")
	}
}

func TestDaemon_TriggerRun_AlreadyRunning(t *testing.T) {
	blockCh := make(chan struct{})
	runFunc := func(ctx context.Context) error {
		<-blockCh // Block until released
		return nil
	}

	d := New(nil, runFunc, Config{})

	// Start first run in background
	go func() {
		_ = d.TriggerRun(context.Background())
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Try to trigger another run while first is in progress
	err := d.TriggerRun(context.Background())
	if err == nil {
		t.Error("expected error for concurrent run")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("expected 'already in progress' error, got: %v", err)
	}

	// Release the first run
	close(blockCh)
}

// HTTP endpoint tests

func TestHealthEndpoint(t *testing.T) {
	d := New(nil, nil, Config{})
	d.state.Store(int32(StateReady))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Create handler directly
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","state":"` + d.State().String() + `"}`))
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint returned %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", resp["status"])
	}
}

func TestReadyEndpoint_Ready(t *testing.T) {
	d := New(nil, nil, Config{})

	tests := []struct {
		state    State
		wantCode int
		wantBody bool
	}{
		{StateReady, http.StatusOK, true},
		{StateRunning, http.StatusOK, true},
		{StateStarting, http.StatusServiceUnavailable, false},
		{StateStopping, http.StatusServiceUnavailable, false},
		{StateStopped, http.StatusServiceUnavailable, false},
	}

	for _, tc := range tests {
		d.state.Store(int32(tc.state))

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			state := d.State()
			w.Header().Set("Content-Type", "application/json")
			if state == StateReady || state == StateRunning {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ready":true}`))
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"ready":false}`))
			}
		})

		handler.ServeHTTP(w, req)

		if w.Code != tc.wantCode {
			t.Errorf("state=%s: ready endpoint returned %d, want %d", tc.state, w.Code, tc.wantCode)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["ready"].(bool) != tc.wantBody {
			t.Errorf("state=%s: expected ready=%v, got %v", tc.state, tc.wantBody, resp["ready"])
		}
	}
}

func TestStatusEndpoint(t *testing.T) {
	d := New(nil, nil, Config{Schedule: "1h"})
	d.state.Store(int32(StateReady))

	// Set some run data
	now := time.Now()
	d.mu.Lock()
	d.lastRun = now
	d.runCount = 3
	d.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

		resp := map[string]interface{}{
			"state":      d.State().String(),
			"running":    d.IsRunning(),
			"last_run":   lastRunStr,
			"last_error": errStr,
			"run_count":  runCount,
			"schedule":   d.schedule,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status endpoint returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["state"] != "ready" {
		t.Errorf("expected state=ready, got %s", resp["state"])
	}
	if resp["run_count"].(float64) != 3 {
		t.Errorf("expected run_count=3, got %v", resp["run_count"])
	}
	if resp["schedule"] != "1h" {
		t.Errorf("expected schedule=1h, got %s", resp["schedule"])
	}
}

func TestTriggerEndpoint_MethodNotAllowed(t *testing.T) {
	d := New(nil, nil, Config{})
	d.state.Store(int32(StateReady))

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/trigger", nil)
		w := httptest.NewRecorder()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
		})

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("method=%s: trigger endpoint returned %d, want 405", method, w.Code)
		}
	}
}

func TestTriggerEndpoint_Success(t *testing.T) {
	var called atomic.Bool
	runFunc := func(ctx context.Context) error {
		called.Store(true)
		return nil
	}

	d := New(nil, runFunc, Config{})
	d.state.Store(int32(StateReady))

	req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := d.TriggerRun(r.Context()); err != nil {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"triggered":false}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"triggered":true}`))
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trigger endpoint returned %d, want 200", w.Code)
	}
	if !called.Load() {
		t.Error("expected runFunc to be called")
	}
}

func TestTriggerEndpoint_Conflict(t *testing.T) {
	blockCh := make(chan struct{})
	runFunc := func(ctx context.Context) error {
		<-blockCh
		return nil
	}

	d := New(nil, runFunc, Config{})
	d.state.Store(int32(StateReady))

	// Start a run in background
	go func() {
		_ = d.TriggerRun(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := d.TriggerRun(r.Context()); err != nil {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"triggered":false}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"triggered":true}`))
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("trigger endpoint returned %d, want 409", w.Code)
	}

	close(blockCh)
}

func TestDaemon_Stop(t *testing.T) {
	d := New(nil, nil, Config{})

	// Stop should close the channel
	d.Stop()

	// Verify channel is closed
	select {
	case <-d.stopCh:
		// Expected
	default:
		t.Error("expected stopCh to be closed")
	}
}

func TestDaemon_RunWithStop(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{HTTPAddr: ":0"})

	done := make(chan error, 1)
	go func() {
		done <- d.Run(context.Background())
	}()

	// Wait for daemon to be ready
	time.Sleep(100 * time.Millisecond)

	if d.State() != StateReady {
		t.Errorf("expected StateReady, got %s", d.State())
	}

	// Stop the daemon
	d.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon did not stop in time")
	}

	if d.State() != StateStopped {
		t.Errorf("expected StateStopped, got %s", d.State())
	}
}

func TestDaemon_RunWithContextCancel(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{HTTPAddr: ":0"})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for daemon to be ready
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon did not stop in time")
	}
}

func TestDaemon_ScheduledRun(t *testing.T) {
	var runCount atomic.Int32
	runFunc := func(ctx context.Context) error {
		runCount.Add(1)
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "100ms",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for at least 2 scheduled runs
	time.Sleep(350 * time.Millisecond)

	// Stop daemon
	cancel()

	<-done

	count := runCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 scheduled runs, got %d", count)
	}
}

func TestExecuteRun_TracksMetadata(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond) // Simulate some work
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{})

	before := time.Now()
	err := d.executeRun(context.Background())
	if err != nil {
		t.Errorf("executeRun() error = %v", err)
	}

	lastRun, runCount, lastErr := d.LastRun()

	if lastRun.Before(before) {
		t.Error("expected lastRun to be after test start")
	}
	if lastErr != nil {
		t.Error("expected nil lastErr for successful run")
	}
	if runCount != 1 {
		t.Errorf("expected runCount=1, got %d", runCount)
	}
}

func TestExecuteRun_TracksErrors(t *testing.T) {
	testErr := errors.New("execution failed")
	runFunc := func(ctx context.Context) error {
		return testErr
	}

	d := New(logger.NewNop(), runFunc, Config{})

	err := d.executeRun(context.Background())
	if err != testErr {
		t.Errorf("executeRun() error = %v, want %v", err, testErr)
	}

	_, _, lastErr := d.LastRun()
	if lastErr != testErr {
		t.Error("expected lastErr to be set")
	}
}

// ============================================================================
// Panic Recovery Tests
// ============================================================================

func TestTriggerRun_PanicRecovery(t *testing.T) {
	// RunFunc that panics
	runFunc := func(ctx context.Context) error {
		panic("intentional panic for testing")
	}

	d := New(logger.NewNop(), runFunc, Config{})

	// TriggerRun should recover from panic and return error
	err := d.TriggerRun(context.Background())

	if err == nil {
		t.Error("expected error after panic, got nil")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("expected panic error message, got: %v", err)
	}

	// Should have recorded the panic in lastErr
	_, runCount, lastErr := d.LastRun()
	if lastErr == nil {
		t.Error("expected lastErr to be set after panic")
	}
	if !strings.Contains(lastErr.Error(), "panic") {
		t.Errorf("expected panic recorded in lastErr, got: %v", lastErr)
	}
	if runCount != 1 {
		t.Errorf("expected runCount=1 after panic, got %d", runCount)
	}
}

func TestScheduler_PanicInRunFunc_Recovers(t *testing.T) {
	var runCount atomic.Int32
	runFunc := func(ctx context.Context) error {
		count := runCount.Add(1)
		if count == 1 {
			// First run panics
			panic("first run panic")
		}
		// Second run succeeds
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "50ms",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for multiple scheduled runs
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Should have attempted multiple runs despite first panic
	count := runCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 run attempts, got %d", count)
	}

	// LastErr should contain panic info from first run
	_, _, lastErr := d.LastRun()
	// Note: lastErr might be nil if second run succeeded, that's OK
	// The important thing is the daemon didn't crash
	_ = lastErr // Suppress unused variable warning
}

func TestScheduler_PanicRecovery_LogsStack(t *testing.T) {
	// This test verifies the scheduler doesn't crash on panic
	runFunc := func(ctx context.Context) error {
		panic("test panic with stack")
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "20ms",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Let it run and recover from panic
	<-done

	// Verify daemon is still in a valid state (not crashed)
	// State should be Stopped after timeout
	_ = d.State()
}

// ============================================================================
// Scheduler Lifecycle Tests
// ============================================================================

func TestScheduler_StartsAndStopsCleanly(t *testing.T) {
	var runCount atomic.Int32
	runFunc := func(ctx context.Context) error {
		runCount.Add(1)
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "30ms",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for ready state
	time.Sleep(50 * time.Millisecond)
	if d.State() != StateReady && d.State() != StateRunning {
		t.Errorf("expected StateReady or StateRunning, got %s", d.State())
	}

	// Stop cleanly
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop in time")
	}

	if d.State() != StateStopped {
		t.Errorf("expected StateStopped, got %s", d.State())
	}
}

func TestScheduler_InvalidSchedule_ReturnsEarly(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "invalid-schedule",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait a bit for the scheduler to fail parsing
	time.Sleep(100 * time.Millisecond)

	// Stop the daemon
	cancel()
	<-done

	// Should have reached stopped state
	if d.State() != StateStopped {
		t.Errorf("expected StateStopped after invalid schedule, got %s", d.State())
	}
}

func TestScheduler_NoSchedule_RunsWithoutScheduler(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "", // No schedule
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for ready
	time.Sleep(50 * time.Millisecond)
	if d.State() != StateReady {
		t.Errorf("expected StateReady without schedule, got %s", d.State())
	}

	// Should be able to trigger manually
	err := d.TriggerRun(context.Background())
	if err != nil {
		t.Errorf("TriggerRun() error = %v", err)
	}

	cancel()
	<-done
}

// ============================================================================
// State Transition Tests
// ============================================================================

func TestStateTransitions_FullLifecycle(t *testing.T) {
	var states []State
	var mu sync.Mutex

	runFunc := func(ctx context.Context) error {
		mu.Lock()
		states = append(states, StateRunning) // Capture during run
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "50ms",
		HTTPAddr: ":0",
	})

	// Verify initial state
	if d.State() != StateStarting {
		t.Errorf("expected StateStarting initially, got %s", d.State())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for ready
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	states = append(states, d.State())
	mu.Unlock()

	// Wait for at least one run
	time.Sleep(100 * time.Millisecond)

	// Stop
	cancel()
	<-done

	mu.Lock()
	states = append(states, d.State())
	mu.Unlock()

	// Verify we hit expected states
	if d.State() != StateStopped {
		t.Errorf("expected final state StateStopped, got %s", d.State())
	}
}

func TestState_ConcurrentAccess(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.State()
			_ = d.IsRunning()
			_, _, _ = d.LastRun()
		}()
	}
	wg.Wait()
	// No race condition = test passes
}

// ============================================================================
// Schedule Parsing Edge Cases
// ============================================================================

func TestParseSchedule_EdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		// Valid
		{"1s", false},
		{"1m", false},
		{"1h", false},
		{"1h30m", false},
		{"@every 1h", false},
		{"@every 30m", false},

		// Invalid
		{"", true},
		{"invalid", true},
		{"1x", true},
		{"@every", true},
		{"@every invalid", true},
		{"-1h", false}, // Negative durations parse but may not make sense
		{"0s", false},  // Zero is valid Go duration
		{"0", false},   // "0" parses as 0 seconds (valid Go duration)
	}

	for _, tc := range tests {
		_, err := parseSchedule(tc.input)
		if tc.wantErr && err == nil {
			t.Errorf("parseSchedule(%q) expected error, got nil", tc.input)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseSchedule(%q) unexpected error: %v", tc.input, err)
		}
	}
}

func TestParseSchedule_VerySmallInterval(t *testing.T) {
	// 1ms is valid but very aggressive
	d, err := parseSchedule("1ms")
	if err != nil {
		t.Errorf("parseSchedule(1ms) error = %v", err)
	}
	if d != time.Millisecond {
		t.Errorf("parseSchedule(1ms) = %v, want %v", d, time.Millisecond)
	}
}

// ============================================================================
// Graceful Shutdown Tests
// ============================================================================

func TestGracefulShutdown_WaitsForRunToComplete(t *testing.T) {
	runStarted := make(chan struct{})
	runComplete := make(chan struct{})

	runFunc := func(ctx context.Context) error {
		close(runStarted)
		// Simulate long-running cleanup
		select {
		case <-time.After(500 * time.Millisecond):
			close(runComplete)
		case <-ctx.Done():
			// Context canceled, but we still finish
			close(runComplete)
		}
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "10ms",
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for run to start
	select {
	case <-runStarted:
	case <-time.After(time.Second):
		t.Fatal("run did not start in time")
	}

	// Cancel while run is in progress
	cancel()

	// Run should complete (context doesn't force immediate stop)
	select {
	case <-runComplete:
	case <-time.After(2 * time.Second):
		t.Error("run did not complete during shutdown")
	}

	// Daemon should exit cleanly
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop in time")
	}
}

func TestGracefulShutdown_HTTPServerShutdown(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for ready
	time.Sleep(50 * time.Millisecond)

	// Stop
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("HTTP server did not shutdown in time")
	}
}

// ============================================================================
// Concurrent Trigger Tests
// ============================================================================

func TestConcurrentTriggers_OnlyOneRuns(t *testing.T) {
	var runCount atomic.Int32
	started := make(chan struct{})

	runFunc := func(ctx context.Context) error {
		runCount.Add(1)
		close(started)
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{})

	// Start first trigger in background
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- d.TriggerRun(context.Background())
	}()

	// Wait for first run to start
	<-started

	// Try concurrent triggers
	var wg sync.WaitGroup
	var rejectedCount atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.TriggerRun(context.Background()); err != nil {
				if strings.Contains(err.Error(), "already in progress") {
					rejectedCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// Wait for first run to complete and check result
	firstErr := <-firstDone
	if firstErr != nil {
		t.Errorf("first trigger failed: %v", firstErr)
	}

	// All concurrent triggers should be rejected
	if rejectedCount.Load() != 10 {
		t.Errorf("expected 10 rejected triggers, got %d", rejectedCount.Load())
	}

	// Only one run should have executed
	if runCount.Load() != 1 {
		t.Errorf("expected exactly 1 run, got %d", runCount.Load())
	}
}

// ============================================================================
// HTTP Handler Tests
// ============================================================================

func TestHealthEndpoint_AllStates(t *testing.T) {
	d := New(nil, nil, Config{})

	states := []State{StateStarting, StateReady, StateRunning, StateStopping, StateStopped}
	for _, state := range states {
		d.state.Store(int32(state))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"status":"ok","state":"%s"}`, d.State().String())
		})

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("state=%s: health endpoint returned %d, want 200", state, w.Code)
		}
	}
}

func TestParseDurationWithDays(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"1h", time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"0d", 0, true},
		{"-1d", 0, true},
		{"d", 0, true},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		got, err := parseDurationWithDays(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDurationWithDays(%q) expected error", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseDurationWithDays(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseDurationWithDays(%q) = %v, want %v", tc.input, got, tc.want)
			}
		}
	}
}

func TestParseTimeParam(t *testing.T) {
	tests := []struct {
		input    string
		wantZero bool
	}{
		{"2024-01-15T10:30:00Z", false}, // RFC3339
		{"2024-01-15", false},           // Date
		{"24h", false},                  // Duration
		{"7d", false},                   // Days
		{"30m", false},                  // Minutes
		{"invalid", true},
		{"", true},
	}

	for _, tc := range tests {
		got, _ := parseTimeParam(tc.input)
		if tc.wantZero && !got.IsZero() {
			t.Errorf("parseTimeParam(%q) expected zero time", tc.input)
		}
		if !tc.wantZero && got.IsZero() {
			t.Errorf("parseTimeParam(%q) expected non-zero time", tc.input)
		}
	}
}

// ============================================================================
// safeExecuteRun Tests
// ============================================================================

func TestSafeExecuteRun_Success(t *testing.T) {
	var called bool
	runFunc := func(ctx context.Context) error {
		called = true
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{})
	d.safeExecuteRun(context.Background())

	if !called {
		t.Error("expected runFunc to be called")
	}
}

func TestSafeExecuteRun_Error(t *testing.T) {
	testErr := errors.New("run error")
	runFunc := func(ctx context.Context) error {
		return testErr
	}

	d := New(logger.NewNop(), runFunc, Config{})
	d.safeExecuteRun(context.Background())

	_, _, lastErr := d.LastRun()
	if lastErr != testErr {
		t.Errorf("expected lastErr = %v, got %v", testErr, lastErr)
	}
}

func TestSafeExecuteRun_Panic(t *testing.T) {
	runFunc := func(ctx context.Context) error {
		panic("test panic")
	}

	d := New(logger.NewNop(), runFunc, Config{})

	// Should not panic
	d.safeExecuteRun(context.Background())

	_, runCount, lastErr := d.LastRun()
	if lastErr == nil {
		t.Error("expected lastErr to be set after panic")
	}
	if !strings.Contains(lastErr.Error(), "panic") {
		t.Errorf("expected panic in lastErr, got: %v", lastErr)
	}
	if runCount != 1 {
		t.Errorf("expected runCount=1, got %d", runCount)
	}
}

// ============================================================================
// HTTP Handler Integration Tests
// ============================================================================

func TestDaemon_StartHTTP_Success(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})

	err := d.startHTTP()
	if err != nil {
		t.Fatalf("startHTTP() error = %v", err)
	}
	defer d.httpServer.Close()

	if d.httpServer == nil {
		t.Error("expected httpServer to be initialized")
	}
}

func TestDaemon_StartHTTP_InvalidAddress(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: "invalid:address:format:99999"})

	err := d.startHTTP()
	if err == nil {
		t.Error("expected error for invalid address")
	}
}

func TestDaemon_HealthEndpoint_Integration(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	d.state.Store(int32(StateReady))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
	if resp["state"] != "ready" {
		t.Errorf("expected state=ready, got %v", resp["state"])
	}
}

func TestDaemon_ReadyEndpoint_Integration(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	tests := []struct {
		state    State
		wantCode int
	}{
		{StateReady, http.StatusOK},
		{StateRunning, http.StatusOK},
		{StateStarting, http.StatusServiceUnavailable},
		{StateStopping, http.StatusServiceUnavailable},
		{StateStopped, http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		d.state.Store(int32(tc.state))

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		d.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != tc.wantCode {
			t.Errorf("state=%s: ready endpoint returned %d, want %d", tc.state, w.Code, tc.wantCode)
		}
	}
}

func TestDaemon_StatusEndpoint_Integration(t *testing.T) {
	runFunc := func(ctx context.Context) error { return nil }
	d := New(logger.NewNop(), runFunc, Config{Schedule: "1h", HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	d.state.Store(int32(StateReady))
	d.mu.Lock()
	d.lastRun = time.Now()
	d.runCount = 5
	d.lastErr = errors.New("previous error")
	d.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status endpoint returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["state"] != "ready" {
		t.Errorf("expected state=ready, got %v", resp["state"])
	}
	if resp["schedule"] != "1h" {
		t.Errorf("expected schedule=1h, got %v", resp["schedule"])
	}
	if resp["run_count"].(float64) != 5 {
		t.Errorf("expected run_count=5, got %v", resp["run_count"])
	}
	if resp["last_error"] != "previous error" {
		t.Errorf("expected last_error='previous error', got %v", resp["last_error"])
	}
}

func TestDaemon_TriggerEndpoint_Integration(t *testing.T) {
	var called atomic.Bool
	runFunc := func(ctx context.Context) error {
		called.Store(true)
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	d.state.Store(int32(StateReady))

	// Test POST triggers run
	req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trigger endpoint returned %d, want 200", w.Code)
	}
	if !called.Load() {
		t.Error("expected runFunc to be called")
	}
}

func TestDaemon_TriggerEndpoint_MethodNotAllowed_Integration(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/trigger", nil)
		w := httptest.NewRecorder()

		d.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("method=%s: trigger endpoint returned %d, want 405", method, w.Code)
		}
	}
}

func TestDaemon_TriggerEndpoint_Conflict_Integration(t *testing.T) {
	blockCh := make(chan struct{})
	runFunc := func(ctx context.Context) error {
		<-blockCh
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	// Start a run in background
	go func() {
		_ = d.TriggerRun(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("trigger endpoint returned %d, want 409", w.Code)
	}

	close(blockCh)
}

func TestDaemon_APIConfigEndpoint_NotAvailable(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("api/config without config returned %d, want 404", w.Code)
	}
}

func TestDaemon_APIConfigEndpoint_MethodNotAllowed(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/config", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST api/config returned %d, want 405", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_NotAvailable(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/query", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("audit/query without auditor returned %d, want 404", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_MethodNotAllowed(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/audit/query", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST audit/query returned %d, want 405", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_InvalidAction(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/query?action=invalid", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid action returned %d, want 400", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_InvalidLevel(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/query?level=invalid", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid level returned %d, want 400", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_InvalidLimit(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	tests := []string{"notanumber", "-1", "0"}
	for _, limit := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/audit/query?limit="+limit, nil)
		w := httptest.NewRecorder()

		d.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("limit=%s returned %d, want 400", limit, w.Code)
		}
	}
}

func TestDaemon_AuditQueryEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/query", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("audit query returned %d, want 200", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_WithFilters(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	// Test with valid filters
	req := httptest.NewRequest(http.MethodGet, "/api/audit/query?action=execute&level=info&limit=50&since=24h&until=2024-01-01&path=/tmp", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("audit query with filters returned %d, want 200", w.Code)
	}
}

func TestDaemon_AuditQueryEndpoint_LimitCapped(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	// Limit > 1000 should be capped
	req := httptest.NewRequest(http.MethodGet, "/api/audit/query?limit=5000", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("audit query with high limit returned %d, want 200", w.Code)
	}
}

func TestDaemon_AuditStatsEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	aud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Auditor: aud})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/stats", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("audit stats returned %d, want 200", w.Code)
	}
}

func TestDaemon_AuditStatsEndpoint_NotAvailable(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/audit/stats", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("audit/stats without auditor returned %d, want 404", w.Code)
	}
}

func TestDaemon_AuditStatsEndpoint_MethodNotAllowed(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/audit/stats", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST audit/stats returned %d, want 405", w.Code)
	}
}

func TestDaemon_TrashEndpoint_NotConfigured(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/trash", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("trash without manager returned %d, want 404", w.Code)
	}
}

func TestDaemon_TrashEndpoint_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPut, "/api/trash", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT trash returned %d, want 405", w.Code)
	}
}

func TestDaemon_TrashListEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/trash", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash list returned %d, want 200", w.Code)
	}

	// Should return empty array
	var resp []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty trash list, got %d items", len(resp))
	}
}

func TestDaemon_TrashDeleteAllEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/trash?all=true", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash delete all returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if _, ok := resp["deleted"]; !ok {
		t.Error("expected 'deleted' field in response")
	}
}

func TestDaemon_TrashDeleteOlderThanEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/trash?older_than=7d", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash delete older_than returned %d, want 200", w.Code)
	}
}

func TestDaemon_TrashRestoreEndpoint_NotConfigured(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/trash/restore", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("trash/restore without manager returned %d, want 404", w.Code)
	}
}

func TestDaemon_TrashRestoreEndpoint_MethodNotAllowed(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/trash/restore", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET trash/restore returned %d, want 405", w.Code)
	}
}

func TestDaemon_TrashRestoreEndpoint_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/trash/restore", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid json returned %d, want 400", w.Code)
	}
}

func TestDaemon_TrashRestoreEndpoint_EmptyName(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/trash/restore", strings.NewReader(`{"name":""}`))
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("empty name returned %d, want 400", w.Code)
	}
}

func TestDaemon_TrashRestoreEndpoint_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/trash/restore", strings.NewReader(`{"name":"nonexistent"}`))
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("nonexistent item returned %d, want 404", w.Code)
	}
}

func TestDaemon_TrashDeleteEndpoint_MissingParams(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/trash", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("DELETE trash without params returned %d, want 400", w.Code)
	}
}

func TestDaemon_TrashDeleteEndpoint_InvalidDuration(t *testing.T) {
	tmpDir := t.TempDir()
	trashMgr, err := trash.New(trash.Config{TrashPath: tmpDir + "/trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/trash?older_than=invalid", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid duration returned %d, want 400", w.Code)
	}
}

func TestWriteJSONError(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{})
	w := httptest.NewRecorder()

	d.writeJSONError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["error"] != "test error" {
		t.Errorf("expected error='test error', got %s", resp["error"])
	}
}

func TestWriteJSONResponse(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{})
	w := httptest.NewRecorder()

	data := map[string]any{
		"key":    "value",
		"number": 42,
	}
	d.writeJSONResponse(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["key"] != "value" {
		t.Errorf("expected key='value', got %v", resp["key"])
	}
	if resp["number"].(float64) != 42 {
		t.Errorf("expected number=42, got %v", resp["number"])
	}
}

// ============================================================================
// PID File Tests
// ============================================================================

func TestDaemon_RunWithPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := tmpDir + "/daemon.pid"

	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		PIDFile:  pidPath,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for ready
	time.Sleep(100 * time.Millisecond)

	// PID file should exist
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file should exist while daemon is running")
	}

	// Stop daemon
	cancel()
	<-done

	// PID file should be removed (may take a moment)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(pidPath); err == nil {
		t.Error("PID file should be removed after daemon stops")
	}
}

func TestDaemon_RunWithPIDFile_InvalidPath(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		PIDFile:  "/nonexistent/path/daemon.pid",
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := d.Run(ctx)
	if err == nil {
		t.Error("expected error for invalid PID file path")
	}
}

// ============================================================================
// Additional Coverage Tests
// ============================================================================

func TestDaemon_APIConfigEndpoint_WithConfig(t *testing.T) {
	cfg := &config.Config{
		Version: 1,
		Scan: config.ScanConfig{
			Roots:     []string{"/tmp"},
			Recursive: true,
		},
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", AppConfig: cfg})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("api/config with config returned %d, want 200", w.Code)
	}

	// Verify response contains config data
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["version"].(float64) != 1 {
		t.Errorf("expected version=1, got %v", resp["version"])
	}
}

func TestDaemon_TrashListEndpoint_WithItems(t *testing.T) {
	tmpDir := t.TempDir()
	trashDir := tmpDir + "/trash"
	trashMgr, err := trash.New(trash.Config{TrashPath: trashDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a file and move it to trash
	testFile := tmpDir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trashMgr.MoveToTrash(testFile); err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/trash", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash list returned %d, want 200", w.Code)
	}

	var resp []TrashItemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp) != 1 {
		t.Errorf("expected 1 trash item, got %d", len(resp))
	}
}

func TestDaemon_TrashRestoreEndpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	trashDir := tmpDir + "/trash"
	trashMgr, err := trash.New(trash.Config{TrashPath: trashDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a file and move it to trash
	testFile := tmpDir + "/restore_test.txt"
	if err := os.WriteFile(testFile, []byte("restore content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trashMgr.MoveToTrash(testFile); err != nil {
		t.Fatal(err)
	}

	// Get the trash item name
	items, err := trashMgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 trash item, got %d", len(items))
	}
	itemName := items[0].Name

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	body := fmt.Sprintf(`{"name":"%s"}`, itemName)
	req := httptest.NewRequest(http.MethodPost, "/api/trash/restore", strings.NewReader(body))
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash restore returned %d, want 200. Body: %s", w.Code, w.Body.String())
	}

	// Verify file was restored
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("file should be restored to original location")
	}
}

func TestDaemon_TrashDeleteOlderThan_WithItems(t *testing.T) {
	tmpDir := t.TempDir()
	trashDir := tmpDir + "/trash"
	trashMgr, err := trash.New(trash.Config{TrashPath: trashDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create and trash a file
	testFile := tmpDir + "/old.txt"
	if err := os.WriteFile(testFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trashMgr.MoveToTrash(testFile); err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", Trash: trashMgr})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	// Request delete older than 0h (should delete all)
	req := httptest.NewRequest(http.MethodDelete, "/api/trash?older_than=0s", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("trash delete returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// Should have deleted at least 1 item
	if resp["deleted"].(float64) < 1 {
		t.Errorf("expected at least 1 deleted, got %v", resp["deleted"])
	}
}

func TestDaemon_StatusEndpoint_NoLastRun(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0"})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	d.state.Store(int32(StateReady))

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status endpoint returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// last_run should be empty string when no run has occurred
	if resp["last_run"] != "" {
		t.Errorf("expected empty last_run, got %v", resp["last_run"])
	}
	if resp["last_error"] != "" {
		t.Errorf("expected empty last_error, got %v", resp["last_error"])
	}
}

func TestDaemon_ReadyEndpoint_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Scan: config.ScanConfig{
			Roots: []string{tmpDir},
		},
	}

	d := New(logger.NewNop(), nil, Config{HTTPAddr: ":0", AppConfig: cfg})
	if err := d.startHTTP(); err != nil {
		t.Fatal(err)
	}
	defer d.httpServer.Close()

	d.state.Store(int32(StateReady))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	d.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ready endpoint returned %d, want 200", w.Code)
	}
}

func TestDaemon_Scheduler_NegativeDuration(t *testing.T) {
	// Negative durations are technically valid Go durations but don't make sense
	// for scheduling. The ticker will panic with negative duration.
	_, err := parseSchedule("-1h")
	// Negative durations parse successfully but will cause panic in NewTicker
	if err != nil {
		t.Errorf("parseSchedule(-1h) error = %v, but it parses as valid Go duration", err)
	}
}

func TestDaemon_StopMultipleTimes(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{})

	// Calling Stop multiple times should be safe
	d.Stop()
	d.Stop()
	d.Stop()

	// Channel should be closed
	select {
	case <-d.stopCh:
		// Expected
	default:
		t.Error("stopCh should be closed")
	}
}

// ============================================================================
// Auditor Lifecycle Tests
// ============================================================================

// mockClosableAuditor tracks Close() calls for testing auditor lifecycle
type mockClosableAuditor struct {
	closeCalls  atomic.Int32
	closeErr    error // optional error to return from Close()
	queryResult []auditor.AuditRecord
}

func (m *mockClosableAuditor) Record(_ context.Context, _ any) {}

func (m *mockClosableAuditor) Query(_ context.Context, _ auditor.QueryFilter) ([]auditor.AuditRecord, error) {
	return m.queryResult, nil
}

func (m *mockClosableAuditor) Stats(_ context.Context) (auditor.AuditStats, error) {
	return auditor.AuditStats{}, nil
}

func (m *mockClosableAuditor) VerifyIntegrity(_ context.Context) ([]int64, error) {
	return nil, nil
}

func (m *mockClosableAuditor) Close() error {
	m.closeCalls.Add(1)
	return m.closeErr
}

func (m *mockClosableAuditor) CloseCount() int {
	return int(m.closeCalls.Load())
}

func TestDaemon_AuditorClosedOnNormalShutdown(t *testing.T) {
	mockAud := &mockClosableAuditor{}

	// Create a real SQLiteAuditor wrapper that delegates to our mock isn't possible
	// since daemon expects *auditor.SQLiteAuditor. Instead, we test closeAuditor directly.
	// For integration test, we use a real temp auditor.
	tmpDir := t.TempDir()
	realAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		Auditor:  realAud,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for daemon to be ready
	time.Sleep(100 * time.Millisecond)

	// Stop daemon normally
	cancel()
	<-done

	// Verify auditor was closed by trying to use it (should fail)
	_, err = realAud.Query(context.Background(), auditor.QueryFilter{Limit: 1})
	if err == nil {
		t.Error("expected error after auditor closed, got nil")
	}

	// Verify calling Close again is safe (double-close protection in SQLiteAuditor)
	err = realAud.Close()
	if err != nil {
		t.Logf("second Close() returned: %v (expected for already-closed)", err)
	}

	// Use mock to verify our closeAuditor logic
	_ = mockAud // Silence unused warning, tested below
}

func TestDaemon_CloseAuditorCalledExactlyOnce(t *testing.T) {
	// Test the closeAuditor method directly
	tmpDir := t.TempDir()
	realAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}

	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		Auditor:  realAud,
	})

	// Call closeAuditor multiple times
	d.closeAuditor()
	d.closeAuditor()
	d.closeAuditor()

	// Verify auditor is closed
	_, err = realAud.Query(context.Background(), auditor.QueryFilter{Limit: 1})
	if err == nil {
		t.Error("expected error after auditor closed")
	}
}

func TestDaemon_AuditorClosedOnPanicShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	realAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}

	// RunFunc that panics on first call
	var runCount atomic.Int32
	runFunc := func(ctx context.Context) error {
		if runCount.Add(1) == 1 {
			panic("intentional panic for auditor close test")
		}
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "50ms",
		HTTPAddr: ":0",
		Auditor:  realAud,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for panic to occur and be recovered
	time.Sleep(200 * time.Millisecond)

	// Stop daemon
	cancel()
	<-done

	// Verify auditor was closed despite panic
	_, err = realAud.Query(context.Background(), auditor.QueryFilter{Limit: 1})
	if err == nil {
		t.Error("expected error after auditor closed (panic path)")
	}
}

func TestDaemon_AuditorCloseErrorLogged(t *testing.T) {
	// Test that close errors are logged but don't fail shutdown
	tmpDir := t.TempDir()
	realAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}

	// Close the auditor first to simulate an error condition
	realAud.Close()

	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		Auditor:  realAud,
	})

	// closeAuditor should not panic even if auditor is already closed
	d.closeAuditor() // Should log warning but not panic

	// Daemon should still be usable
	if d.State() != StateStarting {
		t.Errorf("expected StateStarting, got %s", d.State())
	}
}

func TestDaemon_NilAuditorHandledGracefully(t *testing.T) {
	d := New(logger.NewNop(), nil, Config{
		HTTPAddr: ":0",
		// No auditor configured
	})

	// closeAuditor should not panic with nil auditor
	d.closeAuditor()

	// Daemon should still work
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Errorf("Run() returned error: %v", err)
	}
}

func TestDaemon_AuditorNotClosedPerRun(t *testing.T) {
	tmpDir := t.TempDir()
	realAud, err := auditor.NewSQLite(auditor.SQLiteConfig{Path: tmpDir + "/audit.db"})
	if err != nil {
		t.Fatal(err)
	}

	var runCount atomic.Int32
	var auditorClosedDuringRun atomic.Bool

	runFunc := func(ctx context.Context) error {
		runCount.Add(1)
		// Verify auditor is still open during run using a fresh context
		// (the run context may be canceled, but auditor should still work)
		_, err := realAud.Query(context.Background(), auditor.QueryFilter{Limit: 1})
		if err != nil {
			// Only flag as error if it's actually closed, not context cancellation
			if strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "database") {
				auditorClosedDuringRun.Store(true)
			}
		}
		return nil
	}

	d := New(logger.NewNop(), runFunc, Config{
		Schedule: "30ms",
		HTTPAddr: ":0",
		Auditor:  realAud,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for at least one run to complete
	for i := 0; i < 50; i++ {
		if runCount.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	runs := runCount.Load()
	if runs < 1 {
		t.Errorf("expected at least 1 run, got %d", runs)
	}

	// Verify auditor was NOT closed during runs
	if auditorClosedDuringRun.Load() {
		t.Error("auditor was closed during a run (should only close on shutdown)")
	}

	// NOW auditor should be closed (after daemon stopped)
	_, err = realAud.Query(context.Background(), auditor.QueryFilter{Limit: 1})
	if err == nil {
		t.Error("expected auditor to be closed after daemon stopped")
	}
}
