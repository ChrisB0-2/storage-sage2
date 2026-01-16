package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// LokiConfig holds configuration for Loki log shipping.
type LokiConfig struct {
	URL       string
	BatchSize int
	BatchWait time.Duration
	Labels    map[string]string
	TenantID  string
}

// lokiEntry represents a log entry to be sent to Loki.
type lokiEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]any
}

// lokiRequest is the JSON payload for Loki's push API.
type lokiRequest struct {
	Streams []lokiStream `json:"streams"`
}

// lokiStream represents a stream of log entries with labels.
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"` // [[timestamp_ns, line], ...]
}

// LokiLogger wraps a base logger and ships logs to Loki.
type LokiLogger struct {
	base   Logger
	config LokiConfig
	client *http.Client

	mu       sync.Mutex
	buffer   []lokiEntry
	fields   []Field
	done     chan struct{}
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// NewLokiLogger creates a new LokiLogger that wraps the base logger.
// Logs are sent to both the base logger and Loki asynchronously.
func NewLokiLogger(base Logger, cfg LokiConfig) *LokiLogger {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.BatchWait <= 0 {
		cfg.BatchWait = 5 * time.Second
	}
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{"service": "storage-sage"}
	}

	l := &LokiLogger{
		base:   base,
		config: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer:   make([]lokiEntry, 0, cfg.BatchSize),
		done:     make(chan struct{}),
		shutdown: make(chan struct{}),
	}

	// Start background flusher
	l.wg.Add(1)
	go l.flusher()

	return l
}

// Debug logs at debug level.
func (l *LokiLogger) Debug(msg string, fields ...Field) {
	l.base.Debug(msg, fields...)
	l.enqueue(LevelDebug, msg, fields)
}

// Info logs at info level.
func (l *LokiLogger) Info(msg string, fields ...Field) {
	l.base.Info(msg, fields...)
	l.enqueue(LevelInfo, msg, fields)
}

// Warn logs at warn level.
func (l *LokiLogger) Warn(msg string, fields ...Field) {
	l.base.Warn(msg, fields...)
	l.enqueue(LevelWarn, msg, fields)
}

// Error logs at error level.
func (l *LokiLogger) Error(msg string, fields ...Field) {
	l.base.Error(msg, fields...)
	l.enqueue(LevelError, msg, fields)
}

// WithFields returns a new logger with additional fields.
func (l *LokiLogger) WithFields(fields ...Field) Logger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)

	return &LokiLogger{
		base:     l.base.WithFields(fields...),
		config:   l.config,
		client:   l.client,
		buffer:   l.buffer,
		fields:   newFields,
		done:     l.done,
		shutdown: l.shutdown,
	}
}

// enqueue adds a log entry to the buffer.
func (l *LokiLogger) enqueue(level Level, msg string, fields []Field) {
	entry := lokiEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   msg,
	}

	// Merge base fields with call-specific fields
	allFields := append(l.fields, fields...)
	if len(allFields) > 0 {
		entry.Fields = make(map[string]any, len(allFields))
		for _, f := range allFields {
			entry.Fields[f.Key] = f.Value
		}
	}

	l.mu.Lock()
	l.buffer = append(l.buffer, entry)
	shouldFlush := len(l.buffer) >= l.config.BatchSize
	l.mu.Unlock()

	if shouldFlush {
		l.Flush()
	}
}

// flusher runs in background and flushes buffer periodically.
func (l *LokiLogger) flusher() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.BatchWait)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.Flush()
		case <-l.shutdown:
			l.Flush() // Final flush
			close(l.done)
			return
		}
	}
}

// Flush sends buffered logs to Loki.
func (l *LokiLogger) Flush() {
	l.mu.Lock()
	if len(l.buffer) == 0 {
		l.mu.Unlock()
		return
	}

	// Swap buffer
	entries := l.buffer
	l.buffer = make([]lokiEntry, 0, l.config.BatchSize)
	l.mu.Unlock()

	// Send to Loki (non-blocking, errors logged to base logger)
	go l.send(entries)
}

// send pushes log entries to Loki.
func (l *LokiLogger) send(entries []lokiEntry) {
	if len(entries) == 0 {
		return
	}

	// Build Loki request
	// Group entries by level for better label cardinality
	streams := make(map[string]*lokiStream)

	for _, entry := range entries {
		// Create stream key from level
		key := entry.Level

		stream, exists := streams[key]
		if !exists {
			labels := make(map[string]string, len(l.config.Labels)+1)
			for k, v := range l.config.Labels {
				labels[k] = v
			}
			labels["level"] = entry.Level
			stream = &lokiStream{
				Stream: labels,
				Values: make([][2]string, 0),
			}
			streams[key] = stream
		}

		// Format log line as JSON
		line := l.formatLine(entry)
		timestamp := strconv.FormatInt(entry.Timestamp.UnixNano(), 10)
		stream.Values = append(stream.Values, [2]string{timestamp, line})
	}

	// Convert map to slice
	streamSlice := make([]lokiStream, 0, len(streams))
	for _, s := range streams {
		streamSlice = append(streamSlice, *s)
	}

	req := lokiRequest{Streams: streamSlice}

	// Marshal and send
	body, err := json.Marshal(req)
	if err != nil {
		l.base.Error("loki: failed to marshal request", F("error", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.config.URL+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		l.base.Error("loki: failed to create request", F("error", err.Error()))
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if l.config.TenantID != "" {
		httpReq.Header.Set("X-Scope-OrgID", l.config.TenantID)
	}

	resp, err := l.client.Do(httpReq)
	if err != nil {
		l.base.Error("loki: failed to send logs", F("error", err.Error()), F("entries", len(entries)))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		l.base.Error("loki: server error",
			F("status", resp.StatusCode),
			F("entries", len(entries)),
		)
	}
}

// formatLine formats a log entry as a JSON string for Loki.
func (l *LokiLogger) formatLine(entry lokiEntry) string {
	line := map[string]any{
		"msg": entry.Message,
	}
	for k, v := range entry.Fields {
		line[k] = v
	}

	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Sprintf(`{"msg":%q,"error":"marshal_failed"}`, entry.Message)
	}
	return string(data)
}

// Close shuts down the Loki logger and flushes remaining logs.
func (l *LokiLogger) Close() error {
	close(l.shutdown)

	// Wait for flusher to finish with timeout
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("loki: shutdown timed out")
	}
}
