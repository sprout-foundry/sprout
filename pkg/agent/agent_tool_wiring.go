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

	// Production-only: PR creation and automate workflows require a live
	// agent with full infrastructure (git, filesystem, workspace).
	if isProduction {
		tools.RunAutomateFunc = func(ctx context.Context, args map[string]any) (string, error) {
			return handleRunAutomate(ctx, agent, args)
		}
		tools.CreatePullRequestFunc = func(ctx context.Context, args map[string]any) (string, error) {
			return handleCreatePullRequest(ctx, agent, args)
		}
	}
}
