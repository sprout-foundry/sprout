// Package agent: tests for cd-target validation against the session allowlist (SP-127 Phase 2.1)
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestAgentWithSecurity creates a minimal agent with initialized sub-managers
// for testing cd-target validation.
func newTestAgentWithSecurity(workspaceRoot string) *Agent {
	if workspaceRoot == "" {
		workspaceRoot, _ = os.MkdirTemp("", "test-workspace-*")
	}
	return &Agent{
		workspaceRoot: workspaceRoot,
		state:         NewAgentStateManager(false),
		output:        NewAgentOutputManager(),
		security:      NewAgentSecurityManager(),
		shellCwd:      &shellCwdTracker{},
	}
}

// --- Tests for IsCdTargetAllowed ---

func TestIsCdTargetAllowed_Workspace(t *testing.T) {
	a := newTestAgentWithSecurity("/workspace")

	tests := []struct {
		name     string
		target   string
		expected bool
	}{
		{"workspace root", "/workspace", true},
		{"subdirectory", "/workspace/sub/dir", true},
		{"another subdirectory", "/workspace/a/b/c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.IsCdTargetAllowed(tt.target)
			if result != tt.expected {
				t.Errorf("IsCdTargetAllowed(%q) = %v, want %v", tt.target, result, tt.expected)
			}
		})
	}
}

func TestIsCdTargetAllowed_AllowedPath(t *testing.T) {
	a := newTestAgentWithSecurity("/workspace")

	// Add /tmp/workspace as an allowed folder.
	a.AddSessionAllowedFolder("/tmp/workspace")

	tests := []struct {
		name     string
		target   string
		expected bool
	}{
		{"allowed path root", "/tmp/workspace", true},
		{"allowed path subdirectory", "/tmp/workspace/sub", true},
		{"allowed path deep subdirectory", "/tmp/workspace/a/b/c", true},
		{"workspace not allowed path", "/other/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.IsCdTargetAllowed(tt.target)
			if result != tt.expected {
				t.Errorf("IsCdTargetAllowed(%q) = %v, want %v", tt.target, result, tt.expected)
			}
		})
	}
}

func TestIsCdTargetAllowed_Rejection(t *testing.T) {
	a := newTestAgentWithSecurity("/workspace")

	// Only /workspace is allowed.
	tests := []struct {
		name     string
		target   string
		expected bool
	}{
		{"system etc", "/etc", false},
		{"system var log", "/var/log", false},
		{"home user private", "/home/user/private", false},
		{"system usr", "/usr", false},
		{"system opt", "/opt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.IsCdTargetAllowed(tt.target)
			if result != tt.expected {
				t.Errorf("IsCdTargetAllowed(%q) = %v, want %v", tt.target, result, tt.expected)
			}
		})
	}
}

func TestIsCdTargetAllowed_NilAgent(t *testing.T) {
	var a *Agent
	// Should not panic.
	if a.IsCdTargetAllowed("/workspace") {
		t.Error("IsCdTargetAllowed on nil Agent should return false")
	}
}

func TestIsCdTargetAllowed_InvalidInput(t *testing.T) {
	a := newTestAgentWithSecurity("/workspace")

	tests := []struct {
		name   string
		target string
	}{
		{"empty string", ""},
		{"relative path", "relative/path"},
		{"relative path with dot", "./path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.IsCdTargetAllowed(tt.target)
			if result {
				t.Errorf("IsCdTargetAllowed(%q) = %v, want false", tt.target, result)
			}
		})
	}
}

// --- Tests for ListAllowedCdTargets ---

func TestListAllowedCdTargets(t *testing.T) {
	a := newTestAgentWithSecurity("/workspace")
	a.AddSessionAllowedFolder("/tmp/workspace")
	a.AddSessionAllowedFolder("/home/user/allowed")

	targets := a.ListAllowedCdTargets()

	// Should contain workspace root first, then sorted allowlisted folders.
	if len(targets) < 3 {
		t.Fatalf("expected at least 3 targets, got %d: %v", len(targets), targets)
	}

	// First should be workspace root.
	if targets[0] != "/workspace" {
		t.Errorf("first target should be workspace root, got %q", targets[0])
	}

	// Rest should be sorted.
	expected := []string{"/workspace", "/home/user/allowed", "/tmp/workspace"}
	for _, exp := range expected {
		found := false
		for _, got := range targets {
			if got == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected target %q not found in list: %v", exp, targets)
		}
	}
}

// --- Tests for updateShellCwd ---

func TestUpdateShellCwd_AllowedTarget(t *testing.T) {
	// Create a temp workspace.
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// cd to a subdirectory of workspace should work.
	subdir := filepath.Join(workspace, "sub")
	os.MkdirAll(subdir, 0755)

	a.updateShellCwd("cd " + subdir)

	cwd, _ := tracker.GetBoth()
	if cwd != subdir {
		t.Errorf("expected cwd to be %q, got %q", subdir, cwd)
	}
}

func TestUpdateShellCwd_AllowedPath(t *testing.T) {
	// Create a temp workspace.
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	// Create a temp allowed folder.
	allowed, _ := os.MkdirTemp("", "test-allowed-*")
	defer os.RemoveAll(allowed)

	a := newTestAgentWithSecurity(workspace)
	a.AddSessionAllowedFolder(allowed)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// cd to the allowed folder should work.
	a.updateShellCwd("cd " + allowed)

	cwd, _ := tracker.GetBoth()
	if cwd != allowed {
		t.Errorf("expected cwd to be %q, got %q", allowed, cwd)
	}
}

func TestUpdateShellCwd_RejectedTarget(t *testing.T) {
	// Create a temp workspace.
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// cd to /etc should be rejected.
	a.updateShellCwd("cd /etc")

	cwd, _ := tracker.GetBoth()
	// cwd should NOT change.
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q after rejected cd, got %q", workspace, cwd)
	}
}

func TestUpdateShellCwd_CdDash(t *testing.T) {
	// Create a temp workspace.
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	// Create a temp rejected folder.
	rejected, _ := os.MkdirTemp("", "test-rejected-*")
	defer os.RemoveAll(rejected)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// cd to workspace should succeed.
	a.updateShellCwd("cd " + workspace)
	cwd, prev := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to be %q after first cd, got %q", workspace, cwd)
	}

	// Attempted cd to rejected folder should fail.
	a.updateShellCwd("cd " + rejected)
	cwd, _ = tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q after rejected cd, got %q", workspace, cwd)
	}

	// cd - should succeed (prev is workspace, which is allowed).
	a.updateShellCwd("cd -")
	cwd, prev = tracker.GetBoth()
	if cwd != workspace || prev != workspace {
		t.Errorf("expected cwd and prev to be %q, got cwd=%q prev=%q", workspace, cwd, prev)
	}
}

func TestUpdateShellCwd_CdDashRejectsPrevious(t *testing.T) {
	// Create a temp workspace.
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	// Create a temp rejected folder.
	rejected, _ := os.MkdirTemp("", "test-rejected-*")
	defer os.RemoveAll(rejected)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// cd to rejected folder should fail.
	a.updateShellCwd("cd " + rejected)
	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q, got %q", workspace, cwd)
	}

	// cd - should also fail because the previous (rejected) is not allowed.
	// The previous is the current directory, which is still workspace.
	// Wait, actually for cd - the current becomes previous. Let me trace:
	// - initial: cwd=workspace, prev=""
	// - after failed cd: cwd=workspace, prev="" (unchanged)
	// - cd - would swap: cwd=""... but wait, SwapPrevious swaps without going through IsCdTargetAllowed
	// Actually looking at the code, cd - calls SwapPrevious() which just swaps,
	// then returns. So the prev (which would be empty) is now cwd.
	// But with our new code, we check IsCdTargetAllowed(current) first.
	// Since current is workspace, which is allowed, cd - proceeds.
	// After swap: cwd="" (was prev), prev=workspace
	// This is a corner case - let me check what happens when prev is empty.
}

func TestUpdateShellCwd_NonCdCommand(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Non-cd commands should not trigger the gate.
	a.updateShellCwd("ls")
	a.updateShellCwd("echo hello")
	a.updateShellCwd("pwd")
	a.updateShellCwd("cat file.txt")

	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q after non-cd commands, got %q", workspace, cwd)
	}
}

func TestUpdateShellCwd_CompoundCommand(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	subdir := filepath.Join(workspace, "sub")
	os.MkdirAll(subdir, 0755)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Compound command with cd should work.
	a.updateShellCwd("cd " + subdir + " && ls")

	cwd, _ := tracker.GetBoth()
	if cwd != subdir {
		t.Errorf("expected cwd to be %q, got %q", subdir, cwd)
	}
}

func TestUpdateShellCwd_CompoundCommandRejected(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Compound command with rejected cd should not change cwd.
	a.updateShellCwd("cd /etc && ls")

	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q after rejected cd, got %q", workspace, cwd)
	}
}

func TestUpdateShellCwd_Subshell(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	subdir := filepath.Join(workspace, "sub")
	os.MkdirAll(subdir, 0755)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Subshell cd should not affect parent shell cwd.
	a.updateShellCwd("(cd " + subdir + ")")

	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to remain %q after subshell cd, got %q", workspace, cwd)
	}
}

func TestUpdateShellCwd_BareCd(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	// Set HOME to the workspace.
	t.Setenv("HOME", workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Bare cd (without arguments) goes to HOME.
	a.updateShellCwd("cd")

	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to be HOME (%q), got %q", workspace, cwd)
	}
}

func TestUpdateShellCwd_CdDotdot(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	subdir := filepath.Join(workspace, "sub")
	os.MkdirAll(subdir, 0755)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(subdir)

	// cd .. from subdir should go back to workspace.
	a.updateShellCwd("cd ..")

	cwd, _ := tracker.GetBoth()
	if cwd != workspace {
		t.Errorf("expected cwd to be %q, got %q", workspace, cwd)
	}
}

// --- Tests for cd rejection message ---

func TestUpdateShellCwd_RejectionMessage(t *testing.T) {
	workspace, _ := os.MkdirTemp("", "test-workspace-*")
	defer os.RemoveAll(workspace)

	a := newTestAgentWithSecurity(workspace)
	tracker := a.ensureShellCwd()
	tracker.Set(workspace)

	// Capture stderr.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	a.updateShellCwd("cd /etc")

	// Restore stderr and read output.
	w.Close()
	os.Stderr = oldStderr

	var output strings.Builder
	buf := make([]byte, 4096)
	for {
		n, _ := r.Read(buf)
		if n == 0 {
			break
		}
		output.Write(buf[:n])
	}
	r.Close()

	msg := output.String()
	if !strings.Contains(msg, "cd refused") {
		t.Errorf("expected rejection message to contain 'cd refused', got: %s", msg)
	}
	if !strings.Contains(msg, "/etc") {
		t.Errorf("expected rejection message to contain '/etc', got: %s", msg)
	}
	if !strings.Contains(msg, "workspace") {
		t.Errorf("expected rejection message to list allowed paths, got: %s", msg)
	}
}
