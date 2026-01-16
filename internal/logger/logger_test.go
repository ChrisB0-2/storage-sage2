package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
		{Level(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    Level
		wantErr bool
	}{
		{"debug", LevelDebug, false},
		{"info", LevelInfo, false},
		{"warn", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"error", LevelError, false},
		{"invalid", LevelInfo, true},
		{"", LevelInfo, true},
	}

	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestF(t *testing.T) {
	f := F("key", "value")
	if f.Key != "key" {
		t.Errorf("expected key 'key', got %q", f.Key)
	}
	if f.Value != "value" {
		t.Errorf("expected value 'value', got %v", f.Value)
	}
}

func TestJSONLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name     string
		logLevel Level
		logFunc  func(l *JSONLogger)
		wantLog  bool
	}{
		{"debug at debug level", LevelDebug, func(l *JSONLogger) { l.Debug("test") }, true},
		{"info at debug level", LevelDebug, func(l *JSONLogger) { l.Info("test") }, true},
		{"debug at info level", LevelInfo, func(l *JSONLogger) { l.Debug("test") }, false},
		{"info at info level", LevelInfo, func(l *JSONLogger) { l.Info("test") }, true},
		{"warn at info level", LevelInfo, func(l *JSONLogger) { l.Warn("test") }, true},
		{"info at warn level", LevelWarn, func(l *JSONLogger) { l.Info("test") }, false},
		{"warn at warn level", LevelWarn, func(l *JSONLogger) { l.Warn("test") }, true},
		{"error at warn level", LevelWarn, func(l *JSONLogger) { l.Error("test") }, true},
		{"warn at error level", LevelError, func(l *JSONLogger) { l.Warn("test") }, false},
		{"error at error level", LevelError, func(l *JSONLogger) { l.Error("test") }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := New(tt.logLevel, &buf)
			tt.logFunc(log)

			hasOutput := buf.Len() > 0
			if hasOutput != tt.wantLog {
				t.Errorf("got output = %v, want %v", hasOutput, tt.wantLog)
			}
		})
	}
}

func TestJSONLogger_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	log.Info("test message", F("key1", "value1"), F("key2", 42))

	// Parse the output
	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if entry.Level != "info" {
		t.Errorf("expected level 'info', got %q", entry.Level)
	}
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got %q", entry.Message)
	}
	if entry.Time == "" {
		t.Error("expected time to be set")
	}
	if entry.Fields["key1"] != "value1" {
		t.Errorf("expected fields.key1 = 'value1', got %v", entry.Fields["key1"])
	}
	// JSON numbers are float64
	if entry.Fields["key2"] != float64(42) {
		t.Errorf("expected fields.key2 = 42, got %v", entry.Fields["key2"])
	}
}

func TestJSONLogger_OutputEndsWithNewline(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	log.Info("test")

	output := buf.String()
	if !strings.HasSuffix(output, "\n") {
		t.Error("expected output to end with newline")
	}
}

func TestJSONLogger_WithFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	// Create child logger with base fields
	child := log.WithFields(F("request_id", "abc123"))

	// Log with child
	child.Info("request started")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["request_id"] != "abc123" {
		t.Errorf("expected request_id field, got %v", entry.Fields)
	}
}

func TestJSONLogger_WithFieldsMerge(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	// Create child with base field
	child := log.WithFields(F("base", "value"))

	// Log with additional fields
	child.Info("test", F("extra", "field"))

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["base"] != "value" {
		t.Error("expected base field to be present")
	}
	if entry.Fields["extra"] != "field" {
		t.Error("expected extra field to be present")
	}
}

func TestJSONLogger_WithFieldsChained(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelDebug, &buf)

	// Chain multiple WithFields
	log.WithFields(F("a", 1)).WithFields(F("b", 2)).Debug("test")

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["a"] != float64(1) {
		t.Error("expected field 'a' to be present")
	}
	if entry.Fields["b"] != float64(2) {
		t.Error("expected field 'b' to be present")
	}
}

func TestJSONLogger_NoFieldsOmitted(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	log.Info("no fields")

	// Parse as generic map to check for "fields" key
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Fields should be omitted when empty
	if _, ok := m["fields"]; ok {
		t.Error("expected 'fields' to be omitted when empty")
	}
}

func TestJSONLogger_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelDebug, &buf)

	// Should log at debug
	log.Debug("debug1")
	if buf.Len() == 0 {
		t.Error("expected debug log at debug level")
	}
	buf.Reset()

	// Change level to info
	log.SetLevel(LevelInfo)

	// Should not log debug anymore
	log.Debug("debug2")
	if buf.Len() > 0 {
		t.Error("expected no debug log at info level")
	}

	// Should log info
	log.Info("info")
	if buf.Len() == 0 {
		t.Error("expected info log at info level")
	}
}

func TestJSONLogger_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				log.Info("concurrent log", F("goroutine", id), F("iteration", j))
			}
		}(i)
	}

	wg.Wait()

	// Count lines
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	expected := goroutines * iterations
	if len(lines) != expected {
		t.Errorf("expected %d lines, got %d", expected, len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i+1, err)
		}
	}
}

func TestJSONLogger_NilOutput(t *testing.T) {
	// Should use stderr by default
	log := New(LevelInfo, nil)
	if log.output == nil {
		t.Error("expected non-nil output when nil is passed")
	}
}

func TestNewDefault(t *testing.T) {
	log := NewDefault()
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if log.level != LevelInfo {
		t.Errorf("expected info level, got %v", log.level)
	}
}

func TestNopLogger(t *testing.T) {
	log := NewNop()

	// All methods should work without panic
	log.Debug("debug")
	log.Info("info")
	log.Warn("warn")
	log.Error("error")

	// WithFields should return NopLogger
	child := log.WithFields(F("key", "value"))
	if _, ok := child.(NopLogger); !ok {
		t.Error("expected NopLogger from WithFields")
	}
}

func TestJSONLogger_AllLevelMethods(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelDebug, &buf)

	tests := []struct {
		name     string
		logFunc  func()
		expected string
	}{
		{"Debug", func() { log.Debug("debug msg") }, "debug"},
		{"Info", func() { log.Info("info msg") }, "info"},
		{"Warn", func() { log.Warn("warn msg") }, "warn"},
		{"Error", func() { log.Error("error msg") }, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()

			var entry logEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}
			if entry.Level != tt.expected {
				t.Errorf("expected level %q, got %q", tt.expected, entry.Level)
			}
		})
	}
}

func TestJSONLogger_VariousFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	log := New(LevelInfo, &buf)

	log.Info("test",
		F("string", "value"),
		F("int", 42),
		F("float", 3.14),
		F("bool", true),
		F("nil", nil),
	)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if entry.Fields["string"] != "value" {
		t.Errorf("string field: got %v", entry.Fields["string"])
	}
	if entry.Fields["int"] != float64(42) {
		t.Errorf("int field: got %v", entry.Fields["int"])
	}
	if entry.Fields["float"] != 3.14 {
		t.Errorf("float field: got %v", entry.Fields["float"])
	}
	if entry.Fields["bool"] != true {
		t.Errorf("bool field: got %v", entry.Fields["bool"])
	}
	if entry.Fields["nil"] != nil {
		t.Errorf("nil field: got %v", entry.Fields["nil"])
	}
}
