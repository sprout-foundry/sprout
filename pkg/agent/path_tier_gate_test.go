package agent

import (
	"context"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestApplyFilesystemDecision_Deny confirms a denial returns the
// untouched ctx and ok=false so the caller surfaces a security error.
func TestApplyFilesystemDecision_Deny(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	ctx := context.Background()

	gotCtx, ok := applyFilesystemDecision(ctx, a, security.ApprovalDeny, "/tmp/x.txt", "/tmp", PathTierExternal)
	if ok {
		t.Error("Deny should return ok=false")
	}
	if gotCtx != ctx {
		t.Error("Deny should not mutate ctx")
	}
	if a.IsFolderSessionAllowed("/tmp/x.txt") {
		t.Error("Deny must not add the folder to the allowlist")
	}
}

// TestApplyFilesystemDecision_ApproveOnce confirms a one-shot approval
// returns a bypass ctx but doesn't touch the allowlist — a second
// access to a sibling path must still prompt.
func TestApplyFilesystemDecision_ApproveOnce(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	ctx := context.Background()

	_, ok := applyFilesystemDecision(ctx, a, security.ApprovalApproveOnce, "/tmp/audit/foo.txt", "/tmp/audit", PathTierExternal)
	if !ok {
		t.Fatal("ApproveOnce should return ok=true")
	}
	if a.IsFolderSessionAllowed("/tmp/audit/bar.txt") {
		t.Error("ApproveOnce must NOT add the folder to the allowlist (one-shot)")
	}
}

// TestApplyFilesystemDecision_AllowFolderSession confirms the folder
// IS added to the allowlist and subsequent files under it are
// auto-approved.
func TestApplyFilesystemDecision_AllowFolderSession(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	ctx := context.Background()

	_, ok := applyFilesystemDecision(ctx, a, security.ApprovalAllowFolderSession, "/tmp/proj/a.txt", "/tmp/proj", PathTierExternal)
	if !ok {
		t.Fatal("AllowFolderSession should return ok=true")
	}
	if !a.IsFolderSessionAllowed("/tmp/proj/b.txt") {
		t.Error("after AllowFolderSession, sibling files should auto-approve")
	}
	if !a.IsFolderSessionAllowed("/tmp/proj/sub/deep.txt") {
		t.Error("after AllowFolderSession, deeper files should auto-approve")
	}
	if a.IsFolderSessionAllowed("/tmp/other/c.txt") {
		t.Error("AllowFolderSession scope leaked beyond the approved folder")
	}
}

// TestApplyFilesystemDecision_SensitiveDemotesFolderApproval is the
// defense-in-depth check: if the server somehow receives an
// AllowFolderSession decision for a Sensitive path (broken client,
// API misuse), it must NOT add the folder to the allowlist. It still
// approves THIS invocation, so the in-flight tool call doesn't break.
func TestApplyFilesystemDecision_SensitiveDemotesFolderApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix paths only")
	}
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	ctx := context.Background()

	_, ok := applyFilesystemDecision(ctx, a, security.ApprovalAllowFolderSession, "/etc/secret", "/etc", PathTierSensitive)
	if !ok {
		t.Fatal("Sensitive AllowFolderSession should still approve this invocation")
	}
	if a.IsFolderSessionAllowed("/etc/other") {
		t.Error("Sensitive tier must never add to the allowlist, even if client says AllowFolderSession")
	}
}

// TestFilesystemDecisionFromCLIChoice walks the CLI-to-Decision
// mapping. The default branch must collapse to Deny for safety.
func TestFilesystemDecisionFromCLIChoice(t *testing.T) {
	tests := []struct {
		in   string
		want security.ApprovalDecision
	}{}
	_ = tests // unused — explicit table below covers the enum.

	if got := filesystemDecisionFromCLIChoice(0); got != security.ApprovalDeny {
		t.Errorf("ApprovalChoiceDeny (=0) → %v, want Deny", got)
	}
	// ApprovalChoiceApproveOnce → ApprovalApproveOnce
	// ApprovalChoiceAllowFolderSession → ApprovalAllowFolderSession
	// The constants are in pkg/utils; just check the underlying ints.
	if got := filesystemDecisionFromCLIChoice(1); got != security.ApprovalApproveOnce {
		t.Errorf("ApprovalChoiceApproveOnce (=1) → %v, want ApproveOnce", got)
	}
	if got := filesystemDecisionFromCLIChoice(4); got != security.ApprovalAllowFolderSession {
		t.Errorf("ApprovalChoiceAllowFolderSession (=4) → %v, want AllowFolderSession", got)
	}
	// ApproveAlways and Elevate are shell-only; the filesystem mapper
	// must drop them to Deny (they don't make sense for fs gates).
	if got := filesystemDecisionFromCLIChoice(2); got != security.ApprovalDeny {
		t.Errorf("shell-only ApproveAlways → %v on fs path, want Deny", got)
	}
	if got := filesystemDecisionFromCLIChoice(3); got != security.ApprovalDeny {
		t.Errorf("shell-only Elevate → %v on fs path, want Deny", got)
	}
}
