package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry represents a single security audit log entry.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool"`
	Args      string    `json:"args,omitempty"`
	RiskLevel string    `json:"risk_level"`
	Category  string    `json:"category"`
	Action    string    `json:"action"` // "allowed", "denied", "prompted"
	Reasoning string    `json:"reasoning,omitempty"`
	Source    string    `json:"source,omitempty"` // "classifier", "policy", "user_override"
	SessionID string    `json:"session_id,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
}

// AuditLogger provides thread-safe JSONL audit logging for security decisions.
type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
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

	return &AuditLogger{file: f}, nil
}

// Log marshals the entry to JSON and appends it as a single line
// (JSONL/NDJSON format) followed by a newline.
func (l *AuditLogger) Log(entry AuditEntry) error {
	if l == nil {
		return nil
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}

	return nil
}

// LogEntry is an alias for Log, named for call-site clarity.
// Nil-receiver safe via Log's internal nil guard.
// Accepts any type to allow flexible implementations (e.g., the filesystem
// package uses filesystem.AuditEntry which has the same JSON structure).
func (l *AuditLogger) LogEntry(entry any) error {
	ae, ok := entry.(AuditEntry)
	if !ok {
		return fmt.Errorf("unsupported entry type: %T", entry)
	}
	return l.Log(ae)
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
