package history

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// captureLog runs fn with the standard logger redirected to a buffer.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	fn()
	return buf.String()
}

// TestAuditRevertWrite_NoStackByDefault: the per-write audit line must stay
// concise by default — dumping debug.Stack() for every file in a bulk
// recover/revert floods the console (the failure this guards against).
func TestAuditRevertWrite_NoStackByDefault(t *testing.T) {
	SetAuditStackEnabled(false)
	defer SetAuditStackEnabled(false)

	out := captureLog(t, func() { AuditRevertWrite("revertOne", "/w/a.go", "OriginalCode") })
	if !strings.Contains(out, "[SP077-AUDIT] revert-write caller=revertOne") {
		t.Errorf("expected the audit line, got: %q", out)
	}
	if strings.Contains(out, "stack trace") || strings.Contains(out, "goroutine ") {
		t.Errorf("stack trace must not be logged by default:\n%s", out)
	}
	if strings.Count(out, "\n") > 1 {
		t.Errorf("expected a single-line audit entry, got:\n%s", out)
	}
}

// TestAuditRevertWrite_StackWhenEnabled: with the gate on, the stack is included.
func TestAuditRevertWrite_StackWhenEnabled(t *testing.T) {
	SetAuditStackEnabled(true)
	defer SetAuditStackEnabled(false)

	out := captureLog(t, func() { AuditRevertWrite("revertOne", "/w/a.go", "OriginalCode") })
	if !strings.Contains(out, "--- stack trace ---") {
		t.Errorf("expected a stack trace when enabled, got:\n%s", out)
	}
	if !strings.Contains(out, "goroutine ") {
		t.Errorf("expected a goroutine dump when enabled, got:\n%s", out)
	}
}
