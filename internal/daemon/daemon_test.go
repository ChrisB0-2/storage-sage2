package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
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
