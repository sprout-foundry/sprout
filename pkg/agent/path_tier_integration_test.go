package agent

import (
	"context"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestPathTierGate_EndToEnd walks the three-tier flow once: a Tier B
// path prompts and persists when the user picks "allow folder this
// session"; a second access under the same folder auto-passes; a
// Tier C path always re-prompts no matter how many times the user
// approves. Drives applyFilesystemDecision directly so we don't need
// to spin up a fake event-bus.
func TestPathTierGate_EndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Tier B: user picks "allow folder this session" for /tmp/proj/a.go.
	// The folder /tmp/proj is added to the agent's allowlist.
	bPath := "/tmp/proj/a.go"
	bFolder := "/tmp/proj"
	_, ok := applyFilesystemDecision(context.Background(), a, security.ApprovalAllowFolderSession, bPath, bFolder, PathTierExternal)
	if !ok {
		t.Fatal("Tier B first access (AllowFolderSession) should approve")
	}
	if !a.IsFolderSessionAllowed(bPath) {
		t.Error("first Tier B access should add folder to allowlist")
	}

	// Tier B second access — a sibling file under the same folder.
	// IsFolderSessionAllowed reports true, so handleFileSecurityError
	// short-circuits before any prompt fires. We exercise the check
	// directly here.
	siblingPath := "/tmp/proj/sub/b.go"
	if !a.IsFolderSessionAllowed(siblingPath) {
		t.Error("sibling file under allowlisted folder should auto-pass without prompt")
	}

	// Tier B third access — DIFFERENT folder. Allowlist scope is
	// per-folder, so this must NOT auto-pass.
	differentPath := "/tmp/elsewhere/c.go"
	if a.IsFolderSessionAllowed(differentPath) {
		t.Error("path in a different folder must not be auto-approved")
	}

	// Tier C: even if the user picks AllowFolderSession (broken
	// client), the folder MUST NOT be added. Allow-once semantics
	// still let this invocation through, but the allowlist stays clean.
	sensitivePath := "/etc/secret"
	sensitiveFolder := "/etc"
	_, ok = applyFilesystemDecision(context.Background(), a, security.ApprovalAllowFolderSession, sensitivePath, sensitiveFolder, PathTierSensitive)
	if !ok {
		t.Fatal("Tier C AllowFolderSession should still approve this invocation")
	}
	if a.IsFolderSessionAllowed("/etc/another-file") {
		t.Error("Tier C must never persist to allowlist, even when client sends AllowFolderSession")
	}

	// Tier C second access — must still NOT be on the allowlist.
	if a.IsFolderSessionAllowed(sensitivePath) {
		t.Error("Tier C path must not be on allowlist after a prior approval")
	}
}

// TestPathTierGate_DenyDoesNotPersist confirms that picking "Deny"
// never adds anything to the allowlist — the safety bug we fixed was
// "any approval = all paths allowed"; the inverse "any denial =
// nothing" needs to hold too.
func TestPathTierGate_DenyDoesNotPersist(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	_, ok := applyFilesystemDecision(context.Background(), a, security.ApprovalDeny, "/tmp/x/y.txt", "/tmp/x", PathTierExternal)
	if ok {
		t.Error("Deny must not approve")
	}
	if a.IsFolderSessionAllowed("/tmp/x/y.txt") {
		t.Error("Deny must leave allowlist untouched")
	}
	if len(a.SnapshotSessionAllowedFolders()) != 0 {
		t.Errorf("after Deny: expected empty allowlist, got %v", a.SnapshotSessionAllowedFolders())
	}
}
