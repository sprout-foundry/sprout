//go:build !js

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// ---------------------------------------------------------------------------
// resolveAuditLogPath
// ---------------------------------------------------------------------------

func TestResolveAuditLogPath_DefaultsUnderStateDir(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	got := resolveAuditLogPath(nil)
	want := filepath.Join(stateDir, shellAuditFileName)
	if got != want {
		t.Errorf("resolveAuditLogPath(nil) = %q, want %q", got, want)
	}
}

func TestResolveAuditLogPath_ConfigDirOverride(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())

	auditDir := filepath.Join(t.TempDir(), "audit")
	cfg := &configuration.Config{ComputerUse: &configuration.ComputerUseConfig{AuditLogDir: auditDir}}

	got := resolveAuditLogPath(cfg)
	want := filepath.Join(auditDir, shellAuditFileName)
	if got != want {
		t.Errorf("resolveAuditLogPath(cfg) = %q, want %q", got, want)
	}
}

func TestResolveAuditLogPath_BlankConfigDirFallsBackToStateDir(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	cfg := &configuration.Config{ComputerUse: &configuration.ComputerUseConfig{AuditLogDir: "   "}}
	got := resolveAuditLogPath(cfg)
	want := filepath.Join(stateDir, shellAuditFileName)
	if got != want {
		t.Errorf("resolveAuditLogPath(blank dir) = %q, want %q", got, want)
	}
}

// TestResolveAuditLogPath_MatchesCLIDefault pins the invariant that the agent
// writes to the same file `sprout audit tail` reads: both resolve to
// <state dir>/shell-audit.jsonl when no config override is set.
func TestResolveAuditLogPath_MatchesCLIDefault(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	got := resolveAuditLogPath(nil)

	// Reproduce cmd/audit.go's defaultAuditLogPath resolution.
	cliStateDir, err := envutil.StateDir()
	if err != nil {
		t.Fatalf("envutil.StateDir: %v", err)
	}
	want := filepath.Join(cliStateDir, shellAuditFileName)
	if got != want {
		t.Errorf("agent audit path %q does not match CLI default %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// initAuditLogger / closeAuditLogger
// ---------------------------------------------------------------------------

func TestInitAuditLogger_OpensFileWith0600(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := NewTestAgent()
	a.initAuditLogger()
	defer a.closeAuditLogger()

	if a.GetAuditLogger() == nil {
		t.Fatal("expected audit logger to be attached after initAuditLogger")
	}

	logPath := filepath.Join(stateDir, shellAuditFileName)
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("audit log file not created at %s: %v", logPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %o, want 0600", perm)
	}
}

func TestInitAuditLogger_SetsPackageLogger(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())

	a := NewTestAgent()
	a.initAuditLogger()
	defer a.closeAuditLogger()

	// The package-level classifier logger must be wired so ClassifyToolCall
	// writes reach the file.
	if tools.ClassifierAuditLogger() == nil {
		t.Fatal("expected package-level audit logger to be set by initAuditLogger")
	}
}

func TestCloseAuditLogger_ClosesAndDetaches(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())

	a := NewTestAgent()
	a.initAuditLogger()
	if a.GetAuditLogger() == nil {
		t.Fatal("expected audit logger after init")
	}

	a.closeAuditLogger()

	if a.GetAuditLogger() != nil {
		t.Error("expected audit logger to be nil after close")
	}
	if tools.ClassifierAuditLogger() != nil {
		t.Error("expected package-level audit logger to be cleared after close")
	}

	// Idempotent: a second close must not panic.
	a.closeAuditLogger()
}

func TestInitAuditLogger_NilAgentNoPanic(t *testing.T) {
	var a *Agent
	a.initAuditLogger()
	a.closeAuditLogger()
}

// ---------------------------------------------------------------------------
// Denied shell command lands in the file
// ---------------------------------------------------------------------------

// TestDeniedShellCommandLandsInAuditFile drives the production security path
// (unifiedSecurityGate → logSecurityDecision) for a hard-blocked command with
// a logger created by initAuditLogger, then reads the file back and asserts a
// denied entry with the expected action/fields is present.
func TestDeniedShellCommandLandsInAuditFile(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	restore := SetTestStateDirHook(filepath.Join(stateDir, "sessions"))
	defer restore()

	a := newTestAgent(t)
	defer a.Shutdown()

	if a.GetAuditLogger() == nil {
		t.Fatal("agent should have an audit logger after construction")
	}

	// A critical operation hard-blocks unconditionally and is logged as a
	// security decision.
	err := a.unifiedSecurityGate("shell_command", map[string]interface{}{"command": "rm -rf /"})
	if err == nil {
		t.Fatal("expected unifiedSecurityGate to block 'rm -rf /'")
	}

	logPath := filepath.Join(stateDir, shellAuditFileName)
	entries := readAuditEntries(t, logPath)
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry in the file, got none")
	}

	var found bool
	for _, e := range entries {
		if e.Tool == "shell_command" && e.Action == "blocked" {
			found = true
			if e.RiskLevel == "" {
				t.Error("audit entry RiskLevel is empty")
			}
			if e.Source != "unified-gate" {
				t.Errorf("Source = %q, want 'unified-gate'", e.Source)
			}
		}
	}
	if !found {
		t.Fatalf("expected a blocked shell_command entry, got: %+v", entries)
	}
}

// TestDeniedShellCommand_ClassifierEntryLandsInFile verifies the package-level
// classifier logger (set by initAuditLogger) records a denied classification.
func TestDeniedShellCommand_ClassifierEntryLandsInFile(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := NewTestAgent()
	a.initAuditLogger()
	defer a.closeAuditLogger()

	// ClassifyToolCall logs through the package-level logger.
	tools.ClassifyToolCall("shell_command", map[string]interface{}{"command": "rm -rf /"})

	logPath := filepath.Join(stateDir, shellAuditFileName)
	entries := readAuditEntries(t, logPath)
	if len(entries) == 0 {
		t.Fatal("expected a classifier audit entry, got none")
	}
	if entries[0].Source != "classifier" {
		t.Errorf("Source = %q, want 'classifier'", entries[0].Source)
	}
	if entries[0].Action != "denied" {
		t.Errorf("Action = %q, want 'denied'", entries[0].Action)
	}
}

// TestAuditFileIsValidJSONL asserts every line written to the audit file is a
// parseable JSON object, so `sprout audit tail` can read it.
func TestAuditFileIsValidJSONL(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := NewTestAgent()
	a.initAuditLogger()
	defer a.closeAuditLogger()

	tools.ClassifyToolCall("shell_command", map[string]interface{}{"command": "ls -la"})
	tools.ClassifyToolCall("shell_command", map[string]interface{}{"command": "rm -rf /"})

	logPath := filepath.Join(stateDir, shellAuditFileName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("expected audit log lines")
	}
	for i, line := range lines {
		var entry tools.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nLine: %s", i+1, err, line)
		}
	}
}

// TestCloseAuditLogger_DoesNotClobberOtherAgents verifies the package-level
// classifier logger is only detached by the agent that currently owns it. A
// second agent's logger must survive the first agent's shutdown.
func TestCloseAuditLogger_DoesNotClobberOtherAgents(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a1 := NewTestAgent()
	a1.initAuditLogger()
	l1 := a1.GetAuditLogger()
	if l1 == nil {
		t.Fatal("a1 should have an audit logger")
	}

	a2 := NewTestAgent()
	a2.initAuditLogger()
	l2 := a2.GetAuditLogger()
	if l2 == nil {
		t.Fatal("a2 should have an audit logger")
	}

	// a2 installed its logger last, so it owns the process-global pointer.
	// Closing a1 must not detach a2's logger.
	a1.closeAuditLogger()
	if tools.ClassifierAuditLogger() != l2 {
		t.Error("closing a1 must not clobber a2's package-level logger")
	}

	a2.closeAuditLogger()
	if tools.ClassifierAuditLogger() != nil {
		t.Error("expected package-level logger cleared after owner closes")
	}
}

// ---------------------------------------------------------------------------
// Construction wiring
// ---------------------------------------------------------------------------

// TestNewAgentWithClient_AttachesAuditLogger pins that the production
// construction path opens the audit log, so every LogJSON call site has a
// logger to write to.
func TestNewAgentWithClient_AttachesAuditLogger(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := newTestAgent(t)
	defer a.Shutdown()

	if a.GetAuditLogger() == nil {
		t.Fatal("NewAgentWithClient path should attach an audit logger")
	}

	logPath := filepath.Join(stateDir, shellAuditFileName)
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("audit log file should exist after agent construction: %v", err)
	}
}

// TestShutdownClosesAuditLogger verifies the file handle is released on
// Shutdown: a subsequent write through the detached logger is a no-op and the
// agent no longer holds one.
func TestShutdownClosesAuditLogger(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := newTestAgent(t)
	logger := a.GetAuditLogger()
	if logger == nil {
		t.Fatal("expected audit logger after construction")
	}

	a.Shutdown()

	if a.GetAuditLogger() != nil {
		t.Error("expected audit logger to be nil after Shutdown")
	}
	if tools.ClassifierAuditLogger() != nil {
		t.Error("expected package-level audit logger cleared after Shutdown")
	}
	// Writing through the closed logger must fail (handle released).
	if err := logger.LogJSON([]byte(`{"tool":"x"}`)); err == nil {
		t.Error("expected write to closed audit logger to fail")
	}
}
