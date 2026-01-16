package metrics

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewServer_DefaultAddress(t *testing.T) {
	s := NewServer("")
	if s.Addr() != ":9090" {
		t.Errorf("expected default address ':9090', got %q", s.Addr())
	}
}

func TestNewServer_CustomAddress(t *testing.T) {
	s := NewServer(":8080")
	if s.Addr() != ":8080" {
		t.Errorf("expected address ':8080', got %q", s.Addr())
	}
}

func TestServer_HealthEndpoint(t *testing.T) {
	// Find available port
	addr := getAvailableAddr(t)
	s := NewServer(addr)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	// Wait for server to start
	waitForServer(t, "http://"+addr+"/health")

	// Test health endpoint
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}

	// Check server returned nil (graceful shutdown)
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected nil error from graceful shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down in time")
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	addr := getAvailableAddr(t)
	s := NewServer(addr)

	// Start server
	go func() {
		_ = s.Start()
	}()

	waitForServer(t, "http://"+addr+"/health")

	// Test metrics endpoint
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", ct)
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}

func TestServer_MultipleShutdown(t *testing.T) {
	addr := getAvailableAddr(t)
	s := NewServer(addr)

	// Start server
	go func() {
		_ = s.Start()
	}()

	waitForServer(t, "http://"+addr+"/health")

	// First shutdown should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("first shutdown failed: %v", err)
	}

	// Second shutdown on already-closed server
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	// Should not panic or fail catastrophically
	_ = s.Shutdown(ctx2)
}

// getAvailableAddr finds an available port for testing
func getAvailableAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// waitForServer polls until the server responds or times out
func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not start in time", url)
}
