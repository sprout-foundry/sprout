package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	ollama "github.com/ollama/ollama/api"
)

const (
	OllamaURL   = config.DefaultOllamaURL + "/v1/chat/completions"
	OllamaModel = "gpt-oss:20b"
)

type LocalOllamaClient struct {
	httpClient *http.Client
	baseURL    string
	model      string
	debug      bool
}

// Using OpenAI-compatible endpoint, so we reuse existing ChatRequest and ChatResponse structs

func NewOllamaClient() (*LocalOllamaClient, error) {
	return &LocalOllamaClient{
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // Longer timeout for local inference
		},
		baseURL: OllamaURL,
		model:   OllamaModel,
		debug:   false, // Will be set later via SetDebug
	}, nil
}

func (c *LocalOllamaClient) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Use the superior native Ollama client with streaming support
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	// Convert messages to Ollama format
	ollamaMessages := make([]ollama.Message, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollama.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Handle tools by embedding them in the system message if present
	if len(tools) > 0 {
		toolsText := c.formatToolsForPrompt(tools)
		// Add tools as system message at the beginning
		toolMessage := ollama.Message{
			Role:    "system",
			Content: toolsText,
		}
		ollamaMessages = append([]ollama.Message{toolMessage}, ollamaMessages...)
	}

	// The model name for ollama is without the "ollama:" prefix
	actualModelName := strings.TrimPrefix(c.model, "ollama:")

	// Calculate total token count for context sizing
	totalTokens := 0
	for _, msg := range ollamaMessages {
		totalTokens += c.estimateTokens(msg.Content)
	}

	// Set num_ctx to be slightly larger than the total token count
	numCtx := totalTokens + 1000
	if numCtx < 4096 {
		numCtx = 4096 // Minimum context size
	}

	req := &ollama.ChatRequest{
		Model:    actualModelName,
		Messages: ollamaMessages,
		Options: map[string]interface{}{
			"temperature":    0.1,                                  // Very low for consistency
			"top_p":          0.9,                                  // Focus on high-probability tokens
			"num_ctx":        numCtx,                               // Dynamically calculated context size
			"num_predict":    4096,                                 // Limit output length
			"repeat_penalty": 1.1,                                  // Discourage repetition
			"stop":           []string{"\n\n\n", "```\n\n", "END"}, // Stop sequences
			"stream":         false,                                // Disable streaming for chat completion
		},
	}

	// Add reasoning effort if provided
	if reasoning != "" {
		req.Options["reasoning_effort"] = reasoning
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var responseContent strings.Builder
	respFunc := func(res ollama.ChatResponse) error {
		responseContent.WriteString(res.Message.Content)
		return nil
	}

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	// Estimate token usage since Ollama doesn't provide detailed metrics
	estimatedUsage := struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	}{
		PromptTokens:     totalTokens,
		CompletionTokens: c.estimateTokens(responseContent.String()),
		TotalTokens:      totalTokens + c.estimateTokens(responseContent.String()),
		EstimatedCost:    0.0, // Local inference is free
		PromptTokensDetails: struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		}{
			CachedTokens:     0,
			CacheWriteTokens: nil,
		},
	}

	// Build response in agent API format
	response := &ChatResponse{
		ID:      "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   actualModelName,
		Choices: []Choice{{
			Index: 0,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:    "assistant",
				Content: responseContent.String(),
			},
			FinishReason: "stop",
		}},
		Usage: estimatedUsage,
	}

	return response, nil
}

// formatToolsForPrompt formats tools for inclusion in the system message
func (c *LocalOllamaClient) formatToolsForPrompt(tools []Tool) string {
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("You have access to the following tools:\n\n")

	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("Tool: %s\n", tool.Function.Name))
		sb.WriteString(fmt.Sprintf("Description: %s\n", tool.Function.Description))

		if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
			if props, ok := params["properties"].(map[string]interface{}); ok {
				sb.WriteString("Parameters:\n")
				for name, prop := range props {
					if propMap, ok := prop.(map[string]interface{}); ok {
						if desc, ok := propMap["description"].(string); ok {
							sb.WriteString(fmt.Sprintf("  - %s: %s\n", name, desc))
						}
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("To use a tool, respond with a JSON object in this format:\n")
	sb.WriteString(`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "tool_name", "arguments": "{\"param\": \"value\"}"}}]}`)
	sb.WriteString("\n\n")

	return sb.String()
}

// estimateTokens provides a rough token count estimate
func (c *LocalOllamaClient) estimateTokens(text string) int {
	// Rough approximation: 1 token ≈ 4 characters
	return len(text) / 4
}

func (c *LocalOllamaClient) CheckConnection() error {
	// Check if Ollama is running and gpt-oss model is available
	checkURL := config.DefaultOllamaURL + "/api/tags"

	resp, err := c.httpClient.Get(checkURL)
	if err != nil {
		return fmt.Errorf("Ollama is not running. Please start Ollama first")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama API error (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Ollama tags response: %w", err)
	}

	// Check if gpt-oss model is available
	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return fmt.Errorf("failed to parse Ollama tags response: %w", err)
	}

	hasGPTOSS := false
	for _, model := range tagsResp.Models {
		if model.Name == "gpt-oss:20b" || model.Name == "gpt-oss:latest" || model.Name == "gpt-oss" {
			hasGPTOSS = true
			break
		}
	}

	if !hasGPTOSS {
		return fmt.Errorf("gpt-oss:20b model not found. Please run: ollama pull gpt-oss:20b")
	}

	return nil
}

func (c *LocalOllamaClient) SetDebug(debug bool) {
	c.debug = debug
}

func (c *LocalOllamaClient) SetModel(model string) error {
	c.model = model
	return nil
}

func (c *LocalOllamaClient) GetModel() string {
	return c.model
}

func (c *LocalOllamaClient) GetProvider() string {
	return "ollama"
}

func (c *LocalOllamaClient) GetModelContextLimit() (int, error) {
	// For local Ollama models, we use the model name to determine context
	model := c.model

	switch {
	case strings.Contains(model, "gpt-oss"):
		return 120000, nil // GPT-OSS models typically have ~120k context
	default:
		return 32000, nil // Conservative default for other local models
	}
}

// SupportsVision checks if the current model supports vision
func (c *LocalOllamaClient) SupportsVision() bool {
	// Check if we have a vision model available
	visionModel := c.GetVisionModel()
	return visionModel != ""
}

// GetVisionModel returns the vision model for Ollama
func (c *LocalOllamaClient) GetVisionModel() string {
	// Return empty - vision support depends on local models
	return ""
}

// SendVisionRequest sends a vision-enabled chat request
func (c *LocalOllamaClient) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	if !c.SupportsVision() {
		// Fallback to regular chat request if no vision model available
		return c.SendChatRequest(messages, tools, reasoning)
	}

	// Temporarily switch to vision model for this request
	originalModel := c.model
	visionModel := c.GetVisionModel()

	c.model = visionModel

	// Send the vision request
	response, err := c.SendChatRequest(messages, tools, reasoning)

	// Restore original model
	c.model = originalModel

	return response, err
}
