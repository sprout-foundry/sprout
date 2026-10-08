package tools

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// --unsafe-shell bypass in the shell handler's own gate
//
// Gate 1's approval cascade (pkg/agent/tool_security.go) already honors
// --unsafe-shell, but a headless run has no approval surface: the command
// never reaches that cascade and is denied at the handler's own gate with
// reason "no_channel", so the edit never lands. These tests pin the
// handler-level bypass so the flag is effective for headless shell commands.
//
// The bypass mirrors the approval broker's own unsafe-shell condition
// (pkg/agent/approvals/broker.go): non-hard-block, risk != DANGEROUS, and no
// intent confirmation. It lifts exactly the CAUTION-tier shell prompt.
//
// Classification reference (verified against ClassifyToolCallWithWorkspace):
//   - "printf 'x' > src/added.txt" → CAUTION, ShouldPrompt=true,  IsHardBlock=false
//   - "chmod 777 /etc/passwd"      → DANGEROUS, ShouldBlock=true, IsHardBlock=false
//   - "mkfs.ext4 /dev/sda1"        → DANGEROUS, ShouldBlock=true, IsHardBlock=true
// ---------------------------------------------------------------------------

// TestShellHandler_UnsafeShell_CautionSkipsPrompt verifies that a
// CAUTION-tier shell command (a redirect write) executes without an approval
// request when the session opted into --unsafe-shell.
func TestShellHandler_UnsafeShell_CautionSkipsPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/src", 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	requireCautionTier(t, dir, "printf 'safe\\n' > src/added.txt")

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	env.UnsafeShellMode = true

	res, err := h.Execute(ctx, env, map[string]any{"command": "printf 'safe\\n' > src/added.txt"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("CAUTION command should run under --unsafe-shell, got: %s", res.Output)
	}
	if got := len(am.calls); got != 0 {
		t.Fatalf("--unsafe-shell should skip the CAUTION prompt, got %d calls", got)
	}
	if _, statErr := os.Stat(dir + "/src/added.txt"); statErr != nil {
		t.Fatalf("command did not write the file: %v", statErr)
	}
}

// TestShellHandler_UnsafeShellOff_CautionPrompts verifies that the same
// CAUTION-tier command still requests approval when --unsafe-shell is off.
func TestShellHandler_UnsafeShellOff_CautionPrompts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/src", 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	requireCautionTier(t, dir, "printf 'safe\\n' > src/added.txt")

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	// UnsafeShellMode defaults to false.

	if _, err := h.Execute(ctx, env, map[string]any{"command": "printf 'safe\\n' > src/added.txt"}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := len(am.calls); got != 1 {
		t.Fatalf("without --unsafe-shell the CAUTION command should prompt, got %d calls", got)
	}
}

// TestShellHandler_UnsafeShell_DangerousStillPrompts verifies that a
// DANGEROUS, non-hard-block command is still gated under --unsafe-shell —
// the bypass must not widen past the CAUTION tier.
func TestShellHandler_UnsafeShell_DangerousStillPrompts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	requireDangerousNonHardBlock(t, dir, "chmod 777 /etc/passwd")

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	env.UnsafeShellMode = true

	if _, err := h.Execute(ctx, env, map[string]any{"command": "chmod 777 /etc/passwd"}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := len(am.calls); got != 1 {
		t.Fatalf("DANGEROUS command must still prompt under --unsafe-shell, got %d calls", got)
	}
}

// TestShellHandler_UnsafeShell_HardBlockStillBlocked verifies that a
// hard-block command is still denied under --unsafe-shell, with no approval
// request (the handler's IsHardBlock early-return precedes the gate).
func TestShellHandler_UnsafeShell_HardBlockStillBlocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	env.UnsafeShellMode = true

	res, err := h.Execute(ctx, env, map[string]any{"command": "mkfs.ext4 /dev/sda1"})
	if err == nil {
		t.Fatal("hard block should return an error under --unsafe-shell")
	}
	if !res.IsError {
		t.Fatal("hard block should return IsError under --unsafe-shell")
	}
	if got := len(am.calls); got != 0 {
		t.Fatalf("hard block early-returns before any approval request, got %d calls", got)
	}
}

// requireCautionTier skips the calling test if cmd does not classify as a
// CAUTION-tier shell prompt — the precondition these tests are built on.
// Mirrors the classifier-contract guard convention in
// pkg/agent/unsafe_shell_integration_test.go.
func requireCautionTier(t *testing.T, dir, cmd string) {
	t.Helper()
	sec := ClassifyToolCallWithWorkspace("shell_command", map[string]any{"command": cmd}, dir)
	if sec.Risk != SecurityCaution || sec.IsHardBlock || !sec.ShouldPrompt {
		t.Skipf("classifier returned risk=%s hardBlock=%v prompt=%v for %q — skipping (classifier behavior changed)",
			sec.Risk, sec.IsHardBlock, sec.ShouldPrompt, cmd)
	}
}

// requireDangerousNonHardBlock skips the calling test if cmd does not
// classify as DANGEROUS but not hard-blocked — the boundary the unsafe-shell
// bypass must not cross.
func requireDangerousNonHardBlock(t *testing.T, dir, cmd string) {
	t.Helper()
	sec := ClassifyToolCallWithWorkspace("shell_command", map[string]any{"command": cmd}, dir)
	if sec.Risk != SecurityDangerous || sec.IsHardBlock {
		t.Skipf("classifier returned risk=%s hardBlock=%v for %q — skipping (classifier behavior changed)",
			sec.Risk, sec.IsHardBlock, cmd)
	}
}
