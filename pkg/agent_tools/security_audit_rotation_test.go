//go:build !js

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// padToOversize extends a file to just over maxLogSize with null bytes.
// Used to simulate log growth for rotation tests without writing megabytes
// of real audit entries.
func padToOversize(path string) error {
	return os.Truncate(path, maxLogSize+1)
}

// ---------------------------------------------------------------------------
// Secret Scrubbing
// ---------------------------------------------------------------------------

// TestLog_SecretScrubbing_CommandField verifies that the OutputRedactor
// replaces secret values embedded in the Command and Args fields before
// they hit disk.
func TestLog_SecretScrubbing_CommandField(t *testing.T) {
	// Cannot use t.Parallel() — we call t.Setenv for a sensitive env var.

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Set a sensitive env var that the redactor will pick up (value > 8 chars).
	const secretVal = "super_secret_token_value_12345"
	t.Setenv("MY_API_KEY", secretVal)

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	// Log an entry with the secret embedded in the Command field.
	cmd := "curl -H 'Authorization: Bearer " + secretVal + "' https://api.example.com"
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "shell_command",
		Command:   cmd,
		RiskLevel: "CAUTION",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log with secret in Command: %v", err)
	}

	// Log an entry with a GitHub PAT-style token in Args.
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "shell_command",
		Args:      `{"token":"ghp_xxxxxxxxxxxx1234567890abcdefghij"}`,
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log with secret in Args: %v", err)
	}

	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// The raw env-var secret value should NOT appear anywhere in the output.
	if strings.Contains(string(data), secretVal) {
		t.Errorf("raw secret value found in audit log (should have been redacted):\n%s", string(data))
	}

	// Should contain the redaction placeholder for the env var secret.
	if !strings.Contains(string(data), "[REDACTED:MY_API_KEY]") {
		t.Errorf("expected [REDACTED:MY_API_KEY] placeholder in output:\n%s", string(data))
	}
}

// TestLog_SecretScrubbing_EnvVars verifies that secret values from sensitive
// environment variables are redacted when they appear in Args.
func TestLog_SecretScrubbing_EnvVars(t *testing.T) {
	// Cannot use t.Parallel() — we call t.Setenv for a sensitive env var.

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	const secretVal = "my_super_secret_value_123"
	t.Setenv("SECRET_KEY", secretVal)

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	err = logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "shell_command",
		Args:      `{"env":{"SECRET_KEY":"` + secretVal + `"}}`,
		RiskLevel: "SAFE",
		Action:    "allowed",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(data), secretVal) {
		t.Errorf("raw secret value found in audit log:\n%s", string(data))
	}

	if !strings.Contains(string(data), "[REDACTED:SECRET_KEY]") {
		t.Errorf("expected [REDACTED:SECRET_KEY] placeholder:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Log Rotation
// ---------------------------------------------------------------------------

// TestRotation_TriggeredAtMaxSize verifies that when the log file exceeds
// maxLogSize, the next Log() call rotates it (rename to .1, open fresh).
func TestRotation_TriggeredAtMaxSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")
	rotatedPath := logPath + ".1"

	// Phase 1: Create logger, write entries, close.
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := logger.Log(AuditEntry{
			Timestamp: time.Now(),
			Tool:      fmt.Sprintf("tool-%d", i),
			RiskLevel: "SAFE",
			Action:    "allowed",
		}); err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}
	logger.Close()

	// Phase 2: Pad the log file to exceed maxLogSize.
	if err := padToOversize(logPath); err != nil {
		t.Fatalf("padToOversize: %v", err)
	}

	// Phase 3: Reopen logger at the same path (O_APPEND positions at end).
	logger, err = NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger (re-open): %v", err)
	}

	// Phase 4: Write an entry — total file size now exceeds maxLogSize,
	// triggering rotation after the write.
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "trigger-rotation",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log (trigger rotation): %v", err)
	}

	// Phase 5: Write another entry to the new (post-rotation) file.
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "after-rotation",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log (after rotation): %v", err)
	}
	logger.Close()

	// Verify: rotated backup exists and is large.
	stat, err := os.Stat(rotatedPath)
	if err != nil {
		t.Fatalf("rotated backup should exist: %v", err)
	}
	if stat.Size() < maxLogSize {
		t.Errorf("rotated backup should be >= 10 MB, got %d bytes", stat.Size())
	}

	// Verify: new file has only the post-rotation entry.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile (new file): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("new file should have 1 entry after rotation, got %d lines", len(lines))
	}
	if !strings.Contains(string(data), "after-rotation") {
		t.Errorf("new file should contain the post-rotation entry:\n%s", string(data))
	}
}

// TestRotation_RemovesOldBackup verifies that a pre-existing .1 backup is
// deleted before the current log is renamed into its place.
func TestRotation_RemovesOldBackup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")
	rotatedPath := logPath + ".1"

	// Create logger, write entries, close.
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	logger.Log(AuditEntry{Timestamp: time.Now(), Tool: "old", RiskLevel: "SAFE", Action: "allowed"})
	logger.Close()

	// Create a fake old .1 backup with unique marker.
	if err := os.WriteFile(rotatedPath, []byte("THIS_IS_OLD_BACKUP_DATA\n"), 0o600); err != nil {
		t.Fatalf("WriteFile (.1): %v", err)
	}

	// Pad current file to exceed maxLogSize.
	if err := padToOversize(logPath); err != nil {
		t.Fatalf("padToOversize: %v", err)
	}

	// Reopen and trigger rotation.
	logger, err = NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger (re-open): %v", err)
	}
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "new-entry",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	logger.Close()

	// Verify: the old .1 backup was removed and replaced.
	data, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("ReadFile (.1): %v", err)
	}
	if strings.Contains(string(data), "THIS_IS_OLD_BACKUP_DATA") {
		t.Error("old .1 backup should have been removed during rotation")
	}
}

// TestRotation_PreservesOldDataInBackup verifies that original entries end up
// in the .1 backup file after rotation, while new entries go to a fresh file.
func TestRotation_PreservesOldDataInBackup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")
	rotatedPath := logPath + ".1"

	// Write 5 entries, close.
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := logger.Log(AuditEntry{
			Timestamp: time.Now(),
			Tool:      fmt.Sprintf("original-tool-%d", i),
			RiskLevel: "SAFE",
			Action:    "allowed",
		}); err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}
	logger.Close()

	// Pad to exceed maxLogSize.
	if err := padToOversize(logPath); err != nil {
		t.Fatalf("padToOversize: %v", err)
	}

	// Reopen, trigger rotation, then write to new file.
	logger, err = NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger (re-open): %v", err)
	}

	// This entry triggers rotation (goes into the file that becomes .1).
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "trigger-entry",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log (trigger): %v", err)
	}

	// This entry goes to the fresh file after rotation.
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "new-file-entry",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log (new file): %v", err)
	}
	logger.Close()

	// Verify: .1 backup contains the original entries.
	backupData, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("ReadFile (.1): %v", err)
	}
	for i := 0; i < 5; i++ {
		expected := fmt.Sprintf("original-tool-%d", i)
		if !strings.Contains(string(backupData), expected) {
			t.Errorf(".1 backup missing original entry %q", expected)
		}
	}

	// Verify: new file contains only the post-rotation entry.
	newData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile (new): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(newData)), "\n")
	if len(lines) != 1 {
		t.Errorf("new file should have 1 entry, got %d", len(lines))
	}
	if !strings.Contains(string(newData), "new-file-entry") {
		t.Errorf("new file should contain post-rotation entry:\n%s", string(newData))
	}
}

// TestLog_AfterRotation_NewFileCreated verifies that after rotation, subsequent
// Log() calls write to the newly opened file, not the rotated backup.
func TestLog_AfterRotation_NewFileCreated(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Create logger, write entry, close.
	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	logger.Log(AuditEntry{Timestamp: time.Now(), Tool: "pre", RiskLevel: "SAFE", Action: "allowed"})
	logger.Close()

	// Pad to exceed maxLogSize.
	if err := padToOversize(logPath); err != nil {
		t.Fatalf("padToOversize: %v", err)
	}

	// Reopen logger.
	logger, err = NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger (re-open): %v", err)
	}

	// First write triggers rotation.
	logger.Log(AuditEntry{Timestamp: time.Now(), Tool: "t1", RiskLevel: "SAFE", Action: "allowed"})

	// Second and third writes go to the new file.
	logger.Log(AuditEntry{Timestamp: time.Now(), Tool: "t2", RiskLevel: "SAFE", Action: "allowed"})
	logger.Log(AuditEntry{Timestamp: time.Now(), Tool: "t3", RiskLevel: "SAFE", Action: "allowed"})
	logger.Close()

	// New file should have exactly 2 entries (t2 and t3).
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("new file should have 2 entries (t2, t3), got %d lines", len(lines))
	}
	if !strings.Contains(string(data), "t2") {
		t.Error("new file should contain t2")
	}
	if !strings.Contains(string(data), "t3") {
		t.Error("new file should contain t3")
	}
	// t1 should NOT be in the new file (it was in the file that got rotated).
	if strings.Contains(string(data), `"tool":"t1"`) {
		t.Error("new file should NOT contain t1 (it was rotated to .1)")
	}
}

// ---------------------------------------------------------------------------
// JSON Tag Tests — new fields (Command, Outcome, Headless) and backward compat
// ---------------------------------------------------------------------------

// TestAuditEntry_NewFieldsJSONTags verifies that the custom MarshalJSON emits
// both the new short-form keys (ts, risk) and the legacy keys (timestamp,
// risk_level) alongside the new field keys (command, outcome, headless).
func TestAuditEntry_NewFieldsJSONTags(t *testing.T) {
	t.Parallel()

	entry := AuditEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Tool:      "shell_command",
		Command:   "ls -la /tmp",
		RiskLevel: "SAFE",
		Outcome:   "success",
		Headless:  true,
		SessionID: "sess-abc-123",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify new JSON keys are present.
	wantKeys := []string{"ts", "risk", "command", "outcome", "headless", "session_id"}
	for _, key := range wantKeys {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("JSON missing key %q: %s", key, string(data))
		}
	}

	// Verify backward-compat keys are also present.
	wantCompat := []string{"timestamp", "risk_level"}
	for _, key := range wantCompat {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("JSON missing backward-compat key %q: %s", key, string(data))
		}
	}
}

// TestAuditEntry_OmitsEmptyNewFields verifies that Command, Outcome, and
// Headless=false are omitted from JSON output (omitempty behavior).
func TestAuditEntry_OmitsEmptyNewFields(t *testing.T) {
	t.Parallel()

	entry := AuditEntry{
		Timestamp: time.Now(),
		Tool:      "ls",
		RiskLevel: "SAFE",
		// Command and Outcome are empty; Headless is false (default).
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// command should be omitted (omitempty).
	if strings.Contains(string(data), `"command"`) {
		t.Errorf("command should be omitted when empty:\n%s", string(data))
	}

	// outcome should be omitted (omitempty).
	if strings.Contains(string(data), `"outcome"`) {
		t.Errorf("outcome should be omitted when empty:\n%s", string(data))
	}

	// headless is only added to the marshal map when true (custom MarshalJSON).
	if strings.Contains(string(data), `"headless"`) {
		t.Errorf("headless should be omitted when false:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Post-Close Error
// ---------------------------------------------------------------------------

// TestLog_PostCloseReturnsError verifies that Log() returns an error after
// Close() has been called (the existing TestClose covers this too, but this
// is a focused assertion for the SP-049-3b changes).
func TestLog_PostCloseReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	logger.Close()

	err = logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "post-close",
		RiskLevel: "SAFE",
	})
	if err == nil {
		t.Error("expected error when calling Log() after Close(), got nil")
	}
}

// ---------------------------------------------------------------------------
// Backward-Compatible Unmarshal
// ---------------------------------------------------------------------------

// TestLog_BackwardCompatibleUnmarshal verifies that JSON with legacy field
// names (timestamp, risk_level) is correctly deserialized into AuditEntry.
func TestLog_BackwardCompatibleUnmarshal(t *testing.T) {
	t.Parallel()

	// JSON with old field names (timestamp, risk_level).
	oldJSON := `{
		"timestamp": "2024-01-01T00:00:00Z",
		"tool": "ls",
		"risk_level": "SAFE",
		"category": "read-only",
		"action": "allowed"
	}`

	var entry AuditEntry
	if err := json.Unmarshal([]byte(oldJSON), &entry); err != nil {
		t.Fatalf("Unmarshal legacy JSON: %v", err)
	}

	want := AuditEntry{
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Tool:      "ls",
		RiskLevel: "SAFE",
		Category:  "read-only",
		Action:    "allowed",
	}

	if !entry.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, want.Timestamp)
	}
	if entry.Tool != want.Tool {
		t.Errorf("Tool = %q, want %q", entry.Tool, want.Tool)
	}
	if entry.RiskLevel != want.RiskLevel {
		t.Errorf("RiskLevel = %q, want %q", entry.RiskLevel, want.RiskLevel)
	}
	if entry.Category != want.Category {
		t.Errorf("Category = %q, want %q", entry.Category, want.Category)
	}
	if entry.Action != want.Action {
		t.Errorf("Action = %q, want %q", entry.Action, want.Action)
	}
}
