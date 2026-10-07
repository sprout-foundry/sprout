//go:build !js

package cmd

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/workflow"
)

// TestMarkWorkflowRun pins the CLI wiring for fix (a): a run driven by a
// workflow config is flagged on the agent so every approval surface treats it
// as non-interactive, while a plain run and nil inputs are left untouched.
func TestMarkWorkflowRun(t *testing.T) {
	t.Run("workflow config flags the agent", func(t *testing.T) {
		a := newTestAgentForWorkflowFlag(t)
		markWorkflowRun(a, &workflow.AgentWorkflowConfig{Description: "wf"})
		if !a.IsWorkflowRun() {
			t.Fatal("a workflow-config run must set the workflow-run flag")
		}
		if !a.IsNonInteractive() {
			t.Fatal("a workflow-config run must report non-interactive")
		}
	})

	t.Run("nil config leaves the agent unflagged", func(t *testing.T) {
		a := newTestAgentForWorkflowFlag(t)
		markWorkflowRun(a, nil)
		if a.IsWorkflowRun() {
			t.Fatal("a plain run must not set the workflow-run flag")
		}
	})

	t.Run("nil agent is a no-op", func(t *testing.T) {
		markWorkflowRun(nil, &workflow.AgentWorkflowConfig{Description: "wf"})
	})
}

func newTestAgentForWorkflowFlag(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("agent.NewAgent() error: %v", err)
	}
	t.Cleanup(a.Shutdown)
	return a
}
