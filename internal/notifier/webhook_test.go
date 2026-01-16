package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhook_Notify(t *testing.T) {
	var received WebhookPayload
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := WebhookConfig{
		URL: server.URL,
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	}
	webhook := NewWebhook(cfg)

	payload := WebhookPayload{
		Event:     EventCleanupCompleted,
		Timestamp: time.Now(),
		Message:   "Cleanup finished successfully",
		Summary: &CleanupSummary{
			Root:         "/tmp",
			Mode:         "execute",
			FilesDeleted: 10,
			BytesFreed:   1024 * 1024,
			Duration:     "5s",
		},
	}

	err := webhook.Notify(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify payload
	if received.Event != EventCleanupCompleted {
		t.Errorf("expected event %s, got %s", EventCleanupCompleted, received.Event)
	}
	if received.Summary.FilesDeleted != 10 {
		t.Errorf("expected 10 files deleted, got %d", received.Summary.FilesDeleted)
	}

	// Verify headers
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type: application/json")
	}
	if receivedHeaders.Get("X-Custom-Header") != "test-value" {
		t.Errorf("expected custom header")
	}
}

func TestWebhook_NotifyFiltersEvents(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := WebhookConfig{
		URL:    server.URL,
		Events: []EventType{EventCleanupCompleted, EventCleanupFailed},
	}
	webhook := NewWebhook(cfg)

	// Should send - event is in list
	err := webhook.Notify(context.Background(), WebhookPayload{Event: EventCleanupCompleted})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Should not send - event is not in list
	err = webhook.Notify(context.Background(), WebhookPayload{Event: EventCleanupStarted})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (unchanged), got %d", callCount)
	}

	// Should send - event is in list
	err = webhook.Notify(context.Background(), WebhookPayload{Event: EventCleanupFailed})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestWebhook_NotifyHandlesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	webhook := NewWebhook(WebhookConfig{URL: server.URL})

	err := webhook.Notify(context.Background(), WebhookPayload{Event: EventCleanupCompleted})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestMultiNotifier(t *testing.T) {
	var calls []string

	notifier1 := &mockNotifier{id: "n1", calls: &calls}
	notifier2 := &mockNotifier{id: "n2", calls: &calls}

	multi := NewMultiNotifier(notifier1, notifier2)

	err := multi.Notify(context.Background(), WebhookPayload{Event: EventCleanupCompleted})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(calls))
	}
	if calls[0] != "n1" || calls[1] != "n2" {
		t.Errorf("expected calls from n1 and n2, got %v", calls)
	}
}

type mockNotifier struct {
	id    string
	calls *[]string
}

func (m *mockNotifier) Notify(ctx context.Context, payload WebhookPayload) error {
	*m.calls = append(*m.calls, m.id)
	return nil
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1536 * 1024 * 1024, "1.5 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.input)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestSlackPayload(t *testing.T) {
	payload := WebhookPayload{
		Event:     EventCleanupCompleted,
		Timestamp: time.Now(),
		Summary: &CleanupSummary{
			Root:         "/tmp",
			Mode:         "execute",
			FilesDeleted: 5,
			BytesFreed:   1024 * 1024 * 100,
			Duration:     "10s",
		},
	}

	slack := SlackPayload(payload)

	attachments, ok := slack["attachments"].([]map[string]interface{})
	if !ok || len(attachments) == 0 {
		t.Fatal("expected attachments")
	}

	if attachments[0]["color"] != "good" {
		t.Errorf("expected color 'good' for successful cleanup")
	}

	// Test with errors
	payload.Summary.Errors = 2
	slack = SlackPayload(payload)
	attachments = slack["attachments"].([]map[string]interface{})
	if attachments[0]["color"] != "warning" {
		t.Errorf("expected color 'warning' for cleanup with errors")
	}
}
