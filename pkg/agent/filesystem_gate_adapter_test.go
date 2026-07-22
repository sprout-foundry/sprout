package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestFilesystemGateAdapter_Approve_Deny_SessionAllowlist exercises the
// adapter that bridges tool handlers to handleFileSecurityError. It
// covers the four canonical decisions the dialog can return:
//
//   - ApproveOnce: bypass ctx is set, op succeeds.
//   - AllowFolderSession: folder is added to the agent allowlist.
//   - Deny: original error propagates unchanged.
//   - Elevate: agent's risk profile becomes permissive.
//
// These cover the four callsites from a single place, so the adapter
// is verified against the real handleFileSecurityError rather than a
// mock. The pre-existing approval_allowlist_test.go covers the
// underlying handleFileSecurityError flow; this test asserts the
// adapter's contract (signature + behavior) and the integration with
// the agent's existing session-scoped state.
func TestFilesystemGateAdapter_Approve_Deny_SessionAllowlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	t.Run("approve-once wraps ctx and bypasses the gate", func(t *testing.T) {
		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		adapter := newFilesystemGateAdapter(a)
		if adapter == nil {
			t.Fatal("adapter should not be nil for a real agent")
		}

		// External path under $HOME (matches the Tier B convention
		// used elsewhere in approval_allowlist_test.go: t.TempDir()
		// resolves under /var/folders on macOS which is Sensitive).
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}
		dir, err := os.MkdirTemp(home, "sprout-fs-gate-test-")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		// Force the agent's approval manager to receive a synthetic
		// ApproveOnce decision without standing up a real event-bus
		// dialog. The path under $HOME is External tier; the legacy
		// test agent has no active WebUI client, so handleFileSecurityError
		// will look for the CLI picker and find none. We need the
		// decision surface explicitly — easiest is to inject a
		// permissive resolution through a known channel.
		//
		// Approach: pre-allowlist the folder, which short-circuits
		// handleFileSecurityError before any prompt fires. This
		// exercises the adapter's path through handleFileSecurityError
		// without requiring stdin interaction.
		a.AddSessionAllowedFolder(dir)

		ctx := context.Background()
		newCtx, approved := adapter.RequestPathApproval(ctx, "write_file", filepath.Join(dir, "f.txt"), "", filesystem.ErrWriteOutsideWorkingDirectory)
		if !approved {
			t.Fatal("external path under allowlisted folder should be approved")
		}
		if !filesystem.SecurityBypassEnabled(newCtx) {
			t.Error("returned ctx should carry the security bypass token")
		}
	})

	t.Run("deny preserves the original error", func(t *testing.T) {
		// Drives handleFileSecurityError with no allowlist, no
		// elevation, no active approval channel (test agent has
		// SkipPrompt=true, no WebUI client). handleFileSecurityError
		// returns (ctx, false) when it cannot prompt — the model
		// sees the original error verbatim.
		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		adapter := newFilesystemGateAdapter(a)

		// Use a path outside /tmp — M1's universal /tmp allow would
		// approve /tmp/... paths before reaching the prompt flow.
		denyDir := NonTmpTempDir(t)
		denyFile := filepath.Join(denyDir, "somewhere-else")
		ctx, approved := adapter.RequestPathApproval(context.Background(), "read_file", denyFile, "", filesystem.ErrOutsideWorkingDirectory)
		if approved {
			t.Error("non-interactive agent without an approval channel must deny")
		}
		if ctx != context.Background() {
			t.Errorf("denial should return the original ctx unchanged; got %v", ctx)
		}
	})

	t.Run("elevation auto-approves external paths", func(t *testing.T) {
		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		a.ElevateSessionToPermissive()
		adapter := newFilesystemGateAdapter(a)

		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}
		dir, err := os.MkdirTemp(home, "sprout-fs-gate-elevated-")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", filepath.Join(dir, "f.txt"), "", filesystem.ErrWriteOutsideWorkingDirectory)
		if !approved {
			t.Error("elevated session should auto-approve external-tier paths")
		}
		if !filesystem.SecurityBypassEnabled(ctx) {
			t.Error("elevated approval should set the security bypass token")
		}
	})

	t.Run("elevation still prompts on Sensitive tier", func(t *testing.T) {
		a := newIsolatedTestAgent(t)
		defer a.Shutdown()
		a.ElevateSessionToPermissive()
		adapter := newFilesystemGateAdapter(a)

		// /etc is Sensitive; elevation does not bypass that.
		ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", "/etc/passwd", "", filesystem.ErrWriteOutsideWorkingDirectory)
		if approved {
			t.Error("Sensitive-tier path must NOT auto-approve under elevation")
		}
		if filesystem.SecurityBypassEnabled(ctx) {
			t.Error("Sensitive denial must NOT carry the bypass token")
		}
	})

	t.Run("nil adapter returns (ctx, false) so the original error propagates", func(t *testing.T) {
		// Models the env.FilesystemGate = nil case (subagents,
		// internal callers). The handler's withFilesystemApproval
		// helper has its own nil-guard for this — but the adapter
		// must be defensible too so a misconfigured env doesn't
		// silently widen file access.
		var adapter *filesystemGateAdapter
		ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", "/tmp/x", "", filesystem.ErrWriteOutsideWorkingDirectory)
		if approved {
			t.Error("nil adapter must not approve")
		}
		if ctx != context.Background() {
			t.Errorf("nil adapter must return the original ctx")
		}
	})
}

// TestFilesystemGateInterface_ImplementedByAdapter asserts the compile
// time contract between the adapter and the agent_tools side. If this
// test breaks, the interface signature in handler.go and the adapter
// implementation have drifted — most likely someone added a parameter
// to RequestPathApproval but forgot one side.
func TestFilesystemGateInterface_ImplementedByAdapter(t *testing.T) {
	var _ tools.FilesystemGate = (*filesystemGateAdapter)(nil)
	var _ tools.FilesystemGate = newFilesystemGateAdapter(nil)
}

// TestFilesystemGateAdapter_WebUISurface routes through the event-bus
// dialog path inside handleFileSecurityError. It stubs a subscriber
// that publishes a synthetic ApproveOnce decision on the request, so
// the adapter returns (ctx with bypass, true). This is the proof that
// the adapter works end-to-end through the WebUI prompt surface —
// the same path real browser-tab sessions take when an external file
// access is requested.
func TestFilesystemGateAdapter_WebUISurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// The test agent starts without an event bus or approval manager;
	// wire them up explicitly so handleFileSecurityError's WebUI
	// branch is reachable. Mirrors the setup in approval_broker_test.go.
	eventBus := events.NewEventBus()
	a.SetEventBus(eventBus)
	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(5 * time.Second)
	a.security.SetApprovalMgr(mgr)

	// Subscribe to the approval request event and reply on the bus
	// so the approval manager's blocking call returns. The
	// SecurityApprovalRequestEvent payload carries the request ID;
	// the manager stores the pending channel keyed by it, so we
	// resolve via RespondToApprovalDecision rather than re-publishing.
	sub := eventBus.Subscribe(a.GetEventClientID())
	replied := make(chan struct{})
	go func() {
		defer close(replied)
		for ev := range sub {
			if ev.Type != events.EventTypeSecurityApprovalRequest {
				continue
			}
			payload, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			requestID, _ := payload["request_id"].(string)
			if requestID == "" {
				continue
			}
			// Retry briefly until the pending entry is registered.
			for !mgr.RespondToApprovalDecision(requestID, security.ApprovalApproveOnce) {
				time.Sleep(time.Millisecond)
			}
			return
		}
	}()

	a.SetHasActiveWebUIClients(func() bool { return true })

	// External path under $HOME → External tier (Tier B). Drives
	// handleFileSecurityError's WebUI branch through the manager.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-fs-gate-webui-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	adapter := newFilesystemGateAdapter(a)
	ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", filepath.Join(dir, "f.txt"), "", filesystem.ErrWriteOutsideWorkingDirectory)

	if !approved {
		t.Error("WebUI ApproveOnce decision should approve")
	}
	if !filesystem.SecurityBypassEnabled(ctx) {
		t.Error("approved WebUI decision should set the bypass token")
	}

	// Verify the subscriber actually saw the request before
	// returning — guards against a false-positive where the WebUI
	// branch was skipped (e.g., due to a regression in the
	// branch-reachability predicate) and the test happened to pass
	// because some other path approved it.
	select {
	case <-replied:
	case <-time.After(5 * time.Second):
		t.Fatal("WebUI approval request event was never observed")
	}
}

// TestFilesystemGateAdapter_PassesResolvedPathToWebUIDialog verifies
// that when the user-supplied path diverges from the canonical
// target (symlink case), the resolved path reaches the WebUI dialog
// as both a formatted "target" string AND a structured
// "resolved_path" extra. Without this, a workspace symlink to
// /etc/passwd could be approved under a benign-looking display
// string and silently widen file access.
func TestFilesystemGateAdapter_PassesResolvedPathToWebUIDialog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	eventBus := events.NewEventBus()
	a.SetEventBus(eventBus)
	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(5 * time.Second)
	a.security.SetApprovalMgr(mgr)

	// Subscribe and capture the request payload to inspect extras.
	// Extras are flattened into the event payload
	// (SecurityApprovalRequestEvent merges extras into the top-level
	// map), so we read them directly from the payload.
	sub := eventBus.Subscribe(a.GetEventClientID())
	captured := make(chan map[string]interface{}, 1)
	go func() {
		for ev := range sub {
			if ev.Type != events.EventTypeSecurityApprovalRequest {
				continue
			}
			payload, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			select {
			case captured <- payload:
			default:
			}
			requestID, _ := payload["request_id"].(string)
			if requestID != "" {
				for !mgr.RespondToApprovalDecision(requestID, security.ApprovalDeny) {
					time.Sleep(time.Millisecond)
				}
			}
			return
		}
	}()
	a.SetHasActiveWebUIClients(func() bool { return true })

	// Real symlink: $TMP/<link> → <dir>/real-target. Pass the link
	// as userPath and the resolved target as resolvedPath — the
	// adapter must display BOTH so the user can verify the
	// destination.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-fs-gate-symlink-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	realTarget := filepath.Join(dir, "real")
	if err := os.WriteFile(realTarget, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink(realTarget, linkPath); err != nil {
		t.Fatal(err)
	}

	adapter := newFilesystemGateAdapter(a)
	// resolved path is the canonical form of realTarget; use
	// filepath.EvalSymlinks to canonicalize on macOS (/var →
	// /private/var) the same way the production helper does.
	resolved, err := filepath.EvalSymlinks(realTarget)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = adapter.RequestPathApproval(context.Background(), "write_file", linkPath, resolved, filesystem.ErrWriteOutsideWorkingDirectory)

	select {
	case payload := <-captured:
		target, _ := payload["target"].(string)
		// `target` must include both the user-supplied path and the resolved target.
		if !strings.Contains(target, linkPath) {
			t.Errorf("payload[target] should contain user-supplied path %q, got %q", linkPath, target)
		}
		if !strings.Contains(target, resolved) {
			t.Errorf("payload[target] should contain resolved path %q, got %q", resolved, target)
		}
		// `resolved_path` extra is the structured channel for clients
		// that want to render the canonical target distinctly.
		if got, _ := payload["resolved_path"].(string); got != resolved {
			t.Errorf("payload[resolved_path] = %q, want %q", got, resolved)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WebUI approval request event was never observed")
	}
}