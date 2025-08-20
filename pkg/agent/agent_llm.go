package agent

import (
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// CallLLMForAgent handles LLM calls specifically for agent workflows.
// This now uses the unified interactive flow with agent-specific context.
func CallLLMForAgent(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, contextHandler llm.ContextHandler) (string, string, *llm.TokenUsage, error) {
	// Create workflow context for agent workflows
	workflowContext := llm.GetAgentWorkflowContext()
	workflowContext.ContextHandler = contextHandler

	// Create unified interactive config
	unifiedConfig := &llm.UnifiedInteractiveConfig{
		ModelName:       modelName,
		Messages:        messages,
		Filename:        filename,
		WorkflowContext: workflowContext,
		Config:          cfg,
		Timeout:         timeout,
	}

	return llm.CallLLMWithUnifiedInteractive(unifiedConfig)
}
