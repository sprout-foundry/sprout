package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
)

// newTestLogger creates a StructuredLogger that writes JSON to the given buffer.
func newTestLogger(buf *bytes.Buffer) *StructuredLogger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return &StructuredLogger{inner: slog.New(handler)}
}

// parseLastLine reads the last JSON log line from the buffer and returns it as a map.
func parseLastLine(buf *bytes.Buffer) map[string]interface{} {
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(lines[i], &m); err == nil {
			return m
		}
	}
	return nil
}

// --- NewStructuredLogger ---

func TestNewStructuredLogger_ReturnsNonNilLogger(t *testing.T) {
	logger := NewStructuredLogger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if logger.inner == nil {
		t.Fatal("expected non-nil inner slog.Logger")
	}
}

// --- Default global ---

func TestDefault_IsInitialized(t *testing.T) {
	if Default == nil {
		t.Fatal("expected Default logger to be initialized")
	}
	if Default.inner == nil {
		t.Fatal("expected Default.inner to be non-nil")
	}
}

// --- Context chaining: immutability and new instances ---

func TestWithContext_ReturnsNewInstance(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	result := original.WithContext("sess1", "chat1", "client1")

	if result == original {
		t.Error("expected WithContext to return a new instance, not the same pointer")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if original == nil || original.inner == nil {
		t.Fatal("expected original logger to remain valid")
	}
}

func TestWithProvider_ReturnsNewInstance(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	result := original.WithProvider("anthropic", "claude-3")

	if result == original {
		t.Error("expected WithProvider to return a new instance, not the same pointer")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestWithTool_ReturnsNewInstance(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	result := original.WithTool("shell_command", "tool-abc")

	if result == original {
		t.Error("expected WithTool to return a new instance, not the same pointer")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestWithError_ReturnsNewInstance(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	err := fmt.Errorf("something broke")
	result := original.WithError(err)

	if result == original {
		t.Error("expected WithError to return a new instance, not the same pointer")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestWithField_ReturnsNewInstance(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	result := original.WithField("custom_key", "custom_value")

	if result == original {
		t.Error("expected WithField to return a new instance, not the same pointer")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- Immutability: original logger is unchanged ---

func TestWithContext_OriginalUnchanged(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	_ = original.WithContext("sess1", "chat1", "client1")

	// Original should still have a clean inner logger — log a message and ensure
	// no context fields leaked into the original.
	original.Info("check")
	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}
	if _, ok := line["session_id"]; ok {
		t.Error("expected original logger to NOT have session_id after WithContext was called on it")
	}
	if _, ok := line["chat_id"]; ok {
		t.Error("expected original logger to NOT have chat_id after WithContext was called on it")
	}
	if _, ok := line["client_id"]; ok {
		t.Error("expected original logger to NOT have client_id after WithContext was called on it")
	}
}

func TestWithProvider_OriginalUnchanged(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	_ = original.WithProvider("openai", "gpt-4")

	original.Info("check")
	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}
	if _, ok := line["provider"]; ok {
		t.Error("expected original logger to NOT have provider after WithProvider was called on it")
	}
	if _, ok := line["model"]; ok {
		t.Error("expected original logger to NOT have model after WithProvider was called on it")
	}
}

func TestWithField_OriginalUnchanged(t *testing.T) {
	buf := new(bytes.Buffer)
	original := newTestLogger(buf)

	_ = original.WithField("foo", "bar")

	original.Info("check")
	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}
	if _, ok := line["foo"]; ok {
		t.Error("expected original logger to NOT have foo after WithField was called on it")
	}
}

// --- Field attachment: fields appear in JSON output ---

func TestWithContext_FieldsInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithContext("sess-42", "chat-99", "client-abc")

	logger.Info("hello")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["session_id"].(string); !ok || v != "sess-42" {
		t.Errorf("expected session_id=sess-42, got %v", line["session_id"])
	}
	if v, ok := line["chat_id"].(string); !ok || v != "chat-99" {
		t.Errorf("expected chat_id=chat-99, got %v", line["chat_id"])
	}
	if v, ok := line["client_id"].(string); !ok || v != "client-abc" {
		t.Errorf("expected client_id=client-abc, got %v", line["client_id"])
	}
}

func TestWithProvider_FieldsInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithProvider("anthropic", "claude-3.5")

	logger.Info("msg")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["provider"].(string); !ok || v != "anthropic" {
		t.Errorf("expected provider=anthropic, got %v", line["provider"])
	}
	if v, ok := line["model"].(string); !ok || v != "claude-3.5" {
		t.Errorf("expected model=claude-3.5, got %v", line["model"])
	}
}

func TestWithTool_FieldsInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithTool("file_editor", "edit-123")

	logger.Info("msg")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["tool_name"].(string); !ok || v != "file_editor" {
		t.Errorf("expected tool_name=file_editor, got %v", line["tool_name"])
	}
	if v, ok := line["tool_id"].(string); !ok || v != "edit-123" {
		t.Errorf("expected tool_id=edit-123, got %v", line["tool_id"])
	}
}

func TestWithError_FieldInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	err := fmt.Errorf("disk full")
	logger := newTestLogger(buf).WithError(err)

	logger.Info("msg")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["err"].(string); !ok || v != "disk full" {
		t.Errorf("expected err=\"disk full\", got %v", line["err"])
	}
}

func TestWithField_FieldInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithField("region", "us-east-1")

	logger.Info("msg")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["region"].(string); !ok || v != "us-east-1" {
		t.Errorf("expected region=us-east-1, got %v", line["region"])
	}
}

func TestWithField_NonStringValueInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithField("count", 42)

	logger.Info("msg")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["count"].(float64); !ok || v != 42 {
		t.Errorf("expected count=42, got %v", line["count"])
	}
}

// --- Chained With* calls ---

func TestChainedWith_CombinesFields(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).
		WithContext("s1", "c1", "cl1").
		WithProvider("openai", "gpt-4").
		WithTool("search", "srch-1").
		WithField("custom", true)

	logger.Info("multi")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	if v, ok := line["session_id"].(string); !ok || v != "s1" {
		t.Errorf("expected session_id=s1, got %v", line["session_id"])
	}
	if v, ok := line["provider"].(string); !ok || v != "openai" {
		t.Errorf("expected provider=openai, got %v", line["provider"])
	}
	if v, ok := line["tool_name"].(string); !ok || v != "search" {
		t.Errorf("expected tool_name=search, got %v", line["tool_name"])
	}
	if v, ok := line["custom"].(bool); !ok || v != true {
		t.Errorf("expected custom=true, got %v", line["custom"])
	}
}

// --- Output format: valid JSON and log levels ---

func TestInfo_ProducesValidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Info("test info")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line with valid JSON")
	}
	if line["msg"] != "test info" {
		t.Errorf("expected msg=\"test info\", got %v", line["msg"])
	}
}

func TestWarn_ProducesValidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Warn("test warn")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line with valid JSON")
	}
	if line["msg"] != "test warn" {
		t.Errorf("expected msg=\"test warn\", got %v", line["msg"])
	}
}

func TestError_ProducesValidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Error("test error")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line with valid JSON")
	}
	if line["msg"] != "test error" {
		t.Errorf("expected msg=\"test error\", got %v", line["msg"])
	}
}

func TestDebug_ProducesValidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Debug("test debug")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line with valid JSON")
	}
	if line["msg"] != "test debug" {
		t.Errorf("expected msg=\"test debug\", got %v", line["msg"])
	}
}

func TestLogLevels_AppearInOutput(t *testing.T) {
	tests := []struct {
		name     string
		logFunc  func(*StructuredLogger, string)
		expected string // slog uses lowercase level names in JSON
	}{
		{"Debug level", func(l *StructuredLogger, m string) { l.Debug(m) }, "DEBUG"},
		{"Info level", func(l *StructuredLogger, m string) { l.Info(m) }, "INFO"},
		{"Warn level", func(l *StructuredLogger, m string) { l.Warn(m) }, "WARN"},
		{"Error level", func(l *StructuredLogger, m string) { l.Error(m) }, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			logger := newTestLogger(buf)
			tt.logFunc(logger, "level check")

			line := parseLastLine(buf)
			if line == nil {
				t.Fatal("expected a log line")
			}
			if v, ok := line["level"].(string); !ok || v != tt.expected {
				t.Errorf("expected level=%s, got %v", tt.expected, line["level"])
			}
		})
	}
}

// --- convertArgs ---

func TestConvertArgs_KeyValuePairs(t *testing.T) {
	result := convertArgs("key1", "val1", "key2", 42)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	// result is []slog.Attr, verify keys are correct
	if result[0].Key != "key1" {
		t.Errorf("expected key=key1, got %s", result[0].Key)
	}
	if result[1].Key != "key2" {
		t.Errorf("expected key=key2, got %s", result[1].Key)
	}
}

func TestConvertArgs_EvenArgs(t *testing.T) {
	result := convertArgs("a", 1, "b", 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].Key != "a" {
		t.Errorf("expected first key=a, got %s", result[0].Key)
	}
	if result[1].Key != "b" {
		t.Errorf("expected second key=b, got %s", result[1].Key)
	}
}

func TestConvertArgs_OddArgs_LastValueIgnored(t *testing.T) {
	result := convertArgs("a", 1, "orphan")
	if len(result) != 1 {
		t.Fatalf("expected 1 result (orphan ignored), got %d", len(result))
	}
	if result[0].Key != "a" {
		t.Errorf("expected key=a, got %s", result[0].Key)
	}
}

func TestConvertArgs_NonStringKey_Skipped(t *testing.T) {
	result := convertArgs(123, "val", "good", "value")
	if len(result) != 1 {
		t.Fatalf("expected 1 result (non-string key skipped), got %d", len(result))
	}
	if result[0].Key != "good" {
		t.Errorf("expected key=good, got %s", result[0].Key)
	}
}

func TestConvertArgs_EmptyArgs(t *testing.T) {
	result := convertArgs()
	if len(result) != 0 {
		t.Fatalf("expected 0 results for empty args, got %d", len(result))
	}
}

func TestConvertArgs_SingleArg_Ignored(t *testing.T) {
	result := convertArgs("lonely")
	if len(result) != 0 {
		t.Fatalf("expected 0 results for single arg (no pair), got %d", len(result))
	}
}

// --- Info/Warn/Error/Debug with args produce fields in output ---

func TestInfo_WithArgs_FieldsInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Info("hello", "user", "alice", "count", 5)

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}
	if v, ok := line["user"].(string); !ok || v != "alice" {
		t.Errorf("expected user=alice, got %v", line["user"])
	}
	if v, ok := line["count"].(float64); !ok || v != 5 {
		t.Errorf("expected count=5, got %v", line["count"])
	}
}

func TestError_WithArgs_FieldsInOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	logger.Error("failed", "op", "connect", "retries", 3)

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}
	if v, ok := line["op"].(string); !ok || v != "connect" {
		t.Errorf("expected op=connect, got %v", line["op"])
	}
	if v, ok := line["retries"].(float64); !ok || v != 3 {
		t.Errorf("expected retries=3, got %v", line["retries"])
	}
}

// --- Combined With* and args in a single log ---

func TestCombinedWithAndArgs_AllFieldsPresent(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf).WithContext("s1", "c1", "cl1").WithProvider("p1", "m1")

	logger.Info("combined", "extra", "yes")

	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line")
	}

	for _, field := range []string{"session_id", "chat_id", "client_id", "provider", "model", "extra"} {
		if _, ok := line[field]; !ok {
			t.Errorf("expected field %q in output, got: %v", field, line)
		}
	}
	if line["msg"] != "combined" {
		t.Errorf("expected msg=combined, got %v", line["msg"])
	}
}

// --- WithError(nil) must not panic ---

func TestWithError_NilError_DoesNotPanic(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := newTestLogger(buf)

	// Must not panic
	result := logger.WithError(nil)

	if result == nil {
		t.Fatal("expected WithError(nil) to return a non-nil logger")
	}
	if result.inner == nil {
		t.Fatal("expected WithError(nil) to preserve inner logger")
	}

	// The resulting logger should still work
	result.Info("after nil error")
	line := parseLastLine(buf)
	if line == nil {
		t.Fatal("expected a log line after using logger returned by WithError(nil)")
	}
	if line["msg"] != "after nil error" {
		t.Errorf("expected msg=\"after nil error\", got %v", line["msg"])
	}

	// No "err" field should be present
	if _, ok := line["err"]; ok {
		t.Error("expected no 'err' field when WithError(nil) is used")
	}
}

// --- Nil receiver must not panic ---

func TestInfo_NilReceiver_DoesNotPanic(t *testing.T) {
	var logger *StructuredLogger // explicitly nil

	// Must not panic
	logger.Info("safe")
}

func TestDebug_NilReceiver_DoesNotPanic(t *testing.T) {
	var logger *StructuredLogger
	logger.Debug("safe")
}

func TestWarn_NilReceiver_DoesNotPanic(t *testing.T) {
	var logger *StructuredLogger
	logger.Warn("safe")
}

func TestError_NilReceiver_DoesNotPanic(t *testing.T) {
	var logger *StructuredLogger
	logger.Error("safe")
}
