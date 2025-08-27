package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CallLLMForCodeEditing handles LLM calls specifically for code editing tasks.
// This is focused on code generation and editing, not planning workflows.
func CallLLMForCodeEditing(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, contextHandler ContextHandler) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	ui.Out().Printf("=== CODE EDITING LLM FUNCTION CALLED ===\n")
	logger.Log("=== CODE EDITING LLM FUNCTION CALLED ===")
	logger.Log("=== Code Editing LLM Debug ===")
	logger.Log(fmt.Sprintf("Model: %s", modelName))
	logger.Log(fmt.Sprintf("Filename: %s", filename))
	logger.Log(fmt.Sprintf("Initial messages count: %d", len(messages)))

	// Set tool limit to 1 less than orchestration max attempts
	maxToolCalls := cfg.OrchestrationMaxAttempts - 1
	if maxToolCalls <= 0 {
		maxToolCalls = 7 // Default to 7 if config is not set or invalid
	}
	logger.Log(fmt.Sprintf("Max tool calls: %d", maxToolCalls))

	// Force tool-calling path for code editing
	logger.Log("Using interactive mode with code editing focused tool calling")

	// For code editing, we want a more focused approach than the planning workflow
	// Use a simpler retry mechanism without complex planning requirements
	return callLLMForCodeEditingWithSimpleRetry(modelName, messages, filename, cfg, timeout, contextHandler, maxToolCalls)
}

// callLLMForCodeEditingWithSimpleRetry implements a simpler retry mechanism focused on code editing
func callLLMForCodeEditingWithSimpleRetry(modelName string, initialMessages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, contextHandler ContextHandler, maxToolCalls int) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	currentMessages := make([]prompts.Message, len(initialMessages))
	copy(currentMessages, initialMessages)

	// Track tool calls
	totalToolCalls := 0
	attempts := 0
	maxRetries := cfg.OrchestrationMaxAttempts
	if maxRetries <= 0 {
		maxRetries = 8
	}

	for attempts < maxRetries {
		attempts++
		logger.Log(fmt.Sprintf("Code editing attempt %d/%d", attempts, maxRetries))

		// Get available tools for code editing
		availableTools := GetAvailableTools()
		logger.Log(fmt.Sprintf("Available tools: %d", len(availableTools)))

		// Make the LLM call with tools
		// Get available tool names for the allowed list
		var toolNames []string
		for _, tool := range availableTools {
			toolNames = append(toolNames, tool.Function.Name)
		}
		response, tokenUsage, err := GetLLMResponseWithToolsScoped(modelName, currentMessages, "", cfg, timeout, toolNames)
		if err != nil {
			logger.Log(fmt.Sprintf("LLM call failed: %v", err))
			return "", "", nil, err
		}

		// Parse tool calls from response
		toolCalls, err := ParseToolCalls(response)
		if err != nil {
			logger.Log(fmt.Sprintf("Failed to parse tool calls: %v", err))
			// If we can't parse tool calls, return the response as-is
			return modelName, response, tokenUsage, nil
		}

		logger.Log(fmt.Sprintf("Tool calls found: %d", len(toolCalls)))

		// If no tool calls, return the response
		if len(toolCalls) == 0 {
			logger.Log("No tool calls - returning response")
			return modelName, response, nil, nil
		}

		// Check if we've exceeded the maximum tool calls limit
		totalToolCalls += len(toolCalls)
		if totalToolCalls > maxToolCalls {
			logger.Log(fmt.Sprintf("Maximum tool calls (%d) exceeded, forcing final response", maxToolCalls))
			// Add a system message to force completion
			currentMessages = append(currentMessages, prompts.Message{
				Role:    "system",
				Content: "Maximum tool calls reached. Please provide your final code response without using any more tools.",
			})

			// Make one final call without tools
			finalResponse, tokenUsage, err := GetLLMResponse(modelName, currentMessages, filename, cfg, timeout)
			if err != nil {
				logger.Log(fmt.Sprintf("Final response call failed: %v", err))
				return "", "", nil, err
			}
			return modelName, finalResponse, tokenUsage, nil
		}

		// Process tool calls and get context responses
		var contextResponses []string
		for _, toolCall := range toolCalls {
			logger.Log(fmt.Sprintf("Processing tool call: %s", toolCall.Function.Name))

			// Execute the tool call using the code editing tool executor
			toolResponse, err := executeCodeEditingToolCall(toolCall, cfg)
			if err != nil {
				logger.Log(fmt.Sprintf("Tool call failed: %v", err))
				contextResponses = append(contextResponses, fmt.Sprintf("Error executing %s: %v", toolCall.Function.Name, err))
				continue
			}

			contextResponses = append(contextResponses, fmt.Sprintf("Result of %s: %s", toolCall.Function.Name, toolResponse))
		}

		// Add tool responses to conversation
		if len(contextResponses) > 0 {
			combinedResponse := strings.Join(contextResponses, "\n\n")
			currentMessages = append(currentMessages, prompts.Message{
				Role:    "user",
				Content: fmt.Sprintf("Here are the results of the tool calls:\n\n%s\n\nPlease continue with your code generation or editing.", combinedResponse),
			})
		}

		// Safety check to prevent infinite loops
		if attempts >= maxRetries-1 {
			logger.Log("Approaching maximum retries, forcing completion")
			// Add a system message to force completion
			currentMessages = append(currentMessages, prompts.Message{
				Role:    "system",
				Content: "You have made several tool calls. Please now provide your final code response without using any more tools. Focus on completing the code generation or editing task.",
			})

			// Make one final call without tools
			finalResponse, tokenUsage, err := GetLLMResponse(modelName, currentMessages, filename, cfg, timeout)
			if err != nil {
				logger.Log(fmt.Sprintf("Final response call failed: %v", err))
				return "", "", nil, err
			}
			return modelName, finalResponse, tokenUsage, nil
		}
	}

	logger.Log("Maximum retries reached")
	return "", "", nil, fmt.Errorf("maximum interactive LLM retries reached (%d)", maxRetries)
}

// executeCodeEditingToolCall executes a tool call and returns the result for code editing
func executeCodeEditingToolCall(toolCall ToolCall, cfg *config.Config) (string, error) {
	// Parse the arguments - they might be a JSON string or already parsed object
	var args map[string]interface{}

	// Prefer Arguments if present; fallback to Parameters
	argSource := toolCall.Function.Arguments
	if strings.TrimSpace(argSource) == "" && len(toolCall.Function.Parameters) > 0 {
		argSource = string(toolCall.Function.Parameters)
	}

	// First try to unmarshal as JSON string
	if err := json.Unmarshal([]byte(argSource), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	switch toolCall.Function.Name {
	case "read_file":
		if filePath, ok := args["file_path"].(string); ok {
			// Use the filesystem package to read the file
			content, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
			}
			return string(content), nil
		}
		return "", fmt.Errorf("read_file requires 'file_path' parameter")

	case "ask_user":
		if question, ok := args["question"].(string); ok {
			if cfg.SkipPrompt {
				return "User interaction skipped in non-interactive mode", nil
			}
			ui.Out().Printf("\n🤖 Question: %s\n", question)
			ui.Out().Print("Your answer: ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			return strings.TrimSpace(answer), nil
		}
		return "", fmt.Errorf("ask_user requires 'question' parameter")

	case "run_shell_command":
		if command, ok := args["command"].(string); ok {
			cmd := exec.Command("sh", "-c", command)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
			}
			return string(output), nil
		}
		return "", fmt.Errorf("run_shell_command requires 'command' parameter")

	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

// isLikelyCodeFile returns true for typical text/code files
func isLikelyCodeFile(path string) bool {
	lower := strings.ToLower(path)
	// Common source and text extensions
	exts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".php", ".kt", ".m", ".mm", ".swift", ".scala", ".sql", ".sh", ".bash", ".zsh", ".fish", ".yaml", ".yml", ".json", ".toml", ".ini", ".md", ".txt"}
	for _, e := range exts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return false
}

// CallLLMForCodeEditingWithPatches handles LLM calls for code editing using patch syntax
func CallLLMForCodeEditingWithPatches(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, contextHandler ContextHandler) (string, string, *TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	ui.Out().Printf("=== PATCH-BASED CODE EDITING LLM FUNCTION CALLED ===\n")
	logger.Log("=== PATCH-BASED CODE EDITING LLM FUNCTION CALLED ===")

	// For patch-based editing, use simpler approach without tool loops
	response, tokenUsage, err := GetLLMResponse(modelName, messages, filename, cfg, timeout)
	if err != nil {
		logger.Log(fmt.Sprintf("Direct LLM call failed: %v", err))
		return "", "", nil, err
	}

	logger.Log(fmt.Sprintf("Direct response length: %d chars", len(response)))

	// Parse patches from response
	patches, err := parser.GetUpdatedCodeFromPatchResponse(response)
	if err != nil {
		logger.Log(fmt.Sprintf("Failed to parse patches: %v", err))
		return "", "", nil, fmt.Errorf("failed to parse patches: %w", err)
	}

	if len(patches) == 0 {
		logger.Log("No patches found in response")
		return "", "", nil, fmt.Errorf("no patches found in LLM response")
	}

	logger.Log(fmt.Sprintf("Found %d patches to apply", len(patches)))

	// Apply patches to files
	appliedFiles := []string{}
	for filename, patch := range patches {
		logger.Log(fmt.Sprintf("Applying patch to %s (%d hunks)", filename, len(patch.Hunks)))

		// Check if file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			logger.Log(fmt.Sprintf("File does not exist: %s", filename))
			continue
		}

		// Apply the patch with enhanced error handling
		if err := parser.EnhancedApplyPatchToFile(patch, filename); err != nil {
			logger.Log(fmt.Sprintf("Failed to apply patch to %s: %v", filename, err))
			// Check if it's a custom patch error with suggestions
			if patchErr, ok := err.(*parser.PatchError); ok {
				logger.Log(fmt.Sprintf("Error type: %s, Suggestion: %s", patchErr.Type, patchErr.Suggestion))
			}
			return "", "", nil, fmt.Errorf("failed to apply patch to %s: %w", filename, err)
		}

		appliedFiles = append(appliedFiles, filename)
		logger.Log(fmt.Sprintf("Successfully applied patch to %s", filename))
	}

	if len(appliedFiles) == 0 {
		return "", "", nil, fmt.Errorf("no patches were successfully applied")
	}

	// Return summary of applied changes
	summary := fmt.Sprintf("Applied patches to %d files: %s", len(appliedFiles), strings.Join(appliedFiles, ", "))
	logger.Log("=== End Patch-Based Code Editing LLM Debug ===")
	return summary, modelName, tokenUsage, nil
}
