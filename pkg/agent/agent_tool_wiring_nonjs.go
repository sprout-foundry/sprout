//go:build !js

package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// wireHostOnlyToolFuncs wires the function pointers for tools that require
// host-only infrastructure (git, filesystem, subprocess spawning): PR
// creation and automate workflows. These are gated by isProduction because
// they require a live agent with full desktop/daemon infrastructure.
//
// Populates the agent's ToolFuncSet (the per-agent dispatch path) AND the
// package-level vars (the legacy fallback). Called under ToolFuncMu by
// wireAgentToolFuncs, so the package-var writes are guarded.
//
// This is the native (non-WASM) implementation. The WASM counterpart in
// agent_tool_wiring_js.go wires clear-error stubs instead, since the host
// code (process spawning, exec) cannot run in the browser. See AUDIT-C2.
func wireHostOnlyToolFuncs(agent *Agent, isProduction bool, set *tools.ToolFuncSet) {
	if agent == nil || !isProduction {
		return
	}
	set.RunAutomate = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRunAutomate(ctx, agent, args)
	}
	set.CreatePullRequest = func(ctx context.Context, args map[string]any) (string, error) {
		return handleCreatePullRequest(ctx, agent, args)
	}
	tools.RunAutomateFunc = set.RunAutomate
	tools.CreatePullRequestFunc = set.CreatePullRequest
}
