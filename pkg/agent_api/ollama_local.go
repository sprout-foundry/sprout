package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	ollama "github.com/ollama/ollama/api"
)

// OllamaLocalClient handles local Ollama API requests
type OllamaLocalClient struct {
	*TPSBase
	model string
	debug bool
}

// NewOllamaLocalClient creates a new local Ollama client
func NewOllamaLocalClient(model string) (*OllamaLocalClient, error) {
	// Verify Ollama is running locally
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	// Check if model exists locally
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list local models: %w", err)
	}

	modelFound := false
	for _, m := range listResp.Models {
		if m.Name == model {
			modelFound = true
			break
		}
	}

	if !modelFound {
		availableModels := make([]string, 0, len(listResp.Models))
		for _, m := range listResp.Models {
			availableModels = append(availableModels, m.Name)
		}
		return nil, fmt.Errorf("model %s not found locally. Available models: %v", model, availableModels)
	}

	return &OllamaLocalClient{
		TPSBase: NewTPSBase(),
		model:   model,
		debug:   false,
	}, nil
}

// SendChatRequest sends a chat request to local Ollama
func (c *OllamaLocalClient) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
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
		toolsText := formatToolsForPrompt(tools)
		toolMessage := ollama.Message{
			Role:    "system",
			Content: toolsText,
		}
		ollamaMessages = append([]ollama.Message{toolMessage}, ollamaMessages...)
	}

	// Calculate total token count for context sizing
	totalTokens := 0
	for _, msg := range ollamaMessages {
		totalTokens += c.estimateTokens(msg.Content)
	}

	numCtx := totalTokens + 1000
	if numCtx < 4096 {
		numCtx = 4096
	}

	req := &ollama.ChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Options: map[string]interface{}{
			"temperature":    0.1,
			"top_p":          0.9,
			"num_ctx":        numCtx,
			"num_predict":    4096,
			"repeat_penalty": 1.1,
			"stop":           []string{"\n\n\n", "```\n\n", "END"},
			"stream":         false,
		},
	}

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

	// Track request timing
	startTime := time.Now()

	if c.debug {
		fmt.Printf("DEBUG: Calling local Ollama with model: %s\n", c.model)
	}

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	// Calculate request duration
	duration := time.Since(startTime)

	// Build response
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
	}

	response := &ChatResponse{
		ID:      "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   c.model,
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

	// Track TPS
	if c.GetTracker() != nil && estimatedUsage.CompletionTokens > 0 {
		c.GetTracker().RecordRequest(duration, estimatedUsage.CompletionTokens)
	}

	return response, nil
}

// SetDebug enables or disables debug mode
func (c *OllamaLocalClient) SetDebug(debug bool) {
	c.debug = debug
}

// GetModel returns the current model
func (c *OllamaLocalClient) GetModel() string {
	return c.model
}

// GetProvider returns the provider name
func (c *OllamaLocalClient) GetProvider() string {
	return "ollama-local"
}

// CheckConnection verifies local Ollama is accessible
func (c *OllamaLocalClient) CheckConnection() error {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("could not create ollama client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.List(ctx)
	return err
}

// GetModelContextLimit returns the context limit for the model
func (c *OllamaLocalClient) GetModelContextLimit() (int, error) {
	// Most Ollama models support 4K-32K context
	// This is a conservative default
	return 8192, nil
}

// SetModel sets the model (not supported for local client after initialization)
func (c *OllamaLocalClient) SetModel(model string) error {
	return fmt.Errorf("cannot change model after initialization for local Ollama client")
}

// ListModels returns available local models
func (c *OllamaLocalClient) ListModels() ([]ModelInfo, error) {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list local models: %w", err)
	}

	models := make([]ModelInfo, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		models = append(models, ModelInfo{
			ID:       m.Name,
			Provider: "ollama-local",
		})
	}

	return models, nil
}

// SupportsVision returns false as local Ollama doesn't support vision through this interface
func (c *OllamaLocalClient) SupportsVision() bool {
	return false
}

// GetVisionModel returns empty string as vision is not supported
func (c *OllamaLocalClient) GetVisionModel() string {
	return ""
}

// SendVisionRequest returns an error as vision is not supported
func (c *OllamaLocalClient) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	return nil, fmt.Errorf("vision requests are not supported by local Ollama through this interface")
}

// SendChatRequestStream is not implemented for local Ollama
func (c *OllamaLocalClient) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	// TODO: Implement streaming support using Ollama's native streaming
	return nil, fmt.Errorf("streaming is not yet implemented for local Ollama")
}

// estimateTokens provides a rough token count estimate
func (c *OllamaLocalClient) estimateTokens(text string) int {
	// Rough approximation: 1 token ≈ 4 characters
	return len(text) / 4
}

// formatToolsForPrompt formats tools as a text prompt
func formatToolsForPrompt(tools []Tool) string {
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("You have access to the following tools:\n\n")

	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("Tool: %s\n", tool.Function.Name))
		sb.WriteString(fmt.Sprintf("Description: %s\n", tool.Function.Description))
		// You could add parameter descriptions here if needed
		sb.WriteString("\n")
	}

	sb.WriteString("To use a tool, respond with a JSON object in this format:\n")
	sb.WriteString(`{"tool_call": {"name": "tool_name", "arguments": {...}}}`)
	sb.WriteString("\n\n")

	return sb.String()
}
