package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event types for notifications
type EventType string

const (
	EventCleanupStarted   EventType = "cleanup_started"
	EventCleanupCompleted EventType = "cleanup_completed"
	EventCleanupFailed    EventType = "cleanup_failed"
	EventDaemonStarted    EventType = "daemon_started"
	EventDaemonStopped    EventType = "daemon_stopped"
)

// CleanupSummary contains statistics from a cleanup run
type CleanupSummary struct {
	Root          string    `json:"root"`
	Mode          string    `json:"mode"`
	FilesScanned  int       `json:"files_scanned"`
	FilesDeleted  int       `json:"files_deleted"`
	BytesFreed    int64     `json:"bytes_freed"`
	Errors        int       `json:"errors"`
	Duration      string    `json:"duration"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	ErrorMessages []string  `json:"error_messages,omitempty"`
}

// WebhookPayload is the JSON payload sent to webhook endpoints
type WebhookPayload struct {
	Event     EventType       `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	Hostname  string          `json:"hostname,omitempty"`
	Summary   *CleanupSummary `json:"summary,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// WebhookConfig configures a webhook notification endpoint
type WebhookConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Events  []EventType       `yaml:"events,omitempty"` // Empty = all events
	Timeout time.Duration     `yaml:"timeout,omitempty"`
}

// Webhook sends notifications to HTTP endpoints
type Webhook struct {
	config WebhookConfig
	client *http.Client
}

// NewWebhook creates a new webhook notifier
func NewWebhook(cfg WebhookConfig) *Webhook {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &Webhook{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// Notify sends a notification to the webhook endpoint
func (w *Webhook) Notify(ctx context.Context, payload WebhookPayload) error {
	// Check if we should send this event type
	if !w.shouldNotify(payload.Event) {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "storage-sage/1.0")

	// Add custom headers
	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (w *Webhook) shouldNotify(event EventType) bool {
	// Empty events list means notify for all events
	if len(w.config.Events) == 0 {
		return true
	}

	for _, e := range w.config.Events {
		if e == event {
			return true
		}
	}
	return false
}

// Notifier is the interface for sending notifications
type Notifier interface {
	Notify(ctx context.Context, payload WebhookPayload) error
}

// MultiNotifier sends notifications to multiple endpoints
type MultiNotifier struct {
	mu        sync.RWMutex
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that sends to multiple endpoints
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Notify sends to all configured notifiers, collecting errors
func (m *MultiNotifier) Notify(ctx context.Context, payload WebhookPayload) error {
	m.mu.RLock()
	notifiers := make([]Notifier, len(m.notifiers))
	copy(notifiers, m.notifiers)
	m.mu.RUnlock()

	var errs []error
	for _, n := range notifiers {
		if err := n.Notify(ctx, payload); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %v", errs)
	}
	return nil
}

// Add adds a notifier to the multi-notifier
func (m *MultiNotifier) Add(n Notifier) {
	m.mu.Lock()
	m.notifiers = append(m.notifiers, n)
	m.mu.Unlock()
}

// NoopNotifier does nothing (for when notifications are disabled)
type NoopNotifier struct{}

func (n *NoopNotifier) Notify(ctx context.Context, payload WebhookPayload) error {
	return nil
}

// SlackPayload formats a webhook payload for Slack
func SlackPayload(payload WebhookPayload) map[string]interface{} {
	var color, title string
	switch payload.Event {
	case EventCleanupCompleted:
		if payload.Summary != nil && payload.Summary.Errors > 0 {
			color = "warning"
			title = "Storage-Sage Cleanup Completed with Errors"
		} else {
			color = "good"
			title = "Storage-Sage Cleanup Completed"
		}
	case EventCleanupFailed:
		color = "danger"
		title = "Storage-Sage Cleanup Failed"
	case EventCleanupStarted:
		color = "#439FE0"
		title = "Storage-Sage Cleanup Started"
	default:
		color = "#808080"
		title = fmt.Sprintf("Storage-Sage: %s", payload.Event)
	}

	fields := []map[string]interface{}{}

	if payload.Summary != nil {
		fields = append(fields,
			map[string]interface{}{"title": "Root", "value": payload.Summary.Root, "short": true},
			map[string]interface{}{"title": "Mode", "value": payload.Summary.Mode, "short": true},
			map[string]interface{}{"title": "Files Deleted", "value": fmt.Sprintf("%d", payload.Summary.FilesDeleted), "short": true},
			map[string]interface{}{"title": "Bytes Freed", "value": formatBytes(payload.Summary.BytesFreed), "short": true},
			map[string]interface{}{"title": "Duration", "value": payload.Summary.Duration, "short": true},
		)
		if payload.Summary.Errors > 0 {
			fields = append(fields,
				map[string]interface{}{"title": "Errors", "value": fmt.Sprintf("%d", payload.Summary.Errors), "short": true},
			)
		}
	}

	return map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"title":  title,
				"text":   payload.Message,
				"fields": fields,
				"footer": "storage-sage",
				"ts":     payload.Timestamp.Unix(),
			},
		},
	}
}

func formatBytes(b int64) string {
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
