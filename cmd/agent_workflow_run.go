//go:build !js

package cmd

import (
	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/workflow"
)

// markWorkflowRun flags the agent as executing a workflow/automate run so the
// approval surfaces treat it as non-interactive even when launched from a
// terminal. A run driven by a workflow config has nobody answering a Caution
// prompt, so it must not wait on one. A nil config or agent is a no-op.
func markWorkflowRun(chatAgent *agent.Agent, cfg *workflow.AgentWorkflowConfig) {
	if chatAgent == nil || cfg == nil {
		return
	}
	chatAgent.SetWorkflowRun(true)
}
