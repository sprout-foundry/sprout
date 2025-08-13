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
	"github.com/alantheprice/ledit/pkg/prompts"
)

// ContextHandler is a function type that defines how context requests are handled.
// It takes a slice of ContextRequest and returns a string response and an error.
type ContextHandler func([]ContextRequest, *config.Config) (string, error)

// ContextRequest represents a request for additional context from the LLM.
type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

// ContextResponse represents the LLM's response containing context requests.
type ContextResponse struct {
	ContextRequests []ContextRequest `json:"context_requests"`
}

// CallLLMWithInteractiveContext handles interactive LLM calls, processing context requests, and retrying the LLM call.
// This now supports both legacy context handling and new tool calling
func CallLLMWithInteractiveContext(
	modelName string,
	initialMessages []prompts.Message,
	filename string,
	cfg *config.Config,
	timeout time.Duration,
	contextHandler ContextHandler, // This is the key: it takes a handler function
) (string, error) {
	// Create file detector for automatic file detection
	detector := NewFileDetector()

	// Analyze the user's message for mentioned files
	var userPrompt string
	for _, msg := range initialMessages {
		if msg.Role == "user" {
			userPrompt += fmt.Sprintf("%v ", msg.Content)
		}
	}

	mentionedFiles := detector.DetectMentionedFiles(userPrompt)

	// Enhance the system prompt with tool information
	var enhancedMessages []prompts.Message

	// Add tool information to the system message if it exists
	for i, msg := range initialMessages {
		if i == 0 && msg.Role == "system" {
			enhancedContent := fmt.Sprintf("%s\n\n%s", msg.Content, FormatToolsForPrompt())

			// Add file detection warning if files were mentioned
			if len(mentionedFiles) > 0 {
				fileWarning := GenerateFileReadPrompt(mentionedFiles)
				enhancedContent += fileWarning
			}

			enhancedMessages = append(enhancedMessages, prompts.Message{
				Role:    msg.Role,
				Content: enhancedContent,
			})
		} else {
			enhancedMessages = append(enhancedMessages, msg)
		}
	}

	// If no system message, add tools as first message
	if len(enhancedMessages) == 0 || enhancedMessages[0].Role != "system" {
		toolContent := FormatToolsForPrompt()

		// Add file detection warning if files were mentioned
		if len(mentionedFiles) > 0 {
			fileWarning := GenerateFileReadPrompt(mentionedFiles)
			toolContent += fileWarning
		}

		toolMessage := prompts.Message{
			Role:    "system",
			Content: toolContent,
		}
		enhancedMessages = append([]prompts.Message{toolMessage}, enhancedMessages...)
	}

	currentMessages := enhancedMessages
	maxRetries := 6 // Limit the number of interactive turns

	for i := 0; i < maxRetries; i++ {
		fmt.Printf("[tools] turn %d/%d\n", i+1, maxRetries)
		// Call the main LLM response function (which is in api.go, same package)
		response, _, err := GetLLMResponse(modelName, currentMessages, filename, cfg, timeout)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		fmt.Println("[tools] model returned a response")

		// Check if the response contains tool calls (preferred method)
		if containsToolCall(response) {
			// Parse and execute tool calls
			toolCalls, err := parseToolCalls(response)
			if err != nil || len(toolCalls) == 0 {
				toolCalls, err = extractToolCallsFromResponse(response)
				if err != nil {
					// Log the response that failed to parse for debugging
					fmt.Printf("Failed to parse tool calls from response (length %d chars): %.100s...\n", len(response), response)
					return "", fmt.Errorf("failed to parse tool calls: %w", err)
				}
			}

			if len(toolCalls) > 0 {
				// Execute tool calls using basic implementation
				var toolResults []string
				for _, toolCall := range toolCalls {
					fmt.Printf("[tools] executing %s\n", toolCall.Function.Name)
					result, err := executeBasicToolCall(toolCall, cfg)
					if err != nil {
						toolResults = append(toolResults, fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, err.Error()))
					} else {
						toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, result))
					}
				}

				// Add tool results to messages and continue
				toolResultMessage := prompts.Message{
					Role:    "system",
					Content: fmt.Sprintf("Tool execution results:\n%s", strings.Join(toolResults, "\n")),
				}

				currentMessages = append(currentMessages, toolResultMessage)
				continue
			}
		}

		// Fallback to legacy context request handling
		if strings.Contains(response, "context_requests") {
			contextRequests, err := extractContextRequests(response)
			if err != nil {
				return "", fmt.Errorf("failed to extract context requests: %w", err)
			}

			if len(contextRequests) > 0 {
				// Handle the context requests using the provided handler
				contextContent, err := contextHandler(contextRequests, cfg)
				if err != nil {
					return "", fmt.Errorf("failed to handle context request: %w", err)
				}

				// Append the context content as a new message from the user
				currentMessages = append(currentMessages, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("Context information:\n%s", contextContent),
				})
				// Continue the loop to send the updated messages to the LLM
				continue
			}
		}

		// If no context request, or if all requests were handled, return the response
		return response, nil
	}

	return "", fmt.Errorf("max interactive LLM retries reached (%d)", maxRetries)
}

// Helper functions for tool calling
func containsToolCall(response string) bool {
	// Check for explicit tool call JSON structures with proper context
	// Must be at the start of the response or in a JSON code block
	trimmed := strings.TrimSpace(response)

	// Check if response starts with JSON containing tool_calls
	if strings.HasPrefix(trimmed, "{") && strings.Contains(response, `"tool_calls"`) {
		return true
	}

	// Check for JSON code blocks that contain tool_calls
	if strings.Contains(response, "```json") {
		// Extract JSON blocks and check if they contain tool_calls
		start := strings.Index(response, "```json")
		if start >= 0 {
			start += 7
			end := strings.Index(response[start:], "```")
			if end > 0 {
				jsonContent := response[start : start+end]
				if strings.Contains(jsonContent, `"tool_calls"`) {
					return true
				}
			}
		}
	}

	return false
}

func parseToolCalls(response string) ([]ToolCall, error) {
	// First try to parse as a direct tool call structure (without role)
	var directToolCall struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(response), &directToolCall); err == nil && len(directToolCall.ToolCalls) > 0 {
		// Convert to our ToolCall structure with Arguments as JSON string
		var toolCalls []ToolCall
		for _, tc := range directToolCall.ToolCalls {
			argsBytes, err := json.Marshal(tc.Function.Arguments)
			if err != nil {
				continue
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: string(argsBytes),
				},
			})
		}
		return toolCalls, nil
	}

	// Try to parse the response as a full tool message (with role)
	var toolMessage ToolMessage
	if err := json.Unmarshal([]byte(response), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
		return toolMessage.ToolCalls, nil
	}

	return []ToolCall{}, nil
}

func extractToolCallsFromResponse(response string) ([]ToolCall, error) {
	// Look for JSON blocks in the response
	if strings.Contains(response, "```json") {
		start := strings.Index(response, "```json") + 7
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])

			// First try direct tool call structure with object arguments
			var directToolCall struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string                 `json:"name"`
						Arguments map[string]interface{} `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			}

			if err := json.Unmarshal([]byte(jsonStr), &directToolCall); err == nil && len(directToolCall.ToolCalls) > 0 {
				// Convert to our ToolCall structure with Arguments as JSON string
				var toolCalls []ToolCall
				for _, tc := range directToolCall.ToolCalls {
					argsBytes, err := json.Marshal(tc.Function.Arguments)
					if err != nil {
						continue
					}
					toolCalls = append(toolCalls, ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: ToolCallFunction{
							Name:      tc.Function.Name,
							Arguments: string(argsBytes),
						},
					})
				}
				return toolCalls, nil
			}

			// Try full tool message structure
			var toolMessage ToolMessage
			if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
				return toolMessage.ToolCalls, nil
			}
		}
	}

	return []ToolCall{}, fmt.Errorf("no tool calls found in response")
}

func executeBasicToolCall(toolCall ToolCall, cfg *config.Config) (string, error) {
	// Parse the arguments - they might be a JSON string or already parsed object
	var args map[string]interface{}

	// First try to unmarshal as JSON string
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		// If that fails, the arguments might already be parsed and stored as string
		// This handles cases where the JSON was parsed incorrectly during tool call extraction
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
			fmt.Printf("\n🤖 Question: %s\n", question)
			fmt.Print("Your answer: ")
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

	case "search_web":
		if query, ok := args["query"].(string); ok {
			// This would require importing webcontent package, which creates circular import
			// For now, return a message indicating the tool needs to be implemented
			return fmt.Sprintf("Web search for '%s' - tool implementation needed", query), nil
		}
		return "", fmt.Errorf("search_web requires 'query' parameter")

	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

func extractContextRequests(response string) ([]ContextRequest, error) {
	// Try to find JSON in the response
	var contextResp ContextResponse

	// First try parsing the whole response as JSON
	if err := json.Unmarshal([]byte(response), &contextResp); err == nil {
		return contextResp.ContextRequests, nil
	}

	// Look for JSON blocks
	if strings.Contains(response, "```json") {
		start := strings.Index(response, "```json") + 7
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &contextResp); err == nil {
				return contextResp.ContextRequests, nil
			}
		}
	}

	// Look for bare JSON
	if strings.Contains(response, "context_requests") {
		// Try to extract JSON object containing context_requests
		start := strings.Index(response, "{")
		if start >= 0 {
			// Find the matching closing brace
			depth := 0
			for i := start; i < len(response); i++ {
				if response[i] == '{' {
					depth++
				} else if response[i] == '}' {
					depth--
					if depth == 0 {
						jsonStr := response[start : i+1]
						if err := json.Unmarshal([]byte(jsonStr), &contextResp); err == nil {
							return contextResp.ContextRequests, nil
						}
						break
					}
				}
			}
		}
	}

	return []ContextRequest{}, nil
}
