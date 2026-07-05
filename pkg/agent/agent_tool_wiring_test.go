package agent

import (
	"context"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestWireAgentToolFuncs_AllPointersSet is a regression test for the bug
// where wireAgentToolFuncs wired only the first batch of agent-dependent
// tools (subagent, clarification) but silently missed others added later
// (list_changes, recover_file, revert_my_changes, mcp_refresh). When a
// pointer is nil, the handler returns "agent integration not initialized"
// and the tool is unusable at runtime — with no compile-time signal.
//
// This test enumerates EVERY agent-dependent function pointer in
// pkg/agent_tools so that adding a new pointer without wiring it fails
// the build here, not silently in production.
func TestWireAgentToolFuncs_AllPointersSet(t *testing.T) {
	agent := newTestAgent(t)

	// Reset all pointers to nil so we verify wireAgentToolFuncs sets them,
	// not that a prior test left them populated.
	tools.RunSubagentFunc = nil
	tools.RunParallelSubagentsFunc = nil
	tools.RequestClarificationFunc = nil
	tools.RespondClarificationFunc = nil
	tools.ListChangesFunc = nil
	tools.RecoverFileFunc = nil
	tools.RevertMyChangesFunc = nil
	tools.MCPRefreshFunc = nil
	tools.RunAutomateFunc = nil
	tools.CreatePullRequestFunc = nil

	wireAgentToolFuncs(agent, true)

	checks := []struct {
		name string
		ptr  *func(ctx context.Context, args map[string]any) (string, error)
	}{
		{"RunSubagentFunc", &tools.RunSubagentFunc},
		{"RunParallelSubagentsFunc", &tools.RunParallelSubagentsFunc},
		{"RequestClarificationFunc", &tools.RequestClarificationFunc},
		{"RespondClarificationFunc", &tools.RespondClarificationFunc},
		{"ListChangesFunc", &tools.ListChangesFunc},
		{"RecoverFileFunc", &tools.RecoverFileFunc},
		{"RevertMyChangesFunc", &tools.RevertMyChangesFunc},
		{"MCPRefreshFunc", &tools.MCPRefreshFunc},
		{"RunAutomateFunc", &tools.RunAutomateFunc},
		{"CreatePullRequestFunc", &tools.CreatePullRequestFunc},
	}
	for _, c := range checks {
		if *c.ptr == nil {
			t.Errorf("wireAgentToolFuncs left %s nil — tool will return \"agent integration not initialized\"", c.name)
		}
	}
}

// TestWireAgentToolFuncs_NonProductionSkipsInfraTools verifies the
// isProduction gate: RunAutomate and CreatePullRequest require live git
// and filesystem infrastructure and must stay nil in non-production
// (WASM/SDK) contexts. The change-tracking and clarification tools are
// safe in all contexts and must always be wired.
func TestWireAgentToolFuncs_NonProductionSkipsInfraTools(t *testing.T) {
	agent := newTestAgent(t)

	tools.RunAutomateFunc = nil
	tools.CreatePullRequestFunc = nil
	tools.ListChangesFunc = nil

	wireAgentToolFuncs(agent, false)

	if tools.RunAutomateFunc != nil {
		t.Error("RunAutomateFunc should be nil in non-production mode")
	}
	if tools.CreatePullRequestFunc != nil {
		t.Error("CreatePullRequestFunc should be nil in non-production mode")
	}
	if tools.ListChangesFunc == nil {
		t.Error("ListChangesFunc should be set even in non-production mode")
	}
}

// TestWireAgentToolFuncs_NilAgentIsNoop ensures a nil agent doesn't panic.
func TestWireAgentToolFuncs_NilAgentIsNoop(t *testing.T) {
	// Should not panic.
	wireAgentToolFuncs(nil, true)
}
