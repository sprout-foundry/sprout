package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// wireAgentToolFuncs builds the per-agent tool dispatch set for this agent
// and installs the package-level function pointers in pkg/agent_tools that
// delegate agent-dependent tools (subagent spawn, clarification, PR
// creation, automate) back to an agent instance.
//
// The closures break a circular dependency: the tool handlers in
// pkg/agent_tools cannot import pkg/agent, so we capture the *Agent in a
// closure and carry it in the agent's ToolFuncSet. Handlers resolve the set
// through ToolEnv.ResolveToolFuncs, so in a daemon with multiple live agents
// each tool call dispatches to the agent whose env it runs under.
//
// The package-level pointers are still written for the legacy fallback path
// (callers that build ToolEnv directly). They point at the most recently
// constructed agent — bounded to one agent, acceptable.
//
// Called from initAgentFromResolvedProvider once per agent construction.
func wireAgentToolFuncs(agent *Agent, isProduction bool) {
	if agent == nil {
		return
	}

	set := &tools.ToolFuncSet{
		RunSubagent: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRunSubagent(ctx, agent, args)
		},
		RunParallelSubagents: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRunParallelSubagents(ctx, agent, args)
		},
		RequestClarification: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRequestClarification(ctx, agent, args)
		},
		RespondClarification: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRespondClarification(ctx, agent, args)
		},
		ListChanges: func(ctx context.Context, args map[string]any) (string, error) {
			return handleListChanges(ctx, agent, args)
		},
		RecoverFile: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRecoverFile(ctx, agent, args)
		},
		RevertMyChanges: func(ctx context.Context, args map[string]any) (string, error) {
			return handleRevertMyChanges(ctx, agent, args)
		},
		MCPRefresh: func(ctx context.Context, args map[string]any) (string, error) {
			return handleMCPRefresh(ctx, agent, args)
		},
	}

	tools.ToolFuncMu.Lock()
	defer tools.ToolFuncMu.Unlock()

	// Host-only tools (PR creation, automate workflows) require live
	// infrastructure (git, filesystem, subprocess spawning) and are gated
	// by both isProduction and the build target. wireHostOnlyToolFuncs
	// populates set.RunAutomate/CreatePullRequest and their package vars —
	// real handlers on native builds (when isProduction), clear-error stubs
	// on WASM. See agent_tool_wiring_nonjs.go and agent_tool_wiring_js.go.
	wireHostOnlyToolFuncs(agent, isProduction, set)

	agent.toolFuncs = set

	tools.RunSubagentFunc = set.RunSubagent
	tools.RunParallelSubagentsFunc = set.RunParallelSubagents
	tools.RequestClarificationFunc = set.RequestClarification
	tools.RespondClarificationFunc = set.RespondClarification
	tools.ListChangesFunc = set.ListChanges
	tools.RecoverFileFunc = set.RecoverFile
	tools.RevertMyChangesFunc = set.RevertMyChanges
	tools.MCPRefreshFunc = set.MCPRefresh
}
