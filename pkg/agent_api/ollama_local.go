package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	ollama "github.com/ollama/ollama/api"
)

type ollamaClient interface {
	List(ctx context.Context) (*ollama.ListResponse, error)
	Chat(ctx context.Context, req *ollama.ChatRequest, fn ollama.ChatResponseFunc) error
}

type ollamaClientFactory func() (ollamaClient, error)

// OllamaLocalClient handles local Ollama API requests
type OllamaLocalClient struct {
	*TPSBase
	model         string
	debug         bool
	clientFactory ollamaClientFactory
}

func defaultOllamaClientFactory() (ollamaClient, error) {
	return ollama.ClientFromEnvironment()
}

func ensureModelAvailable(ctx context.Context, client ollamaClient, model string) error {
	listResp, err := client.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list local models: %w", err)
	}

	availableModels := make([]string, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		availableModels = append(availableModels, m.Name)
		if m.Name == model {
			return nil
		}
	}

	return fmt.Errorf("model %s not found locally. Available models: %v", model, availableModels)
}

func newOllamaLocalClientWithFactory(model string, factory ollamaClientFactory) (*OllamaLocalClient, error) {
	if factory == nil {
		factory = defaultOllamaClientFactory
	}

	// Verify Ollama is running locally
	client, err := factory()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	// Get list of available models first
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list local models: %w", err)
	}

	// If no models specified or empty, use first available model
	if strings.TrimSpace(model) == "" {
		if len(listResp.Models) == 0 {
			return nil, fmt.Errorf("no models available locally. Please pull a model first using 'ollama pull <model>'")
		}
		model = listResp.Models[0].Name
	} else {
		// Check if requested model exists locally
		availableModels := make([]string, 0, len(listResp.Models))
		for _, m := range listResp.Models {
			availableModels = append(availableModels, m.Name)
			if m.Name == model {
				// Model found, proceed with creation
				return &OllamaLocalClient{
					TPSBase:       NewTPSBase(),
					model:         model,
					debug:         false,
					clientFactory: factory,
				}, nil
			}
		}

		// Model not found, fallback to first available model
		if len(listResp.Models) > 0 {
			fmt.Fprintf(os.Stderr, "⚠️  Model '%s' not found locally. Available models: %v\n", model, availableModels)
			fmt.Fprintf(os.Stderr, "🔄 Falling back to first available model: %s\n", listResp.Models[0].Name)
			model = listResp.Models[0].Name
		} else {
			return nil, fmt.Errorf("model %s not found locally and no other models available. Available models: %v", model, availableModels)
		}
	}

	return &OllamaLocalClient{
		TPSBase:       NewTPSBase(),
		model:         model,
		debug:         false,
		clientFactory: factory,
	}, nil
}

// NewOllamaLocalClient creates a new local Ollama client
func NewOllamaLocalClient(model string) (*OllamaLocalClient, error) {
	return newOllamaLocalClientWithFactory(model, nil)
}

func (c *OllamaLocalClient) newClient() (ollamaClient, error) {
	if c.clientFactory == nil {
		c.clientFactory = defaultOllamaClientFactory
	}
	return c.clientFactory()
}

func (c *OllamaLocalClient) buildChatRequest(messages []Message, tools []Tool, reasoning string, stream bool) (*ollama.ChatRequest, int) {
	ollamaMessages := make([]ollama.Message, 0, len(messages)+1)
	ollamaTools := convertToolsToOllamaTools(tools)

	// Optional: fold system content into first user message for templates that ignore system role
	if os.Getenv("LEDIT_OLLAMA_FOLD_SYSTEM") != "" {
		var systemParts []string
		injected := false
		for _, m := range messages {
			role := strings.ToLower(m.Role)
			if role == "system" {
				if t := strings.TrimSpace(m.Content); t != "" {
					systemParts = append(systemParts, t)
				}
				continue
			}
			if !injected && role == "user" && len(systemParts) > 0 {
				combined := "System:\n" + strings.Join(systemParts, "\n\n") + "\n\n" + m.Content
				ollamaMessages = append(ollamaMessages, ollama.Message{Role: m.Role, Content: combined})
				injected = true
				continue
			}
			ollamaMessages = append(ollamaMessages, ollama.Message{Role: m.Role, Content: m.Content})
		}
		if len(ollamaMessages) == 0 { // no user message existed
			for _, m := range messages {
				if strings.ToLower(m.Role) != "system" {
					ollamaMessages = append(ollamaMessages, ollama.Message{Role: m.Role, Content: m.Content})
				}
			}
		}
	} else {
		for _, msg := range messages {
			ollamaMessages = append(ollamaMessages, ollama.Message{Role: msg.Role, Content: msg.Content})
		}
	}

	totalTokens := 0
	for _, msg := range ollamaMessages {
		totalTokens += c.estimateTokens(msg.Content)
	}

	// Derive conservative context/prediction sizing based on model limit
	contextLimit, _ := c.GetModelContextLimit()
	if contextLimit <= 0 {
		contextLimit = 32000
	}
	// Keep headroom for generation but do not exceed limit
	headroom := 4096
	if headroom > contextLimit/2 {
		headroom = contextLimit / 2
	}
	numCtx := totalTokens + headroom
	if numCtx > contextLimit {
		numCtx = contextLimit
	}
	if numCtx < totalTokens+1024 {
		numCtx = totalTokens + 1024
		if numCtx > contextLimit {
			numCtx = contextLimit
		}
	}

	// Compute safe prediction cap
	numPredict := contextLimit - totalTokens - 512
	if numPredict < 512 {
		numPredict = 512
	}
	if numPredict > 4096 {
		numPredict = 4096
	}

	// Keep sampling options minimal to align with provider defaults
	options := map[string]any{
		"num_ctx":     numCtx,
		"num_predict": numPredict,
		"stream":      stream,
	}

	if reasoning != "" {
		options["reasoning_effort"] = reasoning
	}

	req := &ollama.ChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Options:  options,
	}

	if len(ollamaTools) > 0 {
		req.Tools = ollamaTools
	}
	req.Stream = &stream

	return req, totalTokens
}

// SendChatRequest sends a chat request to local Ollama
func (c *OllamaLocalClient) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, false)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var responseContent strings.Builder
	var toolCalls []ToolCall
	var lastDoneReason string
	var lastMetrics ollama.Metrics
	respFunc := ollama.ChatResponseFunc(func(res ollama.ChatResponse) error {
		if len(res.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, convertOllamaToolCalls(res.Message.ToolCalls)...)
		} else if trimmed := strings.TrimSpace(res.Message.Content); trimmed != "" {
			responseContent.WriteString(res.Message.Content)
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics

		return nil
	})

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

	finishReason := lastDoneReason
	if finishReason == "" {
		finishReason = "stop"
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
			FinishReason: finishReason,
		}},
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := c.estimateTokens(responseContent.String())
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if len(toolCalls) > 0 {
		response.Choices[0].Message.ToolCalls = toolCalls
	}

	// Track TPS
	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(duration, completionTokens)
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
	client, err := c.newClient()
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
	if strings.Contains(c.model, "qwen3-coder") || strings.Contains(c.model, "gpt-oss") {
		return 128000, nil
	}
	return 32000, nil
}

// SetModel updates the active model after validating it exists locally
func (c *OllamaLocalClient) SetModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model name cannot be empty")
	}

	if model == c.model {
		return nil
	}

	client, err := c.newClient()
	if err != nil {
		return fmt.Errorf("could not create ollama client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get list of available models
	listResp, err := client.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list local models: %w", err)
	}

	// Check if requested model exists locally
	availableModels := make([]string, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		availableModels = append(availableModels, m.Name)
		if m.Name == model {
			// Model found, proceed with switch
			c.model = model
			if c.debug {
				fmt.Printf("DEBUG: Switched local Ollama model to: %s\n", model)
			}
			return nil
		}
	}

	// Model not found, fallback to first available model
	if len(listResp.Models) > 0 {
		fmt.Fprintf(os.Stderr, "⚠️  Model '%s' not found locally. Available models: %v\n", model, availableModels)
		fmt.Fprintf(os.Stderr, "🔄 Falling back to first available model: %s\n", listResp.Models[0].Name)
		c.model = listResp.Models[0].Name
		if c.debug {
			fmt.Printf("DEBUG: Switched local Ollama model to fallback: %s\n", c.model)
		}
		return nil
	}

	return fmt.Errorf("model %s not found locally and no other models available. Available models: %v", model, availableModels)
}

// ListModels returns available local models
func (c *OllamaLocalClient) ListModels() ([]ModelInfo, error) {
	client, err := c.newClient()
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

// SendChatRequestStream streams responses from local Ollama as they arrive
func (c *OllamaLocalClient) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, true)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	builder := NewStreamingResponseBuilder(callback)
	var lastMetrics ollama.Metrics
	var lastDoneReason string

	startTime := time.Now()

	if c.debug {
		fmt.Printf("DEBUG: Streaming local Ollama with model: %s\n", c.model)
	}

	err = client.Chat(ctx, req, func(res ollama.ChatResponse) error {
		chunk := convertOllamaResponseToStreamingChunk(res)
		if err := builder.ProcessChunk(chunk); err != nil {
			return err
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	response := builder.GetResponse()
	if response == nil {
		response = &ChatResponse{}
	}

	if response.ID == "" {
		response.ID = "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if response.Object == "" {
		response.Object = "chat.completion"
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	response.Model = c.model

	if len(response.Choices) == 0 {
		response.Choices = []Choice{{}}
	}

	choice := &response.Choices[0]
	if choice.Message.Role == "" {
		choice.Message.Role = "assistant"
	}
	if choice.FinishReason == "" {
		if lastDoneReason != "" {
			choice.FinishReason = lastDoneReason
		} else {
			choice.FinishReason = "stop"
		}
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := c.estimateTokens(choice.Message.Content)
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(time.Since(startTime), completionTokens)
	}

	return response, nil
}

// estimateTokens provides a rough token count estimate
func (c *OllamaLocalClient) estimateTokens(text string) int {
	// Rough approximation: 1 token ≈ 4 characters
	return len(text) / 4
}

func convertToolsToOllamaTools(tools []Tool) ollama.Tools {
	if len(tools) == 0 {
		return nil
	}

	result := make(ollama.Tools, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Type) == "" {
			continue
		}

		ollamaTool := ollama.Tool{
			Type: tool.Type,
			Function: ollama.ToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
			},
		}

		params := ollama.ToolFunctionParameters{Type: "object", Properties: make(map[string]ollama.ToolProperty)}
		if tool.Function.Parameters != nil {
			if raw, err := json.Marshal(tool.Function.Parameters); err == nil {
				if err := json.Unmarshal(raw, &params); err != nil {
					params = ollama.ToolFunctionParameters{Type: "object", Properties: make(map[string]ollama.ToolProperty)}
				}
			}
		}

		if params.Type == "" {
			params.Type = "object"
		}

		ollamaTool.Function.Parameters = params
		result = append(result, ollamaTool)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func convertOllamaResponseToStreamingChunk(res ollama.ChatResponse) *StreamingChatResponse {
	chunk := &StreamingChatResponse{
		ID:    res.Model,
		Model: res.Model,
	}

	if !res.CreatedAt.IsZero() {
		chunk.Created = res.CreatedAt.Unix()
	}

	delta := StreamingDelta{Role: res.Message.Role}

	if len(res.Message.ToolCalls) == 0 {
		trimmed := strings.TrimSpace(res.Message.Content)
		if trimmed != "" {
			delta.Content = res.Message.Content
		}
	}

	if len(res.Message.ToolCalls) > 0 {
		delta.ToolCalls = make([]StreamingToolCall, 0, len(res.Message.ToolCalls))
		for _, call := range res.Message.ToolCalls {
			var arguments string
			if call.Function.Arguments != nil {
				if encoded, err := json.Marshal(call.Function.Arguments); err == nil {
					arguments = string(encoded)
				} else {
					arguments = fmt.Sprintf("%v", call.Function.Arguments)
				}
			}

			delta.ToolCalls = append(delta.ToolCalls, StreamingToolCall{
				Index: call.Function.Index,
				Function: &StreamingToolCallFunction{
					Name:      call.Function.Name,
					Arguments: arguments,
				},
			})
		}
	}

	choice := StreamingChoice{
		Index: 0,
		Delta: delta,
	}

	if res.DoneReason != "" {
		reason := res.DoneReason
		choice.FinishReason = &reason
	} else if res.Done {
		reason := "stop"
		choice.FinishReason = &reason
	}

	chunk.Choices = []StreamingChoice{choice}
	return chunk
}

func convertOllamaToolCalls(calls []ollama.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}

	result := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		var arguments string
		if call.Function.Arguments != nil {
			if encoded, err := json.Marshal(call.Function.Arguments); err == nil {
				arguments = string(encoded)
			} else {
				arguments = fmt.Sprintf("%v", call.Function.Arguments)
			}
		}

		toolCall := ToolCall{Type: "function"}
		toolCall.Function.Name = call.Function.Name
		toolCall.Function.Arguments = arguments
		result = append(result, toolCall)
	}

	return result
}
