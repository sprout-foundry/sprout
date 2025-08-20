package llm

import (
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// WorkflowType defines the type of workflow being executed
type WorkflowType string

const (
	WorkflowTypeCode  WorkflowType = "code"
	WorkflowTypeAgent WorkflowType = "agent"
	// Future workflow types can be added here
)

// WorkflowContext contains workflow-specific information
type WorkflowContext struct {
	Type           WorkflowType
	SystemPrompt   string
	MaxToolCalls   int
	ContextHandler ContextHandler
	State          map[string]interface{} // For maintaining context state
}

// UnifiedInteractiveConfig contains configuration for the unified interactive flow
type UnifiedInteractiveConfig struct {
	ModelName       string
	Messages        []prompts.Message
	Filename        string
	WorkflowContext *WorkflowContext
	Config          *config.Config
	Timeout         time.Duration
}

// CallLLMWithUnifiedInteractive handles all interactive LLM calls with tools.
// This is the single entry point for both code editing and agent workflows.
func CallLLMWithUnifiedInteractive(cfg *UnifiedInteractiveConfig) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.Config.SkipPrompt)
	logger.Log("=== Unified Interactive LLM Debug ===")
	logger.Log(fmt.Sprintf("Model: %s", cfg.ModelName))
	logger.Log(fmt.Sprintf("Workflow Type: %s", cfg.WorkflowContext.Type))
	logger.Log(fmt.Sprintf("Max Tool Calls: %d", cfg.WorkflowContext.MaxToolCalls))
	logger.Log(fmt.Sprintf("Initial messages count: %d", len(cfg.Messages)))

	// Inject workflow-specific system prompt if provided
	if cfg.WorkflowContext.SystemPrompt != "" {
		logger.Log("Injecting workflow-specific system prompt")
		// Add or replace system message with workflow-specific prompt
		cfg.Messages = injectSystemPrompt(cfg.Messages, cfg.WorkflowContext.SystemPrompt)
	}

	// Check if tools are enabled for this workflow
	if cfg.Config.Interactive && cfg.Config.CodeToolsEnabled {
		logger.Log("Using interactive mode with tools")
		return callLLMWithToolsUnified(cfg)
	}

	// Fallback to simple response
	logger.Log("Using simple response mode")
	return callLLMForSimpleResponseUnified(cfg)
}

// injectSystemPrompt injects or replaces the system prompt in the messages
func injectSystemPrompt(messages []prompts.Message, systemPrompt string) []prompts.Message {
	result := make([]prompts.Message, 0, len(messages))

	// Look for existing system message
	systemFound := false
	for _, msg := range messages {
		if msg.Role == "system" {
			// Replace existing system message
			result = append(result, prompts.Message{
				Role:    "system",
				Content: systemPrompt,
			})
			systemFound = true
		} else {
			result = append(result, msg)
		}
	}

	// If no system message found, add one at the beginning
	if !systemFound {
		result = append([]prompts.Message{{
			Role:    "system",
			Content: systemPrompt,
		}}, result...)
	}

	return result
}

// callLLMWithToolsUnified handles the unified interactive flow with tools
func callLLMWithToolsUnified(cfg *UnifiedInteractiveConfig) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.Config.SkipPrompt)
	maxToolCalls := cfg.WorkflowContext.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 8 // Default limit
	}

	currentMessages := make([]prompts.Message, len(cfg.Messages))
	copy(currentMessages, cfg.Messages)

	totalToolCalls := 0
	attempts := 0
	maxRetries := 10

	for attempts < maxRetries {
		attempts++
		logger.Log(fmt.Sprintf("Interactive attempt %d/%d", attempts, maxRetries))

		// Get available tools for this workflow
		availableTools := GetAvailableTools()
		toolNames := make([]string, len(availableTools))
		for i, tool := range availableTools {
			toolNames[i] = tool.Function.Name
		}

		// Make LLM call with tools
		response, tokenUsage, err := GetLLMResponseWithToolsScoped(
			cfg.ModelName,
			currentMessages,
			cfg.Filename,
			cfg.Config,
			cfg.Timeout,
			toolNames,
		)
		if err != nil {
			logger.Log(fmt.Sprintf("LLM call failed: %v", err))
			return "", "", nil, fmt.Errorf("LLM call failed: %w", err)
		}

		// Parse tool calls from response
		toolCalls, err := ParseToolCalls(response)
		if err != nil {
			logger.Log(fmt.Sprintf("Failed to parse tool calls: %v", err))
			return cfg.ModelName, response, tokenUsage, nil
		}

		// If no tool calls, return the response
		if len(toolCalls) == 0 {
			logger.Log("No tool calls - returning response")
			return cfg.ModelName, response, tokenUsage, nil
		}

		// Check tool call limits
		if totalToolCalls+len(toolCalls) > maxToolCalls {
			logger.Log(fmt.Sprintf("Maximum tool calls (%d) would be exceeded, forcing final response", maxToolCalls))
			// Add a system message to force completion
			currentMessages = append(currentMessages, prompts.Message{
				Role:    "system",
				Content: fmt.Sprintf("Maximum tool calls reached (%d). Please provide your final response without using any more tools.", maxToolCalls),
			})

			// Make one final call without tools
			finalResponse, finalTokenUsage, err := GetLLMResponse(cfg.ModelName, currentMessages, cfg.Filename, cfg.Config, cfg.Timeout)
			if err != nil {
				logger.Log(fmt.Sprintf("Final response call failed: %v", err))
				return "", "", nil, err
			}
			return cfg.ModelName, finalResponse, finalTokenUsage, nil
		}

		// Execute tool calls
		for _, toolCall := range toolCalls {
			logger.Log(fmt.Sprintf("Executing tool: %s", toolCall.Function.Name))

			// Execute the tool call
			toolResponse, err := ExecuteBasicToolCall(toolCall, cfg.Config)
			if err != nil {
				logger.Log(fmt.Sprintf("Tool call failed: %v", err))
				toolResponse = fmt.Sprintf("Error executing tool %s: %v", toolCall.Function.Name, err)
			}

			// Add tool response to messages (convert "tool" role to "assistant" for API compatibility)
			currentMessages = append(currentMessages, prompts.Message{
				Role:    "assistant",
				Content: toolResponse,
			})

			totalToolCalls++

			// Update workflow state if this is an agent workflow
			if cfg.WorkflowContext.Type == WorkflowTypeAgent {
				updateAgentState(cfg.WorkflowContext, toolCall, toolResponse)
			}
		}

		logger.Log(fmt.Sprintf("Total tool calls so far: %d", totalToolCalls))
	}

	logger.Log("Maximum retries reached")
	return "", "", nil, fmt.Errorf("maximum interactive LLM retries reached (%d)", maxRetries)
}

// callLLMForSimpleResponseUnified handles simple responses without tools
func callLLMForSimpleResponseUnified(cfg *UnifiedInteractiveConfig) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.Config.SkipPrompt)

	response, tokenUsage, err := GetLLMResponse(
		cfg.ModelName,
		cfg.Messages,
		cfg.Filename,
		cfg.Config,
		cfg.Timeout,
	)
	if err != nil {
		logger.Log(fmt.Sprintf("Simple LLM call failed: %v", err))
		return "", "", nil, err
	}

	// Strip any tool calls if present (for consistency)
	response = prompts.StripToolCallsIfPresent(response)
	logger.Log(fmt.Sprintf("Simple response length: %d chars", len(response)))
	logger.Log("=== End Unified Interactive LLM Debug ===")

	return cfg.ModelName, response, tokenUsage, nil
}

// updateAgentState updates the agent workflow state based on tool calls and responses
func updateAgentState(ctx *WorkflowContext, toolCall ToolCall, response string) {
	if ctx.State == nil {
		ctx.State = make(map[string]interface{})
	}

	// Track tool usage
	toolUsage, exists := ctx.State["tool_usage"].(map[string]int)
	if !exists {
		toolUsage = make(map[string]int)
	}
	toolUsage[toolCall.Function.Name]++
	ctx.State["tool_usage"] = toolUsage

	// Track conversation history for context
	history, exists := ctx.State["history"].([]string)
	if !exists {
		history = make([]string, 0)
	}
	history = append(history, fmt.Sprintf("Tool: %s -> %s", toolCall.Function.Name, response))
	ctx.State["history"] = history
}

// GetCodeEditingWorkflowContext returns workflow context for code editing
func GetCodeEditingWorkflowContext() *WorkflowContext {
	return &WorkflowContext{
		Type:         WorkflowTypeCode,
		SystemPrompt: "You are an expert code editor. You can use tools to examine files, search code, and make edits. Focus on making accurate, minimal changes to achieve the user's goal.",
		MaxToolCalls: 5,
		State:        make(map[string]interface{}),
	}
}

// GetAgentWorkflowContext returns workflow context for agent workflows
func GetAgentWorkflowContext() *WorkflowContext {
	return &WorkflowContext{
		Type:         WorkflowTypeAgent,
		SystemPrompt: "You are an AI agent that can analyze code, understand user intent, and make changes. Use tools to gather information, plan changes, and execute them. Maintain context across multiple interactions to achieve complex goals.",
		MaxToolCalls: 8,
		State:        make(map[string]interface{}),
	}
}
