package agent

// Tests for the SP-170 MCP tool authorization gate.

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// authzTestAgent returns a bare agent (no interactive surface) wired to a
// recording MCP manager and an isolated config manager, so the allowlist can
// be persisted and observed.
func authzTestAgent(t *testing.T, entries ...string) (*Agent, *callRecordingManager) {
	t.Helper()
	mgr := &callRecordingManager{result: &mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "ok"}},
	}}
	a := callTestAgent(mgr)

	cfgMgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if len(entries) > 0 {
		if err := cfgMgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ApprovedMCPTools = append(cfg.ApprovedMCPTools, entries...)
			return nil
		}); err != nil {
			t.Fatalf("seed allowlist: %v", err)
		}
	}
	a.configManager = cfgMgr
	return a, mgr
}

// TestMCPAuthz_AllowlistedRunsWithoutPrompting covers the first branch of the
// decision order: an allowlisted tool runs, and no surface is consulted.
func TestMCPAuthz_AllowlistedRunsWithoutPrompting(t *testing.T) {
	a, mgr := authzTestAgent(t, "github__delete_repo")

	out, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	})
	if err != nil {
		t.Fatalf("allowlisted tool should run, got error: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected tool output, got %q", out)
	}
	if mgr.callCount != 1 {
		t.Errorf("expected exactly one CallTool, got %d", mgr.callCount)
	}
}

// TestMCPAuthz_ServerEntryCoversItsTools covers the `<server>` entry form.
func TestMCPAuthz_ServerEntryCoversItsTools(t *testing.T) {
	a, mgr := authzTestAgent(t, "github")

	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	}); err != nil {
		t.Fatalf("server-level entry should cover its tools, got error: %v", err)
	}
	if mgr.callCount != 1 {
		t.Errorf("expected exactly one CallTool, got %d", mgr.callCount)
	}
}

// TestMCPAuthz_ToolEntryDoesNotCoverSiblings covers the per-tool entry form's
// narrowness: allowlisting one tool must not authorize another.
func TestMCPAuthz_ToolEntryDoesNotCoverSiblings(t *testing.T) {
	a, mgr := authzTestAgent(t, "github__read")

	_, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	})
	if err == nil {
		t.Fatal("a tool entry must not cover its siblings, got nil error")
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}
}

// TestMCPAuthz_NoSurfaceDeniesAndNamesTheTool covers the third branch: with// no interactive surface the call is denied, and the error is actionable —
// it names the tool, says nobody could be asked, and gives the exact
// allowlist entry that would permit it.
func TestMCPAuthz_NoSurfaceDeniesAndNamesTheTool(t *testing.T) {
	a, mgr := authzTestAgent(t)

	_, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	})
	if err == nil {
		t.Fatal("expected denial with no interactive surface, got nil error")
	}
	msg := err.Error()
	for _, want := range []string{"github", "delete_repo", "github__delete_repo", "approved_mcp_tools"} {
		if !strings.Contains(msg, want) {
			t.Errorf("denial message must mention %q, got: %s", want, msg)
		}
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}
}

// TestMCPAuthz_AcmeDoesNotMatchAcmecorp pins the whole-segment rule through
// the gate (not just the matcher).
func TestMCPAuthz_AcmeDoesNotMatchAcmecorp(t *testing.T) {
	a, mgr := authzTestAgent(t, "acme")

	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "acmecorp", "tool": "read",
	}); err == nil {
		t.Fatal("`acme` must not authorize the `acmecorp` server, got nil error")
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}
}

// TestMCPAuthz_DiscoveryActionsNotGated covers the exemption: list, refresh
// and status run no server-side tool and must never be gated.
func TestMCPAuthz_DiscoveryActionsNotGated(t *testing.T) {
	a, _ := authzTestAgent(t)

	for _, action := range []string{"list", "refresh", "status"} {
		if _, err := a.handleMCPToolsCommand(map[string]interface{}{"action": action}); err != nil {
			t.Errorf("discovery action %q must not be gated, got error: %v", action, err)
		}
	}
}

// TestMCPAuthz_DirectCallIsGated covers the first entry point: a direct
// mcp_<server>_<tool> call reaches the same gate as the meta-command.
func TestMCPAuthz_DirectCallIsGated(t *testing.T) {
	a, mgr := authzTestAgent(t)

	if _, err := a.executeMCPTool("mcp_github_delete_repo", nil); err == nil {
		t.Fatal("a non-allowlisted direct call must be denied, got nil error")
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}

	// And the allowlisted form runs.
	a2, mgr2 := authzTestAgent(t, "github__delete_repo")
	if _, err := a2.executeMCPTool("mcp_github_delete_repo", nil); err != nil {
		t.Fatalf("allowlisted direct call should run, got error: %v", err)
	}
	if mgr2.callCount != 1 {
		t.Errorf("expected exactly one CallTool, got %d", mgr2.callCount)
	}
}

// TestMCPAuthz_AlwaysAllowToolPersistsAndStopsPrompting covers the "always
// allow this tool" answer: the entry is persisted and the next call runs
// without consulting a surface.
func TestMCPAuthz_AlwaysAllowToolPersistsAndStopsPrompting(t *testing.T) {
	a, mgr := authzTestAgent(t)

	if err := a.PersistMCPToolAllowlist("github__delete_repo"); err != nil {
		t.Fatalf("PersistMCPToolAllowlist: %v", err)
	}
	// The persisted entry is visible through the config the gate reads.
	if !a.IsMCPToolAllowlisted("github", "delete_repo") {
		t.Fatal("persisted entry not reflected in the allowlist lookup")
	}

	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	}); err != nil {
		t.Fatalf("second call should run without prompting, got error: %v", err)
	}
	if mgr.callCount != 1 {
		t.Errorf("expected exactly one CallTool, got %d", mgr.callCount)
	}
}

// TestMCPAuthz_PersistIsIdempotent pins the no-duplicate contract shared with
// the shell allowlist.
func TestMCPAuthz_PersistIsIdempotent(t *testing.T) {
	a, _ := authzTestAgent(t)

	for i := 0; i < 2; i++ {
		if err := a.PersistMCPToolAllowlist("github"); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}
	count := 0
	for _, e := range a.GetConfig().ApprovedMCPTools {
		if e == "github" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry, got %d", count)
	}
}

// TestMCPAuthz_PersistRejectsEmptyAndMissingManager covers the guard rails.
func TestMCPAuthz_PersistRejectsEmptyAndMissingManager(t *testing.T) {
	a, _ := authzTestAgent(t)
	if err := a.PersistMCPToolAllowlist(""); err == nil {
		t.Error("empty entry must be rejected")
	}

	bare := &Agent{}
	if err := bare.PersistMCPToolAllowlist("github"); err == nil {
		t.Error("missing config manager must be rejected")
	}
}

// TestMCPAuthz_NilAgentAuthorizesNothing pins the fail-closed nil guard.
func TestMCPAuthz_NilAgentAuthorizesNothing(t *testing.T) {
	var a *Agent
	if a.IsMCPToolAllowlisted("github", "read") {
		t.Error("nil agent must authorize nothing")
	}
	if err := a.authorizeMCPTool("github", "read"); err == nil {
		t.Error("nil agent must deny")
	}
}

// TestMCPAuthz_ServerWideAllowlistEntry covers the entry the "always allow
// this server" answer writes.
func TestMCPAuthz_ServerWideAllowlistEntry(t *testing.T) {
	a, _ := authzTestAgent(t)
	if err := a.PersistMCPToolAllowlist("github"); err != nil {
		t.Fatalf("PersistMCPToolAllowlist: %v", err)
	}
	for _, tool := range []string{"read", "delete_repo", "create_issue"} {
		if !a.IsMCPToolAllowlisted("github", tool) {
			t.Errorf("server-wide entry should cover %q", tool)
		}
	}
	if a.IsMCPToolAllowlisted("gitlab", "read") {
		t.Error("server-wide entry must not cover another server")
	}
}

// TestMCPAuthz_GitHubURLRouterIsGated covers the third route to a server-side
// MCP call: fetch_url on a GitHub URL reaches tryRouteGitHubToMCP, and the
// model chooses the URL. Without the gate this path would run an ungated
// github/* tool call, and a denial must surface as a denial rather than
// silently falling back to a plain fetch (which would defeat the gate).
func TestMCPAuthz_GitHubURLRouterIsGated(t *testing.T) {
	mgr := newGitHubRouterMCPManager().
		withGitHubServer(true).
		withCallResult(&mcp.MCPToolCallResult{
			Content: []mcp.MCPContent{{Type: "text", Text: "issue body"}},
		})
	// An agent with no allowlist entry and no interactive surface.
	a := testAgent(t, mgr)
	if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ApprovedMCPTools = nil
		return nil
	}); err != nil {
		t.Fatalf("clear allowlist: %v", err)
	}

	_, handled, err := a.tryRouteGitHubToMCP(context.Background(),
		"https://github.com/owner/repo/issues/42")
	if err == nil {
		t.Fatal("expected the router path to be gated, got nil error")
	}
	if !strings.Contains(err.Error(), "github__get_issue") {
		t.Errorf("denial must name the allowlist entry, got: %v", err)
	}
	if handled {
		t.Error("a denied call must not report as handled")
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}
}

// TestMCPAuthz_WorkflowRunHasNoSurface pins that a workflow run never offers
// the terminal surface: nobody is guaranteed to be at the terminal, so the
// run denies instead of blocking on a picker.
func TestMCPAuthz_WorkflowRunHasNoSurface(t *testing.T) {
	a, mgr := authzTestAgent(t)
	a.workflowRun.Store(true)

	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "github", "tool": "delete_repo",
	}); err == nil {
		t.Fatal("a workflow run must be denied, got nil error")
	}
	if mgr.callCount != 0 {
		t.Errorf("denied call must not reach the server, got %d calls", mgr.callCount)
	}
}

// TestMCPAuthz_DeniedCallDoesNotReachServerForDirectPath is a belt-and-braces
// check that the direct path's denial happens before the manager call, using
// a manager that fails loudly if reached.
func TestMCPAuthz_DeniedCallDoesNotReachServerForDirectPath(t *testing.T) {
	mgr := &callRecordingManager{err: context.DeadlineExceeded}
	a := callTestAgent(mgr)
	cfgMgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	a.configManager = cfgMgr

	_, err := a.executeMCPTool("mcp_github_delete_repo", nil)
	if err == nil {
		t.Fatal("expected denial, got nil error")
	}
	if strings.Contains(err.Error(), "deadline") {
		t.Errorf("denial must come from the gate, not the manager: %v", err)
	}
	if mgr.callCount != 0 {
		t.Errorf("expected no server call, got %d", mgr.callCount)
	}
}
