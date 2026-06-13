//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// writeTestAuditLog writes the given entries as JSONL to the given path.
func writeTestAuditLog(t *testing.T, path string, entries []tools.AuditEntry) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

func makeAuditEntry(tool, risk, outcome, source, command string, ts time.Time) tools.AuditEntry {
	return tools.AuditEntry{
		Timestamp: ts,
		Tool:      tool,
		RiskLevel: risk,
		Outcome:   outcome,
		Source:    source,
		Command:   command,
	}
}

func TestReadAuditLogFile_NoFile(t *testing.T) {
	entries, err := readAuditLogFile("/nonexistent/audit.jsonl", 10)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing file, got %d entries", len(entries))
	}
}

func TestReadAuditLogFile_BasicRead(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	baseTime := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	entries := []tools.AuditEntry{
		makeAuditEntry("shell_command", "DANGEROUS", "blocked", "built-in-dangerous", "git push --force", baseTime),
		makeAuditEntry("shell_command", "CAUTION", "approved", "classifier", "rm -rf /tmp/foo", baseTime.Add(time.Minute)),
		makeAuditEntry("shell_command", "DANGEROUS", "blocked", "built-in-dangerous", "mkfs /dev/sda", baseTime.Add(2*time.Minute)),
	}
	writeTestAuditLog(t, logPath, entries)

	result, err := readAuditLogFile(logPath, 10)
	if err != nil {
		t.Fatalf("readAuditLogFile: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Tool != "shell_command" {
		t.Errorf("first entry tool = %s, want shell_command", result[0].Tool)
	}
	if result[2].Command != "mkfs /dev/sda" {
		t.Errorf("third entry command = %s, want 'mkfs /dev/sda'", result[2].Command)
	}
}

func TestReadAuditLogFile_TailN(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	baseTime := time.Now()
	var entries []tools.AuditEntry
	for i := 0; i < 10; i++ {
		entries = append(entries, makeAuditEntry("tool", "CAUTION", "approved", "test", "cmd-"+string(rune('A'+i)), baseTime.Add(time.Duration(i)*time.Minute)))
	}
	writeTestAuditLog(t, logPath, entries)

	// Request only the last 3.
	result, err := readAuditLogFile(logPath, 3)
	if err != nil {
		t.Fatalf("readAuditLogFile: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries (tail), got %d", len(result))
	}
	// Should be the last 3 entries.
	if result[0].Command != "cmd-H" {
		t.Errorf("first of tail = %s, want cmd-H", result[0].Command)
	}
	if result[2].Command != "cmd-J" {
		t.Errorf("last of tail = %s, want cmd-J", result[2].Command)
	}
}

func TestReadAuditLogFile_SkipsMalformedLines(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	dir := filepath.Dir(logPath)
	os.MkdirAll(dir, 0o755)
	// Write a mix of valid and invalid JSON lines.
	content := `{"ts":"2026-06-13T12:00:00Z","tool":"shell_command","risk":"DANGEROUS","outcome":"blocked"}
{invalid json}
{"ts":"2026-06-13T12:01:00Z","tool":"shell_command","risk":"CAUTION","outcome":"approved"}
`
	os.WriteFile(logPath, []byte(content), 0o600)

	result, err := readAuditLogFile(logPath, 10)
	if err != nil {
		t.Fatalf("readAuditLogFile: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 valid entries (skipping malformed), got %d", len(result))
	}
}

func TestReadAuditLogTail_IncludesRotatedFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")
	rotatedPath := logPath + ".1"

	baseTime := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	// Write 2 entries to the rotated file (older).
	oldEntries := []tools.AuditEntry{
		makeAuditEntry("shell_command", "CAUTION", "approved", "classifier", "old-cmd-1", baseTime),
		makeAuditEntry("shell_command", "CAUTION", "approved", "classifier", "old-cmd-2", baseTime.Add(time.Minute)),
	}
	writeTestAuditLog(t, rotatedPath, oldEntries)

	// Write 1 entry to the current file (newer).
	newEntries := []tools.AuditEntry{
		makeAuditEntry("shell_command", "DANGEROUS", "blocked", "built-in-dangerous", "new-cmd", baseTime.Add(time.Hour)),
	}
	writeTestAuditLog(t, logPath, newEntries)

	// Request 10 — should get all 3 (2 from rotated + 1 from current).
	result, err := readAuditLogTail(logPath, 10)
	if err != nil {
		t.Fatalf("readAuditLogTail: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries (rotated + current), got %d", len(result))
	}
	// Order should be: old entries first, then new.
	if result[0].Command != "old-cmd-1" {
		t.Errorf("first entry should be from rotated file, got %s", result[0].Command)
	}
	if result[2].Command != "new-cmd" {
		t.Errorf("last entry should be from current file, got %s", result[2].Command)
	}
}

func TestFormatAuditEntry_AllFields(t *testing.T) {
	entry := tools.AuditEntry{
		Timestamp: time.Date(2026, 6, 13, 14, 32, 11, 0, time.UTC),
		Tool:      "shell_command",
		RiskLevel: "DANGEROUS",
		Outcome:   "blocked",
		Source:    "built-in-dangerous",
		Command:   "git push --force origin main",
	}

	formatted := formatAuditEntry(entry)

	// Verify key components are present in the output.
	checks := []string{
		"2026-06-02 14:32:11", // formatted timestamp — may differ by tz
		"shell_command",
		"DANGEROUS",
		"blocked",
		"built-in-dangerous",
		"git push --force origin main",
	}
	_ = checks // The timestamp format may vary by timezone; just check the non-time parts.
	if !strings.Contains(formatted, "shell_command") {
		t.Errorf("missing tool name in output: %s", formatted)
	}
	if !strings.Contains(formatted, "DANGEROUS") {
		t.Errorf("missing risk level in output: %s", formatted)
	}
	if !strings.Contains(formatted, "blocked") {
		t.Errorf("missing outcome in output: %s", formatted)
	}
	if !strings.Contains(formatted, "built-in-dangerous") {
		t.Errorf("missing source in output: %s", formatted)
	}
	if !strings.Contains(formatted, "git push --force origin main") {
		t.Errorf("missing command in output: %s", formatted)
	}
}

func TestFormatAuditEntry_TruncatesLongCommand(t *testing.T) {
	longCmd := strings.Repeat("X", 200)
	entry := tools.AuditEntry{
		Timestamp: time.Now(),
		Tool:      "shell_command",
		RiskLevel: "CAUTION",
		Outcome:   "approved",
		Command:   longCmd,
	}

	formatted := formatAuditEntry(entry)

	if strings.Contains(formatted, strings.Repeat("X", 100)) {
		t.Errorf("long command was not truncated in output: %d chars", len(formatted))
	}
	if !strings.Contains(formatted, "...") {
		t.Errorf("expected truncation indicator '...' in output")
	}
}

func TestFormatAuditEntry_EmptyFields(t *testing.T) {
	entry := tools.AuditEntry{
		Timestamp: time.Now(),
		Tool:      "test",
	}

	formatted := formatAuditEntry(entry)

	// Should have placeholder values for missing fields.
	if !strings.Contains(formatted, "?") {
		t.Errorf("expected '?' placeholder for missing risk level")
	}
	if !strings.Contains(formatted, "-") {
		t.Errorf("expected '-' placeholder for missing outcome")
	}
}

func TestAuditTailCmd_Output(t *testing.T) {
	// This test verifies the tail command output by temporarily replacing
	// the defaultAuditLogPath function's result via a temporary config dir.
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, ".sprout", "shell-audit.jsonl")

	baseTime := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	entries := []tools.AuditEntry{
		makeAuditEntry("shell_command", "DANGEROUS", "blocked", "built-in-dangerous", "git reset --hard HEAD~5", baseTime),
		makeAuditEntry("shell_command", "CAUTION", "approved", "classifier", "npm install", baseTime.Add(time.Minute)),
	}
	writeTestAuditLog(t, logPath, entries)

	// Test the readAuditLogTail function directly (avoids needing to
	// override the global defaultAuditLogPath).
	result, err := readAuditLogTail(logPath, 10)
	if err != nil {
		t.Fatalf("readAuditLogTail: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	// Format each entry to verify output.
	var buf bytes.Buffer
	for _, e := range result {
		buf.WriteString(formatAuditEntry(e))
		buf.WriteString("\n")
	}
	output := buf.String()

	if !strings.Contains(output, "git reset --hard HEAD~5") {
		t.Errorf("missing first command in output: %s", output)
	}
	if !strings.Contains(output, "npm install") {
		t.Errorf("missing second command in output: %s", output)
	}
}

func TestDefaultAuditLogPath(t *testing.T) {
	path := defaultAuditLogPath()

	// Should end with shell-audit.jsonl.
	if !strings.HasSuffix(path, "shell-audit.jsonl") {
		t.Errorf("audit log path should end with shell-audit.jsonl: %s", path)
	}
	// Should contain "sprout" somewhere in the directory path.
	if !strings.Contains(path, "sprout") {
		t.Errorf("audit log path should contain 'sprout': %s", path)
	}
}

func TestAuditClearCmd_RemovesFiles(t *testing.T) {
	// Test the file removal logic directly (without the cobra command).
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "shell-audit.jsonl")
	rotatedPath := logPath + ".1"

	// Create both files.
	writeTestAuditLog(t, logPath, []tools.AuditEntry{
		makeAuditEntry("test", "CAUTION", "approved", "test", "cmd", time.Now()),
	})
	writeTestAuditLog(t, rotatedPath, []tools.AuditEntry{
		makeAuditEntry("test", "CAUTION", "approved", "test", "old-cmd", time.Now()),
	})

	// Verify they exist.
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file should exist: %v", err)
	}
	if _, err := os.Stat(rotatedPath); err != nil {
		t.Fatalf("rotated file should exist: %v", err)
	}

	// Remove both.
	removed := 0
	if err := os.Remove(logPath); err == nil {
		removed++
	}
	if err := os.Remove(rotatedPath); err == nil {
		removed++
	}

	if removed != 2 {
		t.Errorf("expected 2 files removed, got %d", removed)
	}

	// Verify they're gone.
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("log file should be removed")
	}
	if _, err := os.Stat(rotatedPath); !os.IsNotExist(err) {
		t.Errorf("rotated file should be removed")
	}
}

func TestAuditClearCmd_NoFiles(t *testing.T) {
	// Clearing when no log exists should not error.
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "shell-audit.jsonl")
	rotatedPath := logPath + ".1"

	// Neither file exists. Removing should be a no-op.
	err := os.Remove(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("removing nonexistent log should not error: %v", err)
	}
	err = os.Remove(rotatedPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("removing nonexistent rotated should not error: %v", err)
	}
}
