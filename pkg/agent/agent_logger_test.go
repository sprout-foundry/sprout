package agent

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestLogger creates an AgentLogger backed by a real agent and a temp file
// for capturing output. The agent has session, iteration, provider, model set.
func newTestLogger(t *testing.T) (*AgentLogger, *os.File, string) {
	t.Helper()
	agent := newTestAgent(t)
	t.Cleanup(agent.Shutdown)

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("create temp log file: %v", err)
	}
	agent.debugLogFile = f
	agent.debug = true
	agent.state.SetSessionID("test-session-123")
	agent.state.SetCurrentIteration(5)

	logger := NewAgentLogger(agent)
	return logger, f, dir
}

// newTestLoggerNoFile creates a logger without a debug file (falls back to stderr).
func newTestLoggerNoFile(t *testing.T) *AgentLogger {
	t.Helper()
	agent := newTestAgent(t)
	t.Cleanup(agent.Shutdown)
	agent.debug = true
	return NewAgentLogger(agent)
}

// ----------------------------------------------------------------------------
// NewAgentLogger
// ----------------------------------------------------------------------------

func TestAgentLoggerNilAgent(t *testing.T) {
	logger := NewAgentLogger(nil)
	if logger != nil {
		t.Error("NewAgentLogger(nil) should return nil")
	}
}

func TestAgentLoggerValidAgent(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if logger.agent == nil {
		t.Error("logger.agent should not be nil")
	}
	if logger.file != f {
		t.Error("logger.file should point to agent's debugLogFile")
	}
}

// ----------------------------------------------------------------------------
// Nil receiver safety (AgentLogger)
// ----------------------------------------------------------------------------

func TestAgentLoggerNilReceiverMethods(t *testing.T) {
	var l *AgentLogger

	// None of these should panic
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error("test")
	l.SetJSONMode(true)
	l.WithFields(nil)
	l.WithFields(map[string]string{"k": "v"})
	l.contextFields()
	l.writeEntry("info", "test", nil)
}

// ----------------------------------------------------------------------------
// Nil receiver safety (LogContext)
// ----------------------------------------------------------------------------

func TestAgentLoggerNilLogContextReceiver(t *testing.T) {
	var lc *LogContext

	lc.Debug("test")
	lc.Info("test")
	lc.Warn("test")
	lc.Error("test")
}

func TestAgentLoggerNilLogContextLogger(t *testing.T) {
	lc := &LogContext{logger: nil, fields: map[string]string{"key": "value"}}
	lc.Debug("test")
	lc.Info("test")
	lc.Warn("test")
	lc.Error("test")
}

// ----------------------------------------------------------------------------
// Debug filtering
// ----------------------------------------------------------------------------

func TestAgentLoggerDebugFilteredWhenDebugOff(t *testing.T) {
	agent := newTestAgent(t)
	agent.Shutdown() // shutdown now; we only need the struct fields

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("create temp log file: %v", err)
	}
	defer f.Close()

	agent.debug = false // ensure debug is OFF
	agent.debugLogFile = f

	logger := NewAgentLogger(agent)
	logger.Debug("should not appear")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if len(content) > 0 {
		t.Errorf("debug message should be filtered when debug=false, got: %s", content)
	}
}

func TestAgentLoggerDebugWrittenWhenDebugOn(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.Debug("debug message")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "debug message") {
		t.Errorf("debug message should be written when debug=true, got: %s", content)
	}
}

// ----------------------------------------------------------------------------
// contextFields
// ----------------------------------------------------------------------------

func TestAgentLoggerContextFieldsExtractsAllFields(t *testing.T) {
	logger, _, _ := newTestLogger(t)
	fields := logger.contextFields()

	if fields["session"] != "test-session-123" {
		t.Errorf("session = %q, want %q", fields["session"], "test-session-123")
	}
	if fields["iter"] != "5" {
		t.Errorf("iter = %q, want %q", fields["iter"], "5")
	}
	if fields["provider"] == "" {
		t.Error("provider should not be empty")
	}
	if fields["model"] == "" {
		t.Error("model should not be empty")
	}
}

func TestAgentLoggerContextFieldsNilAgent(t *testing.T) {
	l := &AgentLogger{agent: nil}
	fields := l.contextFields()
	if fields != nil {
		t.Errorf("contextFields with nil agent = %+v, want nil", fields)
	}
}

func TestAgentLoggerContextFieldsNilReceiver(t *testing.T) {
	var l *AgentLogger
	fields := l.contextFields()
	if fields != nil {
		t.Errorf("nil contextFields = %+v, want nil", fields)
	}
}

func TestAgentLoggerContextFieldsEmptyAgent(t *testing.T) {
	// Agent with empty session, zero iteration, etc.
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("create temp log file: %v", err)
	}
	agent.debugLogFile = f
	agent.debug = true
	// Do NOT set session ID or iteration — leave defaults

	logger := NewAgentLogger(agent)
	fields := logger.contextFields()

	if _, ok := fields["session"]; ok {
		t.Error("session should not be in fields when empty")
	}
	if _, ok := fields["iter"]; ok {
		t.Error("iter should not be in fields when iteration is 0")
	}
}

// ----------------------------------------------------------------------------
// Non-JSON (human-readable) mode
// ----------------------------------------------------------------------------

func TestAgentLoggerInfoHumanReadable(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.Info("hello world")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	line := strings.TrimSpace(string(content))

	// Should contain: timestamp, level, session, iter, message
	if !strings.Contains(line, "info") {
		t.Errorf("expected 'info' in line: %s", line)
	}
	if !strings.Contains(line, "session=test-session-123") {
		t.Errorf("expected 'session=test-session-123' in line: %s", line)
	}
	if !strings.Contains(line, "iter=5") {
		t.Errorf("expected 'iter=5' in line: %s", line)
	}
	if !strings.Contains(line, "hello world") {
		t.Errorf("expected 'hello world' in line: %s", line)
	}
}

func TestAgentLoggerWarnHumanReadable(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.Warn("something happened")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "warn") {
		t.Errorf("expected 'warn' in output: %s", content)
	}
	if !strings.Contains(string(content), "something happened") {
		t.Errorf("expected message in output: %s", content)
	}
}

func TestAgentLoggerErrorHumanReadable(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.Error("oops %d", 42)

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "error") {
		t.Errorf("expected 'error' in output: %s", content)
	}
	if !strings.Contains(string(content), "oops 42") {
		t.Errorf("expected formatted message 'oops 42' in output: %s", content)
	}
}

// ----------------------------------------------------------------------------
// JSON mode
// ----------------------------------------------------------------------------

func TestAgentLoggerJSONModeValidJSON(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.SetJSONMode(true)
	logger.Info("json test message")

	f.Seek(0, 0)
	line, _ := io.ReadAll(f)

	var entry LogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\nraw: %s", err, line)
	}

	if entry.Level != "info" {
		t.Errorf("level = %q, want %q", entry.Level, "info")
	}
	if entry.Message != "json test message" {
		t.Errorf("message = %q, want %q", entry.Message, "json test message")
	}
	if entry.SessionID != "test-session-123" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "test-session-123")
	}
	if entry.Iteration != 5 {
		t.Errorf("iteration = %d, want %d", entry.Iteration, 5)
	}
	if entry.Provider == "" {
		t.Error("provider should not be empty")
	}
	if entry.Model == "" {
		t.Error("model should not be empty")
	}
}

func TestAgentLoggerJSONModeError(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.SetJSONMode(true)
	logger.Error("json error test")

	f.Seek(0, 0)
	line, _ := io.ReadAll(f)

	var entry LogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if entry.Level != "error" {
		t.Errorf("level = %q, want %q", entry.Level, "error")
	}
	if entry.Message != "json error test" {
		t.Errorf("message = %q, want %q", entry.Message, "json error test")
	}
}

func TestAgentLoggerJSONModeExtraFields(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.SetJSONMode(true)
	lc := logger.WithFields(map[string]string{
		"component": "auth",
		"action":    "login",
	})
	lc.Info("extra fields test")

	f.Seek(0, 0)
	line, _ := io.ReadAll(f)

	var entry LogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	// Extra fields should be in entry.Fields (not in top-level session/iter/provider/model)
	if entry.Fields == nil {
		t.Fatal("Fields is nil, expected non-nil")
	}
	if entry.Fields["component"] != "auth" {
		t.Errorf("Fields[component] = %q, want %q", entry.Fields["component"], "auth")
	}
	if entry.Fields["action"] != "login" {
		t.Errorf("Fields[action] = %q, want %q", entry.Fields["action"], "login")
	}
}

// ----------------------------------------------------------------------------
// WithFields and LogContext
// ----------------------------------------------------------------------------

func TestAgentLoggerWithFieldsReturnsLogContext(t *testing.T) {
	logger, _, _ := newTestLogger(t)
	lc := logger.WithFields(map[string]string{"key": "value"})
	if lc == nil {
		t.Fatal("WithFields returned nil")
	}
	if lc.logger != logger {
		t.Error("LogContext.logger should point to original logger")
	}
	if lc.fields["key"] != "value" {
		t.Errorf("LogContext.fields[key] = %q, want %q", lc.fields["key"], "value")
	}
}

func TestAgentLoggerWithFieldsNilReceiver(t *testing.T) {
	var l *AgentLogger
	lc := l.WithFields(map[string]string{"k": "v"})
	if lc != nil {
		t.Error("WithFields on nil logger should return nil")
	}
}

func TestAgentLoggerLogContextDebug(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	lc := logger.WithFields(map[string]string{"comp": "auth"})
	lc.Debug("ctx debug msg")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "ctx debug msg") {
		t.Errorf("expected 'ctx debug msg' in output: %s", content)
	}
	if !strings.Contains(string(content), "comp=auth") {
		t.Errorf("expected 'comp=auth' in output: %s", content)
	}
}

func TestAgentLoggerLogContextInfo(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	lc := logger.WithFields(map[string]string{"comp": "db"})
	lc.Info("ctx info %s", "formatted")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "ctx info formatted") {
		t.Errorf("expected formatted message in output: %s", content)
	}
}

func TestAgentLoggerLogContextWarn(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	lc := logger.WithFields(map[string]string{"comp": "api"})
	lc.Warn("ctx warn msg")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "warn") {
		t.Errorf("expected 'warn' level in output: %s", content)
	}
	if !strings.Contains(string(content), "ctx warn msg") {
		t.Errorf("expected 'ctx warn msg' in output: %s", content)
	}
}

func TestAgentLoggerLogContextError(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	lc := logger.WithFields(map[string]string{"comp": "net"})
	lc.Error("ctx error %d", 500)

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	if !strings.Contains(string(content), "error") {
		t.Errorf("expected 'error' level in output: %s", content)
	}
	if !strings.Contains(string(content), "ctx error 500") {
		t.Errorf("expected 'ctx error 500' in output: %s", content)
	}
}

func TestAgentLoggerLogContextMergesWithAgentContext(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	lc := logger.WithFields(map[string]string{"extra": "field"})
	lc.Info("merged")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	// Should have both agent context (session, iter) and extra field
	if !strings.Contains(string(content), "session=test-session-123") {
		t.Errorf("expected agent context 'session' in output: %s", content)
	}
	if !strings.Contains(string(content), "extra=field") {
		t.Errorf("expected extra field in output: %s", content)
	}
}

// ----------------------------------------------------------------------------
// JSON mode with WithFields
// ----------------------------------------------------------------------------

func TestAgentLoggerLogContextJSONMode(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.SetJSONMode(true)
	lc := logger.WithFields(map[string]string{"step": "init"})
	lc.Info("json ctx")

	f.Seek(0, 0)
	line, _ := io.ReadAll(f)

	var entry LogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	if entry.Message != "json ctx" {
		t.Errorf("message = %q, want %q", entry.Message, "json ctx")
	}
	// "step" is not one of the reserved fields, so it should be in Fields
	if entry.Fields["step"] != "init" {
		t.Errorf("Fields[step] = %q, want %q", entry.Fields["step"], "init")
	}
	// But session/iter/provider/model should be in their own fields
	if entry.SessionID != "test-session-123" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "test-session-123")
	}
	if entry.Fields["session"] != "" {
		t.Error("Fields[session] should be empty; session goes to SessionID")
	}
}

// ----------------------------------------------------------------------------
// SetJSONMode
// ----------------------------------------------------------------------------

func TestAgentLoggerSetJSONModeNilReceiver(t *testing.T) {
	var l *AgentLogger
	l.SetJSONMode(true) // should not panic
}

// ----------------------------------------------------------------------------
// Multiple log entries
// ----------------------------------------------------------------------------

func TestAgentLoggerMultipleLogEntries(t *testing.T) {
	logger, f, _ := newTestLogger(t)
	f.Truncate(0)
	f.Seek(0, 0)

	logger.Info("first")
	logger.Warn("second")
	logger.Error("third")

	f.Seek(0, 0)
	content, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %s", len(lines), content)
	}
}

// ----------------------------------------------------------------------------
// No debugLogFile fallback to stderr (basic smoke test)
// ----------------------------------------------------------------------------

func TestAgentLoggerNoFileFallback(t *testing.T) {
	// When agent has no debugLogFile, the logger falls back to os.Stderr.
	// We just verify it doesn't panic.
	logger := newTestLoggerNoFile(t)
	logger.Info("no file test")
	logger.Debug("no file debug")
}
