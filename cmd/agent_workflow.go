//go:build !js

package cmd

import (
	"github.com/sprout-foundry/sprout/pkg/workflow"
)

// Type aliases so existing cmd/ code can reference workflow types without
// changing every call site. The real definitions live in pkg/workflow.
type (
	AgentWorkflowConfig            = workflow.AgentWorkflowConfig
	AgentWorkflowStep              = workflow.AgentWorkflowStep
	AgentWorkflowInitial           = workflow.AgentWorkflowInitial
	AgentWorkflowRuntime           = workflow.AgentWorkflowRuntime
	AgentWorkflowBudgetConfig      = workflow.AgentWorkflowBudgetConfig
	AgentWorkflowProgressConfig    = workflow.AgentWorkflowProgressConfig
	AgentWorkflowOrchestrationConfig = workflow.AgentWorkflowOrchestrationConfig
	AgentWorkflowLoopConfig        = workflow.AgentWorkflowLoopConfig
	WorkflowExecutionState         = workflow.WorkflowExecutionState
	WorkflowSubagentOverrides      = workflow.WorkflowSubagentOverrides
	WorkflowSubagentOverride       = workflow.WorkflowSubagentOverride
)

// buildWorkflowCLIOverrides constructs a CLIOverrides that wires workflow JSON
// flag values back to the cmd package's real flag variables via closures.
func buildWorkflowCLIOverrides() *workflow.CLIOverrides {
	return &workflow.CLIOverrides{
		SetWebUI:           func(disabled bool) { disableWebUI = disabled },
		SetWebPort:         func(port int) { webPort = port },
		SetDaemon:          func(enabled bool) { daemonMode = enabled },
		SetNoStream:        func(enabled bool) { agentNoStreaming = enabled },
		GetNoStream:        func() bool { return agentNoStreaming },
		BudgetUSD:          agentBudgetUSD,
		BudgetWarn:         agentBudgetWarn,
		HeartbeatSeconds:   agentHeartbeatSeconds,
	}
}
