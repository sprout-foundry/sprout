//go:build !js

package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// readAuditEntries reads a JSONL audit log file and returns parsed entries.
//
// It lives in its own !js file (rather than security_audit_test.go, which is
// !windows) because security_audit_init_test.go compiles on Windows and calls
// it.
func readAuditEntries(t *testing.T, logPath string) []tools.AuditEntry {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entries []tools.AuditEntry
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry tools.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse audit entry: %v\nLine: %s", err, line)
		}
		entries = append(entries, entry)
	}
	return entries
}
