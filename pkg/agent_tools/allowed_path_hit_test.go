package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ---------------------------------------------------------------------------
// SP-127 Phase 2.7: discriminated AllowedPathHit audit event tests
// ---------------------------------------------------------------------------

// capturingLogger implements filesystem.AuditLogger for test isolation.
// It captures all entries written via LogJSON so tests can assert on them
// without touching the filesystem.
type capturingLogger struct {
	entries []map[string]any
}

func (l *capturingLogger) Log(entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	l.entries = append(l.entries, m)
	return nil
}

func (l *capturingLogger) LogEntry(entry any) error { return l.Log(entry) }

func (l *capturingLogger) LogJSON(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	l.entries = append(l.entries, m)
	return nil
}

func (l *capturingLogger) Close() error { return nil }

// TestPrecheckFileAccess_EmitsAllowedPathHitAudit verifies that when a
// session-allowlisted path is allowed, PrecheckFileAccess emits the
// discriminated "allowed_path_hit" audit entry alongside the base "allowed"
// entry from the classifier.
func TestPrecheckFileAccess_EmitsAllowedPathHitAudit(t *testing.T) {
	t.Parallel()

	capturing := &capturingLogger{}
	ctx := filesystem.WithAuditLogger(context.Background(), capturing)

	// stubClassifier returns "allow" and reports the path as session-allowed.
	classifier := stubClassifier{
		decision:         "allow",
		isSessionAllowed: true,
	}

	_, verdict := PrecheckFileAccess(ctx, classifier, "write_file", "/data/somefile.txt")
	if verdict != "allow" {
		t.Fatalf("verdict = %q, want 'allow'", verdict)
	}

	// The classifier's auditPathDecision already emitted a base "allowed" entry.
	// PrecheckFileAccess should additionally emit the discriminated entry.
	found := false
	for _, entry := range capturing.entries {
		if entry["action"] == string(AuditActionAllowedPathHit) {
			found = true
			if entry["tool"] != "write_file" {
				t.Errorf("tool = %q, want 'write_file'", entry["tool"])
			}
			break
		}
	}
	if !found {
		t.Errorf("captured entries: %+v\nexpected 'allowed_path_hit' action", capturing.entries)
	}
}

// TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenWorkspacePath verifies
// that a workspace path (not session-allowlisted) does NOT emit
// AllowedPathHit — it only gets the base "allowed" entry from the classifier.
func TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenWorkspacePath(t *testing.T) {
	t.Parallel()

	capturing := &capturingLogger{}
	ctx := filesystem.WithAuditLogger(context.Background(), capturing)

	// stubClassifier returns "allow" but path is NOT session-allowed
	// (e.g. workspace root or /tmp — the boring case).
	classifier := stubClassifier{
		decision:         "allow",
		isSessionAllowed: false,
	}

	_, verdict := PrecheckFileAccess(ctx, classifier, "read_file", "/workspace/file.txt")
	if verdict != "allow" {
		t.Fatalf("verdict = %q, want 'allow'", verdict)
	}

	for _, entry := range capturing.entries {
		if entry["action"] == string(AuditActionAllowedPathHit) {
			t.Errorf("workspace path should not emit AllowedPathHit; got entry: %+v", entry)
		}
	}
}

// TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenDenied verifies that a
// denied path does NOT emit AllowedPathHit.
func TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenDenied(t *testing.T) {
	t.Parallel()

	capturing := &capturingLogger{}
	ctx := filesystem.WithAuditLogger(context.Background(), capturing)

	classifier := stubClassifier{
		decision:         "deny",
		isSessionAllowed: true, // irrelevant — verdict is deny
	}

	_, verdict := PrecheckFileAccess(ctx, classifier, "write_file", "/etc/passwd")
	if verdict != "deny" {
		t.Fatalf("verdict = %q, want 'deny'", verdict)
	}

	for _, entry := range capturing.entries {
		if entry["action"] == string(AuditActionAllowedPathHit) {
			t.Errorf("denied path should not emit AllowedPathHit; got entry: %+v", entry)
		}
	}
}

// TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenNilLogger verifies that
// when no audit logger is on ctx, PrecheckFileAccess does not panic and
// still returns the correct verdict.
func TestPrecheckFileAccess_NoAllowedPathHitAudit_WhenNilLogger(t *testing.T) {
	t.Parallel()

	classifier := stubClassifier{
		decision:         "allow",
		isSessionAllowed: true,
	}

	// ctx has no audit logger — use bare context.Background()
	_, verdict := PrecheckFileAccess(context.Background(), classifier, "write_file", "/data/somefile.txt")
	if verdict != "allow" {
		t.Errorf("verdict = %q, want 'allow'", verdict)
	}
}

// TestPrecheckFileAccess_AllowedPathHitWithRealLogger verifies the
// discriminated entry with a real on-disk logger (no mock).
func TestPrecheckFileAccess_AllowedPathHitWithRealLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	ctx := filesystem.WithAuditLogger(context.Background(), logger)

	classifier := stubClassifier{
		decision:         "allow",
		isSessionAllowed: true,
	}

	_, _ = PrecheckFileAccess(ctx, classifier, "edit_file", "/data/project/config.json")

	// Read the log file and verify the discriminated entry.
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	var found bool
	for _, line := range lines {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("json.Unmarshal(%q): %v", line, err)
		}
		if m["action"] == string(AuditActionAllowedPathHit) {
			found = true
			if m["tool"] != "edit_file" {
				t.Errorf("tool = %v, want 'edit_file'", m["tool"])
			}
			break
		}
	}
	if !found {
		t.Errorf("log file contents:\n%s\nexpected 'allowed_path_hit' entry", contents)
	}
}
