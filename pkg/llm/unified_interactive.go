package llm

import (
	"fmt"
	"strings"
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
	logger.Log("Using interactive mode with tools (forced)")
	return callLLMWithToolsUnified(cfg)
}

// injectSystemPrompt injects or replaces the system prompt in the messages
func injectSystemPrompt(messages []prompts.Message, systemPrompt string) []prompts.Message {
	// Prepend an additional workflow-scoped system message so existing strict prompts (e.g., patch rules) remain intact.
	return append([]prompts.Message{{
		Role:    "system",
		Content: systemPrompt,
	}}, messages...)
}

// toolCallKey generates a deterministic key for a tool call for deduping
func toolCallKey(tc ToolCall) string {
	args := tc.Function.Arguments
	if strings.TrimSpace(args) == "" && len(tc.Function.Parameters) > 0 {
		args = string(tc.Function.Parameters)
	}
	payload := tc.Function.Name + "|" + args
	return utils.GenerateRequestHash(payload)
}

// executeToolWithPolicies runs a tool call with timeout, dedupe, and structured logs
func executeToolWithPolicies(tc ToolCall, cfg *UnifiedInteractiveConfig, seen map[string]bool, perCallTimeout time.Duration) (string, bool) {
	logger := utils.GetLogger(cfg.Config.SkipPrompt)
	key := toolCallKey(tc)
	if seen[key] {
		logger.Log("Skipping duplicate tool call: " + tc.Function.Name)
		return "", true
	}
	seen[key] = true

	// enforce per-call timeout
	if perCallTimeout <= 0 {
		perCallTimeout = 45 * time.Second
	}
	done := make(chan struct{})
	var result string
	var runErr error
	start := time.Now()

	go func() {
		defer close(done)
		result, runErr = ExecuteBasicToolCall(tc, cfg.Config)
	}()

	select {
	case <-done:
		// logged below
	case <-time.After(perCallTimeout):
		logger.Log(fmt.Sprintf("Tool %s timed out after %s", tc.Function.Name, perCallTimeout))
		return fmt.Sprintf("timeout: %s", tc.Function.Name), false
	}

	dur := time.Since(start)
	// Truncate large args/output for logs
	truncatedArgs := utils.TruncateString(tc.Function.Arguments, 400)
	truncatedOut := utils.TruncateString(result, 800)
	if runErr != nil {
		logger.Log(fmt.Sprintf("TOOL RESULT ✖ %s in %s args=%q err=%v", tc.Function.Name, dur, truncatedArgs, runErr))
		return fmt.Sprintf("error: %v", runErr), false
	}
	logger.Log(fmt.Sprintf("TOOL RESULT ✓ %s in %s args=%q out=%q", tc.Function.Name, dur, truncatedArgs, truncatedOut))
	return result, false
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

		// Execute tool calls with dedup, per-call timeout and structured logs
		seen := map[string]bool{}
		for _, toolCall := range toolCalls {
			logger.Log(fmt.Sprintf("🔧 TOOL CALL STARTING → %s (ID: %s)", toolCall.Function.Name, toolCall.ID))
			logger.Log(fmt.Sprintf("   Arguments: %s", utils.TruncateString(toolCall.Function.Arguments, 400)))

			if run := utils.GetRunLogger(); run != nil {
				run.LogEvent("tool_call", map[string]any{"tool": toolCall.Function.Name})
			}

			// Execute tools with proper response handling
			var toolResponse string
			var wasDuplicate bool
			var runErr error

			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Log(fmt.Sprintf("❌ TOOL PANIC in %s: %v", toolCall.Function.Name, r))
						toolResponse = fmt.Sprintf("Tool %s failed with panic: %v", toolCall.Function.Name, r)
						runErr = fmt.Errorf("tool panic: %v", r)
					}
				}()
				toolResponse, wasDuplicate = executeToolWithPolicies(toolCall, cfg, seen, 45*time.Second)
			}()

			if wasDuplicate {
				logger.Log(fmt.Sprintf("⚠️  TOOL SKIPPED - DUPLICATE: %s", toolCall.Function.Name))
				continue
			}

			if runErr != nil {
				logger.Log(fmt.Sprintf("❌ TOOL FAILED: %s - Error: %v", toolCall.Function.Name, runErr))
			} else {
				logger.Log(fmt.Sprintf("✅ TOOL COMPLETED: %s", toolCall.Function.Name))
			}

			// Clean the tool response to ensure valid JSON
			cleanResponse := strings.TrimSpace(toolResponse)
			logger.Log(fmt.Sprintf("   Tool Response: '%s'", utils.TruncateString(cleanResponse, 200)))

			if cleanResponse == "" {
				cleanResponse = fmt.Sprintf("Tool %s executed successfully (no output)", toolCall.Function.Name)
				logger.Log(fmt.Sprintf("   Using default success message"))
			}

			// Add tool response in a format compatible with the provider
			// Some providers don't support the "tool" role, so we include the response as part of the next user message
			toolInfo := fmt.Sprintf("[Tool Response: %s]\n%s", toolCall.Function.Name, cleanResponse)

			// Create a user message that includes the tool response
			toolMessage := prompts.Message{
				Role:    "user",
				Content: toolInfo,
			}
			if toolCall.ID != "" {
				toolMessage.ToolCallID = &toolCall.ID
			}

			logger.Log(fmt.Sprintf("📝 ADDING TOOL RESPONSE to conversation (Role: %s)", toolMessage.Role))
			logger.Log(fmt.Sprintf("   Tool Message Content Preview: '%s'", utils.TruncateString(toolInfo, 100)))
			currentMessages = append(currentMessages, toolMessage)
			totalToolCalls++
			if cfg.WorkflowContext.Type == WorkflowTypeAgent {
				updateAgentState(cfg.WorkflowContext, toolCall, toolResponse)
			}
		}

		logger.Log(fmt.Sprintf("Total tool calls so far: %d", totalToolCalls))
	}

	logger.Log("Maximum retries reached")
	return "", "", nil, fmt.Errorf("maximum interactive LLM retries reached (%d)", maxRetries)
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
		SystemPrompt: "", // Don't override workflow-specific system messages
		MaxToolCalls: 8,
		State:        make(map[string]interface{}),
	}
}
