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
	ui "github.com/alantheprice/ledit/pkg/ui"
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
	ui.Out().Printf("🤖 LLM is using tool: %s\n", toolCall.Function.Name)

	// Parse the arguments from JSON string to map
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	name := toolCall.Function.Name
	start := time.Now()
	logger := utils.GetLogger(te.cfg.SkipPrompt)
	logger.LogProcessStep(fmt.Sprintf("TOOL CALL → %s args=%s", name, args))

	switch name {
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
	}
	logger.LogProcessStep(fmt.Sprintf("TOOL DONE ← %s in %s", name, time.Since(start)))
	return "", fmt.Errorf("unknown tool: %s", name)
}

func (te *ToolExecutor) executeWebSearch(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("search_web requires 'query' parameter")
	}

	// Notify user about web search being performed
	ui.Out().Printf("🔍 Searching web for: %s\n", query)
	ui.PublishStatus("Searching the web…")

	// Use the FetchContextFromSearch function that exists in webcontent package
	result, err := webcontent.FetchContextFromSearch(query, te.cfg)
	if err != nil {
		ui.Out().Printf("   ❌ Web search failed: %v\n", err)
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if result == "" {
		ui.Out().Printf("   ⚠️  No relevant web content found\n")
		return "No relevant web content found for the query.", nil
	}

	ui.Out().Printf("   ✅ Web search completed (%d bytes of content)\n", len(result))
	return result, nil
}

func (te *ToolExecutor) executeReadFile(args map[string]interface{}) (string, error) {
	path, ok := args["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("read_file requires 'file_path' parameter")
	}

	// Notify user about file being read
	ui.Out().Printf("📖 Reading file: %s\n", path)
	ui.PublishStatus("Reading file content…")

	content, err := filesystem.ReadFile(path)
	if err != nil {
		ui.Out().Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	ui.Out().Printf("   ✅ File read successfully (%d bytes)\n", len(content))
	return string(content), nil
}

func (te *ToolExecutor) executeShellCommand(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("run_shell_command requires 'command' parameter")
	}

	// Notify user about what command is being executed
	ui.Out().Printf("🔧 Executing shell command: %s\n", command)
	ui.PublishStatus("Running a shell command…")

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		ui.Out().Printf("   ❌ Command failed: %v\n", err)
		return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	ui.Out().Printf("   ✅ Command completed successfully\n")
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
			return "", fmt.Errorf("workspace.json not found. Please run 'ledit init' or ensure you are in a ledir project")
		}
		return "", fmt.Errorf("failed to load workspace: %w", err)
	}

	switch action {
	case "search_embeddings":
		query, ok := args["query"].(string)
		if !ok {
			return "", fmt.Errorf("workspace_context action 'search_embeddings' requires 'query' parameter")
		}
		ui.Out().Printf("🧠 Searching workspace embeddings for: %s\n", query)
		ui.PublishStatus("Searching workspace embeddings…")
		fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(query, ws, te.cfg, logger)
		if err != nil {
			ui.Out().Printf("   ❌ Embedding search failed: %v\n", err)
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
		ui.Out().Printf("   ✅ Embedding search completed.\n")
		return result.String(), nil

	case "search_keywords":
		query, ok := args["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("workspace_context action 'search_keywords' requires non-empty 'query' parameter")
		}
		ui.Out().Printf("🔎 Keyword searching workspace for: %s\n", query)
		ui.PublishStatus("Searching workspace by keywords…")
		// Use a ripgrep fallback to grep for keywords across Go files; include fallback to grep if rg not available
		cmd := exec.Command("sh", "-c", fmt.Sprintf("command -v rg >/dev/null 2>&1 && rg -n -l -i --glob '*.go' %q . || grep -r -n -l -i --include=*.go %q .", query, query))
		output, err := cmd.Output()
		if err != nil {
			ui.Out().Printf("   ❌ Keyword search failed: %v\n", err)
			return "", fmt.Errorf("keyword search failed: %w", err)
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		var result strings.Builder
		result.WriteString("Files found via keyword search:\n")
		count := 0
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			// Strip line numbers if present
			if idx := strings.Index(line, ":"); idx > -1 {
				line = line[:idx]
			}
			result.WriteString(fmt.Sprintf("  - %s\n", strings.TrimPrefix(line, "./")))
			count++
		}
		if count == 0 {
			result.WriteString("  No matches found.\n")
		}
		ui.Out().Printf("   ✅ Keyword search completed (%d files).\n", count)
		return result.String(), nil

	case "load_tree":
		ui.Out().Printf("🌳 Loading workspace file tree.\n")
		ui.PublishStatus("Loading workspace tree…")
		tree, err := workspace.GetFormattedFileTree(ws)
		if err != nil {
			ui.Out().Printf("   ❌ Failed to load file tree: %v\n", err)
			return "", fmt.Errorf("failed to load file tree: %w", err)
		}
		ui.Out().Printf("   ✅ File tree loaded.\n")
		return tree, nil

	case "load_summary":
		ui.Out().Printf("📝 Loading full workspace summary.\n")
		ui.PublishStatus("Building workspace summary…")
		summary, err := workspace.GetFullWorkspaceSummary(ws, te.cfg.CodeStyle, te.cfg, logger)
		if err != nil {
			ui.Out().Printf("   ❌ Failed to load workspace summary: %v\n", err)
			return "", fmt.Errorf("failed to load workspace summary: %w", err)
		}
		ui.Out().Printf("   ✅ Workspace summary loaded.\n")
		return summary, nil

	default:
		return "", fmt.Errorf("unknown action for workspace_context: %s. Valid actions are 'search_embeddings', 'search_keywords', 'load_tree', 'load_summary'", action)
	}
}

// CallLLMWithToolSupport makes an LLM call with tool calling support
func CallLLMWithToolSupport(modelName string, messages []prompts.Message, systemPrompt string, cfg *config.Config, timeout time.Duration) (string, error) {
	// Enhance the system prompt with tool information
	enhancedSystemPrompt := systemPrompt + "\n\n" + llm.FormatToolsForPrompt()

	// For now, we'll use the existing GetLLMResponse function and parse tool calls manually
	// In a full implementation, you'd modify the LLM API calls to support native tool calling
	response, _, err := llm.GetLLMResponse(modelName, messages, enhancedSystemPrompt, cfg, timeout)
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
	finalResponse, _, err := llm.GetLLMResponse(modelName, messages, enhancedSystemPrompt, cfg, timeout)

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
