package logging

import (
	"log/slog"
	"os"
)

// StructuredLogger wraps slog.Logger with domain-specific convenience methods
// for consistent structured logging throughout the agent codebase.
type StructuredLogger struct {
	inner *slog.Logger
}

// NewStructuredLogger creates a new logger writing to stderr with JSON handler
func NewStructuredLogger() *StructuredLogger {
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return &StructuredLogger{inner: slog.New(handler)}
}

// WithContext returns a logger with session/conversation context
func (l *StructuredLogger) WithContext(sessionID, chatID, clientID string) *StructuredLogger {
	return &StructuredLogger{
		inner: l.inner.With(
			slog.String("session_id", sessionID),
			slog.String("chat_id", chatID),
			slog.String("client_id", clientID),
		),
	}
}

// WithProvider returns a logger with provider/model context
func (l *StructuredLogger) WithProvider(provider, model string) *StructuredLogger {
	return &StructuredLogger{
		inner: l.inner.With(
			slog.String("provider", provider),
			slog.String("model", model),
		),
	}
}

// WithTool returns a logger with tool execution context
func (l *StructuredLogger) WithTool(toolName, toolID string) *StructuredLogger {
	return &StructuredLogger{
		inner: l.inner.With(
			slog.String("tool_name", toolName),
			slog.String("tool_id", toolID),
		),
	}
}

// WithError returns a logger with error context
func (l *StructuredLogger) WithError(err error) *StructuredLogger {
	if err == nil {
		return &StructuredLogger{inner: l.inner}
	}
	return &StructuredLogger{
		inner: l.inner.With(
			slog.Any("err", err),
		),
	}
}

// WithField adds an arbitrary key-value pair
func (l *StructuredLogger) WithField(key string, value interface{}) *StructuredLogger {
	return &StructuredLogger{
		inner: l.inner.With(slog.Any(key, value)),
	}
}

// Debug logs at debug level
func (l *StructuredLogger) Debug(msg string, args ...interface{}) {
	if l == nil || l.inner == nil {
		return
	}
	attrs := convertArgs(args...)
	l.inner.Debug(msg, attrsToAny(attrs)...)
}

// Info logs at info level
func (l *StructuredLogger) Info(msg string, args ...interface{}) {
	if l == nil || l.inner == nil {
		return
	}
	attrs := convertArgs(args...)
	l.inner.Info(msg, attrsToAny(attrs)...)
}

// Warn logs at warn level
func (l *StructuredLogger) Warn(msg string, args ...interface{}) {
	if l == nil || l.inner == nil {
		return
	}
	attrs := convertArgs(args...)
	l.inner.Warn(msg, attrsToAny(attrs)...)
}

// Error logs at error level
func (l *StructuredLogger) Error(msg string, args ...interface{}) {
	if l == nil || l.inner == nil {
		return
	}
	attrs := convertArgs(args...)
	l.inner.Error(msg, attrsToAny(attrs)...)
}

// attrsToAny converts []slog.Attr to []any for slog.Logger methods.
func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for i, a := range attrs {
		out[i] = a
	}
	return out
}

// convertArgs converts alternating key-value pairs into slog.Attr values.
// If an odd number of args is provided, the last value is ignored.
// Non-string keys are skipped along with their associated value.
func convertArgs(args ...interface{}) []slog.Attr {
	result := make([]slog.Attr, 0, len(args)/2+1)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		result = append(result, slog.Any(key, args[i+1]))
	}
	return result
}

// Default is the global logger instance initialized on first use
var Default *StructuredLogger

func init() {
	Default = NewStructuredLogger()
}
