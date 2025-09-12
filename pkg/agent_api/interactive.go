package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/tools"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CallLLMWithInteractiveContext is a compatibility wrapper that adapts the legacy interactive context
// functionality to use the new agent API system. This allows existing commands like cmd/process.go
// to migrate to the new API without significant changes.
func CallLLMWithInteractiveContext(
	modelName string,
	initialMessages []prompts.Message,
	filename string,
	cfg *config.Config,
	timeout time.Duration,
	contextHandler func([]ContextRequest, *config.Config) (string, error),
) (string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	
	// Create the appropriate client based on model name
	client, err := createClientForModel(modelName)
	if err != nil {
		return "", fmt.Errorf("failed to create client for model %s: %w", modelName, err)
	}
	
	// Convert legacy prompts.Message to agent API Message format
	messages := convertLegacyMessages(initialMessages)
	
	// Set up tools for interactive functionality
	tools := []Tool{
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "ask_user",
				Description: "Ask the user a question and get their response",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"question": map[string]interface{}{
							"type":        "string",
							"description": "The question to ask the user",
						},
					},
					"required": []string{"question"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "read_file", 
				Description: "Read the contents of a file. Supports reading entire files (up to 100KB, larger files are truncated) or specific line ranges for efficiency.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to read",
						},
						"start_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: Start line number (1-based) for reading a specific range",
						},
						"end_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: End line number (1-based) for reading a specific range",
						},
					},
					"required": []string{"file_path"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "run_shell_command",
				Description: "Execute a shell command",
				Parameters: map[string]interface{}{
					"type": "object", 
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "The shell command to execute",
						},
					},
					"required": []string{"command"},
				},
			},
		},
	}
	
	// Interactive loop with tool support
	maxRetries := cfg.OrchestrationMaxAttempts
	if maxRetries <= 0 {
		maxRetries = 8
	}
	
	for i := 0; i < maxRetries; i++ {
		logger.Logf("DEBUG: Interactive turn %d/%d", i+1, maxRetries)
		
		// Send request to LLM with tools
		response, err := client.SendChatRequest(messages, tools, "")
		if err != nil {
			return "", fmt.Errorf("LLM call failed on turn %d: %w", i+1, err)
		}
		
		// Extract content from first choice
		if len(response.Choices) == 0 {
			return "", fmt.Errorf("no choices in LLM response")
		}
		content := response.Choices[0].Message.Content
		
		// Check if response contains tool calls
		if hasToolCalls(content) {
			toolCalls, err := parseToolCallsFromResponse(content)
			if err != nil {
				logger.Logf("DEBUG: Failed to parse tool calls: %v", err)
				continue
			}
			
			// Execute tool calls and collect results
			var toolResults []string
			
			for _, toolCall := range toolCalls {
				result, _, err := executeToolCallWithContext(toolCall, contextHandler, cfg)
				if err != nil {
					toolResults = append(toolResults, fmt.Sprintf("Tool %s failed: %v", toolCall.Function.Name, err))
					continue
				}
				
				toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, result))
			}
			
			// Add tool results to conversation and continue
			toolResultMessage := Message{
				Role:    "system",
				Content: fmt.Sprintf("Tool execution results:\n%s", strings.Join(toolResults, "\n")),
			}
			messages = append(messages, toolResultMessage)
			
			// Continue the loop unless we're done
			continue
		}
		
		// If no tool calls, check for legacy context requests
		if strings.Contains(content, "context_requests") {
			contextRequests, err := extractContextRequestsFromResponse(content)
			if err != nil {
				logger.Logf("DEBUG: Failed to parse context requests: %v", err)
			} else if len(contextRequests) > 0 {
				// Handle context requests using the legacy handler
				contextContent, err := contextHandler(contextRequests, cfg)
				if err != nil {
					return "", fmt.Errorf("failed to handle context request: %w", err)
				}
				
				// Add context as user message and continue
				contextMessage := Message{
					Role:    "user",
					Content: fmt.Sprintf("Context information:\n%s", contextContent),
				}
				messages = append(messages, contextMessage)
				continue
			}
		}
		
		// No tool calls or context requests - return the response
		return content, nil
	}
	
	return "", fmt.Errorf("max interactive retries reached (%d)", maxRetries)
}

// createClientForModel creates the appropriate client based on the model name
func createClientForModel(modelName string) (ClientInterface, error) {
	// Use unified model selection to resolve model reference
	modelSelector := &ModelSelection{} // No config needed for basic resolution
	clientType, model, err := modelSelector.ResolveModelReference(modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve model %s: %w", modelName, err)
	}
	
	return NewUnifiedClientWithModel(clientType, model)
}

// convertLegacyMessages converts legacy prompts.Message to agent API Message
func convertLegacyMessages(legacyMessages []prompts.Message) []Message {
	messages := make([]Message, len(legacyMessages))
	
	for i, msg := range legacyMessages {
		messages[i] = Message{
			Role:    msg.Role,
			Content: fmt.Sprintf("%v", msg.Content), // Convert interface{} to string
		}
	}
	
	return messages
}

// hasToolCalls checks if a response contains tool calls
func hasToolCalls(content string) bool {
	return strings.Contains(content, `"tool_calls"`) || 
		   strings.Contains(content, `"function"`) ||
		   strings.Contains(content, "ask_user") ||
		   strings.Contains(content, "read_file") ||
		   strings.Contains(content, "run_shell_command")
}

// ToolCallResponse represents a tool call in the response
type ToolCallResponse struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// parseToolCallsFromResponse extracts tool calls from LLM response
func parseToolCallsFromResponse(content string) ([]ToolCallResponse, error) {
	// Simple parsing - look for tool_calls array
	var response struct {
		ToolCalls []ToolCallResponse `json:"tool_calls"`
	}
	
	// Try to parse as direct JSON
	if err := json.Unmarshal([]byte(content), &response); err == nil {
		return response.ToolCalls, nil
	}
	
	// Try to extract JSON from markdown code blocks
	if strings.Contains(content, "```json") {
		start := strings.Index(content, "```json") + 7
		end := strings.Index(content[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(content[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &response); err == nil {
				return response.ToolCalls, nil
			}
		}
	}
	
	// Fallback: try to find individual tool calls
	return extractIndividualToolCalls(content)
}

// extractIndividualToolCalls attempts to parse individual tool calls from malformed JSON
func extractIndividualToolCalls(content string) ([]ToolCallResponse, error) {
	// Simple pattern matching for common tool calls
	var toolCalls []ToolCallResponse
	
	// Look for ask_user patterns
	if strings.Contains(content, "ask_user") {
		if question := extractBetween(content, `"question"`, `"`); question != "" {
			toolCalls = append(toolCalls, ToolCallResponse{
				ID:   "ask_user_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "ask_user",
					Arguments: fmt.Sprintf(`{"question": "%s"}`, question),
				},
			})
		}
	}
	
	return toolCalls, nil
}

// extractBetween extracts text between two delimiters
func extractBetween(text, start, end string) string {
	startIdx := strings.Index(text, start)
	if startIdx == -1 {
		return ""
	}
	startIdx += len(start)
	
	endIdx := strings.Index(text[startIdx:], end)
	if endIdx == -1 {
		return ""
	}
	
	return strings.TrimSpace(text[startIdx : startIdx+endIdx])
}

// executeToolCallWithContext executes a tool call and returns whether it was a context request
func executeToolCallWithContext(toolCall ToolCallResponse, contextHandler func([]ContextRequest, *config.Config) (string, error), cfg *config.Config) (string, bool, error) {
	switch toolCall.Function.Name {
	case "ask_user":
		// Parse arguments
		var args struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return "", false, fmt.Errorf("failed to parse ask_user arguments: %w", err)
		}
		
		// Use the legacy context handler to ask the user
		contextReqs := []ContextRequest{{Type: "user_input", Query: args.Question}}
		result, err := contextHandler(contextReqs, cfg)
		return result, true, err
		
	case "read_file", "run_shell_command":
		// Use the unified tool executor
		typesToolCall := types.ToolCall{
			ID:   toolCall.ID,
			Type: toolCall.Type,
			Function: types.ToolCallFunction{
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			},
		}
		
		result, err := tools.ExecuteToolCall(context.Background(), typesToolCall)
		if err != nil {
			return "", false, err
		}
		
		if !result.Success {
			return "", false, fmt.Errorf("tool execution failed: %v", strings.Join(result.Errors, "; "))
		}
		
		return fmt.Sprintf("%v", result.Output), false, nil
		
	default:
		return "", false, fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

// ContextRequest represents a legacy context request (for compatibility)
type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

// extractContextRequestsFromResponse parses legacy context requests
func extractContextRequestsFromResponse(content string) ([]ContextRequest, error) {
	var response struct {
		ContextRequests []ContextRequest `json:"context_requests"`
	}
	
	// Try to parse as JSON
	if err := json.Unmarshal([]byte(content), &response); err == nil {
		return response.ContextRequests, nil
	}
	
	// Try to extract from code blocks
	if strings.Contains(content, "```json") {
		start := strings.Index(content, "```json") + 7
		end := strings.Index(content[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(content[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &response); err == nil {
				return response.ContextRequests, nil
			}
		}
	}
	
	return []ContextRequest{}, nil
}