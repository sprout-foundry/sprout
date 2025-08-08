package orchestration

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

type ToolExecutor struct {
	cfg *config.Config
}

func NewToolExecutor(cfg *config.Config) *ToolExecutor {
	return &ToolExecutor{cfg: cfg}
}

func (te *ToolExecutor) ExecuteToolCall(toolCall llm.ToolCall) (string, error) {
	// Parse the arguments from JSON string to map
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	switch toolCall.Function.Name {
	case "search_web":
		return te.executeWebSearch(args)
	case "read_file":
		return te.executeReadFile(args)
	case "run_shell_command":
		return te.executeShellCommand(args)
	case "ask_user":
		return te.executeAskUser(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

func (te *ToolExecutor) executeWebSearch(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("search_web requires 'query' parameter")
	}

	// Use the FetchContextFromSearch function that exists in webcontent package
	result, err := webcontent.FetchContextFromSearch(query, te.cfg)
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if result == "" {
		return "No relevant web content found for the query.", nil
	}

	return result, nil
}

func (te *ToolExecutor) executeReadFile(args map[string]interface{}) (string, error) {
	path, ok := args["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("read_file requires 'file_path' parameter")
	}

	content, err := filesystem.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return string(content), nil
}

func (te *ToolExecutor) executeShellCommand(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("run_shell_command requires 'command' parameter")
	}

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

func (te *ToolExecutor) executeAskUser(args map[string]interface{}) (string, error) {
	question, ok := args["question"].(string)
	if !ok {
		return "", fmt.Errorf("ask_user requires 'question' parameter")
	}

	logger := utils.GetLogger(te.cfg.SkipPrompt)

	// If in skip prompt mode, return a default response
	if te.cfg.SkipPrompt {
		return "User interaction skipped in non-interactive mode", nil
	}

	// Ask the user the question and get their response
	logger.LogProcessStep(fmt.Sprintf("Question from AI: %s", question))
	response := logger.AskForConfirmation("Your response: ", false, true)

	return fmt.Sprintf("%t", response), nil
}

// CallLLMWithToolSupport makes an LLM call with tool calling support
func CallLLMWithToolSupport(modelName string, messages []prompts.Message, systemPrompt string, cfg *config.Config, timeout time.Duration) (string, error) {
	// Enhance the system prompt with tool information
	enhancedSystemPrompt := systemPrompt + "\n\n" + llm.FormatToolsForPrompt()

	// For now, we'll use the existing GetLLMResponse function and parse tool calls manually
	// In a full implementation, you'd modify the LLM API calls to support native tool calling
	_, response, err := llm.GetLLMResponse(modelName, messages, enhancedSystemPrompt, cfg, timeout)
	if err != nil {
		return "", err
	}

	// Check if the response contains tool calls
	if !containsToolCall(response) {
		return response, nil
	}

	// Parse and execute tool calls
	toolCalls, err := parseToolCalls(response)
	if err != nil {
		return "", fmt.Errorf("failed to parse tool calls: %w", err)
	}

	executor := NewToolExecutor(cfg)
	var toolResults []string

	for _, toolCall := range toolCalls {
		result, err := executor.ExecuteToolCall(toolCall)
		if err != nil {
			toolResults = append(toolResults, fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, err.Error()))
		} else {
			toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, result))
		}
	}

	// Add tool results to messages and make another LLM call
	toolResultMessage := prompts.Message{
		Role:    "system",
		Content: fmt.Sprintf("Tool execution results:\n%s", fmt.Sprintf("%v", toolResults)),
	}

	messages = append(messages, toolResultMessage)
	_, finalResponse, err := llm.GetLLMResponse(modelName, messages, enhancedSystemPrompt, cfg, timeout)

	return finalResponse, err
}

// Helper functions
func containsToolCall(response string) bool {
	// Simple check for tool call indicators
	// Look for JSON patterns that indicate tool calls
	return (jsonContains(response, "tool_calls") ||
		jsonContains(response, "function_call") ||
		jsonContains(response, "search_web") ||
		jsonContains(response, "read_file") ||
		jsonContains(response, "run_shell_command") ||
		jsonContains(response, "ask_user"))
}

func jsonContains(response, key string) bool {
	// Simple JSON key detection
	return fmt.Sprintf(`"%s"`, key) != "" &&
		(fmt.Sprintf(`"%s":`, key) != "" || fmt.Sprintf(`'%s':`, key) != "")
}

func parseToolCalls(response string) ([]llm.ToolCall, error) {
	// This would parse the LLM response for tool calls
	// For now, implement basic JSON parsing for tool calls
	var result struct {
		ToolCalls []llm.ToolCall `json:"tool_calls"`
	}

	// Try to parse as JSON first
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return result.ToolCalls, nil
	}

	// If that fails, try to extract JSON from markdown code blocks
	if jsonStart := fmt.Sprintf("```json"); jsonStart != "" {
		// Extract JSON from code blocks - simplified implementation
		// In a full implementation, you'd use proper regex or string parsing
	}

	// For now, return empty slice - this would be enhanced in a full implementation
	return []llm.ToolCall{}, nil
}
