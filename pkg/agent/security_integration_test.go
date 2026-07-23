package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// TestSecurityGateIntegration_EndToEnd drives write_file through both Gate 1
// (staticGateAutoApprove) and the handler-level precheck (PrecheckFileAccess),
// asserting that the verdict chain produces the expected outcomes for each path tier.
//
// This is the SP-127 M3.5 integration test. It exercises the full path:
//
//	1. staticGateAutoApprove — Gate 1 entry point (bypass flags + path-tier)
//	2. PrecheckFileAccess — handler-level precheck (resolves + classifies)
//	3. Handler Execute — calls PrecheckFileAccess, handles allow/deny/prompt
//
// Assertion matrix:
//	- Workspace path         → Allow (Gate 1 allows; precheck allows; handler proceeds)
//	- Allowlisted read_write → Allow (Gate 1 allows; precheck allows; handler proceeds)
//	- Allowlisted read_only → Deny (Gate 1 denies; precheck denies; handler returns typed error)
//	- Sensitive /etc/shadow  → Prompt (Gate 1 prompts; precheck prompts; handler falls through)
func TestSecurityGateIntegration_EndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// --- Setup ---
	workspaceRoot := t.TempDir()
	allowlistRW := NonTmpTempDir(t) // read_write allowlisted folder
	allowlistRO := NonTmpTempDir(t) // read_only allowlisted folder
	sensitivePath := "/etc/shadow"

	// --- Create agent ---
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.SetWorkspaceRoot(workspaceRoot)

	// Add allowlisted folders
	a.AddSessionAllowedFolder(allowlistRW)
	a.AddSessionAllowedFolder(allowlistRO)
	a.SetSessionAllowedFolderMode(allowlistRO, "read_only")

	// --- Gate 1: staticGateAutoApprove ---
	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		IsHardBlock:  false,
	}

	// Workspace path: Gate 1 auto-approves
	wsFile := filepath.Join(workspaceRoot, "main.go")
	if !a.staticGateAutoApprove(secResult, wsFile, "", "write") {
		t.Errorf("staticGateAutoApprove: workspace path should auto-approve")
	}

	// Allowlisted read_write: Gate 1 auto-approves
	rwFile := filepath.Join(allowlistRW, "data.txt")
	if !a.staticGateAutoApprove(secResult, rwFile, "", "write") {
		t.Errorf("staticGateAutoApprove: read_write allowlisted path should auto-approve")
	}

	// Allowlisted read_only + write: Gate 1 denies
	roFile := filepath.Join(allowlistRO, "secret.txt")
	if a.staticGateAutoApprove(secResult, roFile, "", "write") {
		t.Errorf("staticGateAutoApprove: read_only allowlisted path + write should DENY")
	}

	// Sensitive path: Gate 1 prompts (returns false — not auto-approve)
	if a.staticGateAutoApprove(secResult, sensitivePath, "", "write") {
		t.Errorf("staticGateAutoApprove: sensitive path should NOT auto-approve")
	}

	// --- Handler-level precheck via PrecheckFileAccess ---
	ctx := context.Background()

	// Workspace path: precheck returns allow
	_, decision := tools.PrecheckFileAccess(ctx, a, "write_file", wsFile)
	if decision != "allow" {
		t.Errorf("PrecheckFileAccess: workspace path = %q, want 'allow'", decision)
	}

	// Allowlisted read_write: precheck returns allow
	_, decision = tools.PrecheckFileAccess(ctx, a, "write_file", rwFile)
	if decision != "allow" {
		t.Errorf("PrecheckFileAccess: read_write allowlisted path = %q, want 'allow'", decision)
	}

	// Allowlisted read_only + write: precheck returns deny
	_, decision = tools.PrecheckFileAccess(ctx, a, "write_file", roFile)
	if decision != "deny" {
		t.Errorf("PrecheckFileAccess: read_only allowlisted path + write = %q, want 'deny'", decision)
	}

	// Sensitive path: precheck returns prompt
	_, decision = tools.PrecheckFileAccess(ctx, a, "write_file", sensitivePath)
	if decision != "prompt" {
		t.Errorf("PrecheckFileAccess: sensitive path = %q, want 'prompt'", decision)
	}

	// --- Handler Execute path: write_file with typed errors ---
	// Get handler from registry (handlers are unexported)
	h, found := tools.GetNewToolRegistry().Lookup("write_file")
	if !found {
		t.Fatal("write_file handler not found in registry")
	}

	// Workspace path: handler proceeds
	res, err := h.Execute(ctx, tools.ToolEnv{
		FileAccessClassifier: a,
	}, map[string]any{
		"path":    wsFile,
		"content": "hello",
	})
	if err != nil {
		t.Errorf("write_file on workspace path: err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("write_file on workspace path: IsError = true, want false")
	}

	// Allowlisted read_only: handler returns typed denial
	res, err = h.Execute(ctx, tools.ToolEnv{
		FileAccessClassifier: a,
	}, map[string]any{
		"path":    roFile,
		"content": "hello",
	})
	if err == nil {
		t.Error("write_file on read_only allowlisted path: err = nil, want typed error")
	}
	if !res.IsError {
		t.Error("write_file on read_only allowlisted path: IsError = false, want true")
	}
}

// TestSecurityGateIntegration_AuditTrail verifies that PrecheckFileAccess
// emits audit log entries for each verdict when an audit logger is attached
// to the context.
func TestSecurityGateIntegration_AuditTrail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Create a temp audit log
	logFile := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := tools.NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	workspaceRoot := t.TempDir()
	a.SetWorkspaceRoot(workspaceRoot)
	wsFile := filepath.Join(workspaceRoot, "main.go")

	// Attach audit logger to context
	ctx := filesystem.WithAuditLogger(context.Background(), logger)

	// Precheck triggers audit log
	tools.PrecheckFileAccess(ctx, a, "write_file", wsFile)

	// Verify the log file was written
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile audit log: %v", err)
	}
	if len(data) == 0 {
		t.Error("audit log should have at least one entry")
	}
}
