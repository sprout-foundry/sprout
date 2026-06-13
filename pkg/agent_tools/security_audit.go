//go:build !js

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/security"
)

// maxLogSize is the maximum size in bytes before log rotation triggers.
const maxLogSize = 10 * 1024 * 1024 // 10 MB

// AuditEntry represents a single security audit log entry.
type AuditEntry struct {
	Timestamp time.Time `json:"ts"`
	Tool      string    `json:"tool"`
	Command   string    `json:"command,omitempty"`
	RiskLevel string    `json:"risk"`
	Outcome   string    `json:"outcome,omitempty"`
	Category  string    `json:"category,omitempty"`
	Action    string    `json:"action,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	Source    string    `json:"source,omitempty"`
	Headless  bool      `json:"headless"`
	SessionID string    `json:"session_id,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Args      string    `json:"args,omitempty"`
}

// MarshalJSON implements custom marshaling that emits both new and legacy
// JSON keys for backward compatibility. Omits empty/zero optional fields.
func (e AuditEntry) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{
		// New short-form keys
		"ts":         e.Timestamp.Format(time.RFC3339),
		"timestamp":  e.Timestamp.Format(time.RFC3339), // legacy alias
		"tool":       e.Tool,
		"risk":       e.RiskLevel,
		"risk_level": e.RiskLevel, // legacy alias
	}
	if e.Command != "" {
		m["command"] = e.Command
	}
	if e.Outcome != "" {
		m["outcome"] = e.Outcome
	}
	if e.Category != "" {
		m["category"] = e.Category
	}
	if e.Action != "" {
		m["action"] = e.Action
	}
	if e.Reasoning != "" {
		m["reasoning"] = e.Reasoning
	}
	if e.Source != "" {
		m["source"] = e.Source
	}
	if e.Headless {
		m["headless"] = true
	}
	if e.SessionID != "" {
		m["session_id"] = e.SessionID
	}
	if e.Workspace != "" {
		m["workspace"] = e.Workspace
	}
	if e.Args != "" {
		m["args"] = e.Args
	}
	return json.Marshal(m)
}

// UnmarshalJSON accepts both new (ts, risk) and legacy (timestamp, risk_level)
// JSON keys for backward compatibility with older log entries.
func (e *AuditEntry) UnmarshalJSON(data []byte) error {
	type Alias AuditEntry
	aux := &struct {
		TimestampOld string `json:"timestamp"`
		RiskLevelOld string `json:"risk_level"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// Fallback to old key names if new ones were empty
	if e.Timestamp.IsZero() && aux.TimestampOld != "" {
		t, err := time.Parse(time.RFC3339, aux.TimestampOld)
		if err == nil {
			e.Timestamp = t
		}
	}
	if e.RiskLevel == "" && aux.RiskLevelOld != "" {
		e.RiskLevel = aux.RiskLevelOld
	}
	return nil
}

// AuditLogger provides thread-safe JSONL audit logging for security decisions.
type AuditLogger struct {
	mu      sync.Mutex
	file    *os.File
	logPath string
}

// NewAuditLogger creates or opens a log file at the given path,
// automatically creating parent directories as needed.
func NewAuditLogger(logPath string) (*AuditLogger, error) {
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log file: %w", err)
	}

	return &AuditLogger{file: f, logPath: logPath}, nil
}

// Log marshals the entry to JSON, scrubs secrets from Command and Args,
// appends it as a single line (JSONL format), then checks for rotation.
func (l *AuditLogger) Log(entry AuditEntry) error {
	if l == nil {
		return nil
	}

	// Create a mutable copy
	e := entry

	// Scrub secrets from Command and Args fields before logging
	redactor := security.NewOutputRedactor()
	if e.Command != "" {
		e.Command = redactor.RedactString(e.Command)
	}
	if e.Args != "" {
		e.Args = redactor.RedactString(e.Args)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return fmt.Errorf("audit logger: file is closed")
	}

	if _, err := l.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}

	// Check size and rotate if needed
	if err := l.maybeRotate(); err != nil {
		return fmt.Errorf("rotate audit log: %w", err)
	}

	return nil
}

// LogEntry is an alias for Log, named for call-site clarity.
// Nil-receiver safe via Log's internal nil guard.
func (l *AuditLogger) LogEntry(entry AuditEntry) error {
	return l.Log(entry)
}

// maybeRotate closes the current log, renames it to logPath+".1" (removing
// any existing backup), and opens a fresh file when the size exceeds
// maxLogSize. Must be called with l.mu held.
func (l *AuditLogger) maybeRotate() error {
	stat, err := l.file.Stat()
	if err != nil {
		return fmt.Errorf("stat audit log: %w", err)
	}
	if stat.Size() <= maxLogSize {
		return nil
	}

	// Close current file
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close audit log for rotation: %w", err)
	}
	l.file = nil

	rotatedPath := l.logPath + ".1"

	// Remove any existing rotated file
	_ = os.Remove(rotatedPath)

	// Rename current to .1
	if err := os.Rename(l.logPath, rotatedPath); err != nil {
		return fmt.Errorf("rename audit log for rotation: %w", err)
	}

	// Open fresh file
	f, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open new audit log after rotation: %w", err)
	}
	l.file = f
	return nil
}

// Close closes the underlying log file.
func (l *AuditLogger) Close() error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
