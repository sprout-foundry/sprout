package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
	"github.com/alantheprice/ledit/pkg/workspace"
)

type ToolExecutor struct {
	cfg *config.Config
}

func NewToolExecutor(cfg *config.Config) *ToolExecutor {
	return &ToolExecutor{cfg: cfg}
}

func (te *ToolExecutor) ExecuteToolCall(toolCall llm.ToolCall) (string, error) {
	// Log the tool being used
	fmt.Printf("🤖 LLM is using tool: %s\n", toolCall.Function.Name)

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
	case "workspace_context":
		return te.executeWorkspaceContext(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

func (te *ToolExecutor) executeWebSearch(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("search_web requires 'query' parameter")
	}

	// Notify user about web search being performed
	fmt.Printf("🔍 Searching web for: %s\n", query)

	// Use the FetchContextFromSearch function that exists in webcontent package
	result, err := webcontent.FetchContextFromSearch(query, te.cfg)
	if err != nil {
		fmt.Printf("   ❌ Web search failed: %v\n", err)
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if result == "" {
		fmt.Printf("   ⚠️  No relevant web content found\n")
		return "No relevant web content found for the query.", nil
	}

	fmt.Printf("   ✅ Web search completed (%d bytes of content)\n", len(result))
	return result, nil
}

func (te *ToolExecutor) executeReadFile(args map[string]interface{}) (string, error) {
	path, ok := args["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("read_file requires 'file_path' parameter")
	}

	// Notify user about file being read
	fmt.Printf("📖 Reading file: %s\n", path)

	content, err := filesystem.ReadFile(path)
	if err != nil {
		fmt.Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	fmt.Printf("   ✅ File read successfully (%d bytes)\n", len(content))
	return string(content), nil
}

func (te *ToolExecutor) executeShellCommand(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("run_shell_command requires 'command' parameter")
	}

	// Notify user about what command is being executed
	fmt.Printf("🔧 Executing shell command: %s\n", command)

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("   ❌ Command failed: %v\n", err)
		return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("   ✅ Command completed successfully\n")
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

// executeWorkspaceContext handles the "workspace_context" tool call.
func (te *ToolExecutor) executeWorkspaceContext(args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok {
		return "", fmt.Errorf("workspace_context requires 'action' parameter")
	}

	logger := utils.GetLogger(te.cfg.SkipPrompt)

	// Load workspace once, as it's needed for all actions
	ws, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace.json not found. Please run 'ledit init' or ensure you are in a ledir project.")
		}
		return "", fmt.Errorf("failed to load workspace: %w", err)
	}

	switch action {
	case "search_embeddings":
		query, ok := args["query"].(string)
		if !ok {
			return "", fmt.Errorf("workspace_context action 'search_embeddings' requires 'query' parameter")
		}
		fmt.Printf("🧠 Searching workspace embeddings for: %s\n", query)
		fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(query, ws, te.cfg, logger)
		if err != nil {
			fmt.Printf("   ❌ Embedding search failed: %v\n", err)
			return "", fmt.Errorf("embedding search failed: %w", err)
		}

		var result strings.Builder
		result.WriteString("Files found via embedding search:\n")
		if len(fullContextFiles) > 0 {
			result.WriteString("  Full Context Files:\n")
			for _, f := range fullContextFiles {
				result.WriteString(fmt.Sprintf("    - %s\n", f))
			}
		}
		if len(summaryContextFiles) > 0 {
			result.WriteString("  Summary Context Files:\n")
			for _, f := range summaryContextFiles {
				result.WriteString(fmt.Sprintf("    - %s\n", f))
			}
		}
		if len(fullContextFiles) == 0 && len(summaryContextFiles) == 0 {
			result.WriteString("  No relevant files found.\n")
		}
		fmt.Printf("   ✅ Embedding search completed.\n")
		return result.String(), nil

	case "load_tree":
		fmt.Printf("🌳 Loading workspace file tree.\n")
		tree, err := workspace.GetFormattedFileTree(ws)
		if err != nil {
			fmt.Printf("   ❌ Failed to load file tree: %v\n", err)
			return "", fmt.Errorf("failed to load file tree: %w", err)
		}
		fmt.Printf("   ✅ File tree loaded.\n")
		return tree, nil

	case "load_summary":
		fmt.Printf("📝 Loading full workspace summary.\n")
		summary, err := workspace.GetFullWorkspaceSummary(ws, te.cfg.CodeStyle, te.cfg, logger)
		if err != nil {
			fmt.Printf("   ❌ Failed to load workspace summary: %v\n", err)
			return "", fmt.Errorf("failed to load workspace summary: %w", err)
		}
		fmt.Printf("   ✅ Workspace summary loaded.\n")
		return summary, nil

	default:
		return "", fmt.Errorf("unknown action for workspace_context: %s. Valid actions are 'search_embeddings', 'load_tree', 'load_summary'.", action)
	}
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
		jsonContains(response, "ask_user") ||
		jsonContains(response, "workspace_context"))
}

func jsonContains(response, key string) bool {
	// Simple JSON key detection
	return strings.Contains(response, fmt.Sprintf(`"%s"`, key)) || strings.Contains(response, fmt.Sprintf(`'%s'`, key))
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
	jsonStart := "```json"
	if strings.Contains(response, jsonStart) {
		startIndex := strings.Index(response, jsonStart)
		if startIndex != -1 {
			// Find the end of the JSON block
			endBlockMarker := "```"
			// Search for "```" after the start of the JSON block
			endIndex := strings.Index(response[startIndex+len(jsonStart):], endBlockMarker)
			if endIndex != -1 {
				jsonBlock := response[startIndex+len(jsonStart) : startIndex+len(jsonStart)+endIndex]
				if err := json.Unmarshal([]byte(jsonBlock), &result); err == nil {
					return result.ToolCalls, nil
				}
			}
		}
	}

	// If no tool calls found or parsing failed, return empty slice
	return []llm.ToolCall{}, nil
}
