package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// wireAgentToolFuncs sets the package-level function pointers in
// pkg/agent_tools that delegate agent-dependent tools (subagent spawn,
// clarification, PR creation, automate) back to this agent instance.
//
// These function pointers break a circular dependency: the tool handlers
// in pkg/agent_tools cannot import pkg/agent, so we capture the *Agent
// in a closure and install it as a function pointer.
//
// Called from initAgentFromResolvedProvider once per agent construction.
// In daemon mode with multiple agents, the function pointers point to
// the most recently constructed agent — concurrent agent sessions must
// not invoke these tools simultaneously.
func wireAgentToolFuncs(agent *Agent, isProduction bool) {
	if agent == nil {
		return
	}

	tools.ToolFuncMu.Lock()
	defer tools.ToolFuncMu.Unlock()

	tools.RunSubagentFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRunSubagent(ctx, agent, args)
	}
	tools.RunParallelSubagentsFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRunParallelSubagents(ctx, agent, args)
	}
	tools.RequestClarificationFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRequestClarification(ctx, agent, args)
	}
	tools.RespondClarificationFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRespondClarification(ctx, agent, args)
	}

	// Change-tracking tools — these delegate to the agent's ChangeTracker.
	tools.ListChangesFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleListChanges(ctx, agent, args)
	}
	tools.RecoverFileFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRecoverFile(ctx, agent, args)
	}
	tools.RevertMyChangesFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRevertMyChanges(ctx, agent, args)
	}
	tools.MCPRefreshFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleMCPRefresh(ctx, agent, args)
	}

	// Host-only tools (PR creation, automate workflows) require live
	// infrastructure (git, filesystem, subprocess spawning) and are gated
	// by both isProduction and the build target. The build-tagged
	// wireHostOnlyToolFuncs function wires the real handlers on native
	// builds (when isProduction) and clear-error stubs on WASM. See
	// agent_tool_wiring_nonjs.go and agent_tool_wiring_js.go.
	wireHostOnlyToolFuncs(agent, isProduction)
}
