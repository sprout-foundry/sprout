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
// This is the native (non-WASM) implementation. The WASM counterpart in
// agent_tool_wiring_js.go wires clear-error stubs instead, since the host
// code (process spawning, exec) cannot run in the browser. See AUDIT-C2.
func wireHostOnlyToolFuncs(agent *Agent, isProduction bool) {
	if agent == nil || !isProduction {
		return
	}
	tools.RunAutomateFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleRunAutomate(ctx, agent, args)
	}
	tools.CreatePullRequestFunc = func(ctx context.Context, args map[string]any) (string, error) {
		return handleCreatePullRequest(ctx, agent, args)
	}
}
