package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity levels.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel parses a string into a Level.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s", s)
	}
}

// Field represents a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

// F creates a new Field.
func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Logger is the interface for structured logging.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	WithFields(fields ...Field) Logger
}

// JSONLogger implements Logger with JSON output.
type JSONLogger struct {
	mu     sync.Mutex
	level  Level
	output io.Writer
	fields []Field
}

// logEntry represents a single log entry.
type logEntry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// New creates a new JSONLogger.
func New(level Level, output io.Writer) *JSONLogger {
	if output == nil {
		output = os.Stderr
	}
	return &JSONLogger{
		level:  level,
		output: output,
		fields: nil,
	}
}

// NewDefault creates a logger with info level writing to stderr.
func NewDefault() *JSONLogger {
	return New(LevelInfo, os.Stderr)
}

// Debug logs at debug level.
func (l *JSONLogger) Debug(msg string, fields ...Field) {
	l.log(LevelDebug, msg, fields)
}

// Info logs at info level.
func (l *JSONLogger) Info(msg string, fields ...Field) {
	l.log(LevelInfo, msg, fields)
}

// Warn logs at warn level.
func (l *JSONLogger) Warn(msg string, fields ...Field) {
	l.log(LevelWarn, msg, fields)
}

// Error logs at error level.
func (l *JSONLogger) Error(msg string, fields ...Field) {
	l.log(LevelError, msg, fields)
}

// WithFields returns a new logger with additional fields.
func (l *JSONLogger) WithFields(fields ...Field) Logger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	return &JSONLogger{
		level:  l.level,
		output: l.output,
		fields: newFields,
	}
}

func (l *JSONLogger) log(level Level, msg string, fields []Field) {
	if level < l.level {
		return
	}

	entry := logEntry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   level.String(),
		Message: msg,
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
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple format if JSON fails
		_, _ = fmt.Fprintf(l.output, "%s [%s] %s\n", entry.Time, entry.Level, msg)
		return
	}

	_, _ = l.output.Write(data)
	_, _ = l.output.Write([]byte("\n"))
}

// SetLevel changes the log level.
func (l *JSONLogger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// NopLogger is a logger that discards all output.
type NopLogger struct{}

func (NopLogger) Debug(msg string, fields ...Field) {}
func (NopLogger) Info(msg string, fields ...Field)  {}
func (NopLogger) Warn(msg string, fields ...Field)  {}
func (NopLogger) Error(msg string, fields ...Field) {}
func (NopLogger) WithFields(fields ...Field) Logger { return NopLogger{} }

// NewNop creates a no-op logger.
func NewNop() Logger {
	return NopLogger{}
}
