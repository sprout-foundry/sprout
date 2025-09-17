package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	OpenAIURL = "https://api.openai.com/v1/chat/completions"
)

type OpenAIClient struct {
	httpClient *http.Client
	apiKey     string
	model      string
	debug      bool
}

// OpenAI-specific request/response structures
type OpenAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	Reasoning   string    `json:"reasoning,omitempty"` // For reasoning models
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role             string     `json:"role"`
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content,omitempty"`
			ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost,omitempty"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	return &OpenAIClient{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		apiKey: apiKey,
		model:  "gpt-4o-mini", // Default to cost-effective model
		debug:  false,
	}, nil
}

func (c *OpenAIClient) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Calculate appropriate max_tokens based on model and context
	maxTokens := c.calculateMaxTokens(messages, tools)

	req := OpenAIRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	// Only include temperature for models that support it (not GPT-5 models)
	if !strings.Contains(c.model, "gpt-5") {
		temp := 0.1 // Low for consistency
		req.Temperature = &temp
	}

	// Only include max_tokens for models that support it (not GPT-5 or o1 models)
	if !strings.Contains(c.model, "gpt-5") && !strings.Contains(c.model, "o1") {
		req.MaxTokens = maxTokens
	}

	// Only include reasoning parameter for o1 models that support it
	if reasoning != "" && (strings.Contains(c.model, "o1") || strings.Contains(c.model, "reasoning")) {
		req.Reasoning = reasoning
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.debug {
		fmt.Printf("OpenAI Request: %s\n", string(reqBody))
	}

	httpReq, err := http.NewRequest("POST", OpenAIURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if c.debug {
		fmt.Printf("OpenAI Response: %s\n", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp OpenAIResponse
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != nil {
			return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var openaiResp OpenAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Handle API errors in successful HTTP responses
	if openaiResp.Error != nil {
		return nil, fmt.Errorf("OpenAI API error: %s", openaiResp.Error.Message)
	}

	// Convert to agent API format
	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openaiResp.Choices[0]

	// Calculate estimated cost if not provided
	estimatedCost := openaiResp.Usage.EstimatedCost
	if estimatedCost == 0 {
		// Use static OpenAI pricing for accurate cost calculation including cached tokens
		cachedTokens := openaiResp.Usage.PromptTokensDetails.CachedTokens
		estimatedCost = c.calculateOpenAICostWithCaching(openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, cachedTokens)
	}

	response := &ChatResponse{
		ID:      openaiResp.ID,
		Object:  openaiResp.Object,
		Created: openaiResp.Created,
		Model:   openaiResp.Model,
		Choices: []Choice{{
			Index: choice.Index,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				ToolCalls:        choice.Message.ToolCalls,
			},
			FinishReason: choice.FinishReason,
		}},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			TotalTokens:      openaiResp.Usage.TotalTokens,
			EstimatedCost:    estimatedCost,
			PromptTokensDetails: struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			}{
				CachedTokens:     openaiResp.Usage.PromptTokensDetails.CachedTokens,
				CacheWriteTokens: openaiResp.Usage.PromptTokensDetails.CacheWriteTokens,
			},
		},
	}

	return response, nil
}

// calculateMaxTokens calculates appropriate max_tokens based on input size and model limits
func (c *OpenAIClient) calculateMaxTokens(messages []Message, tools []Tool) int {
	// Get model context limit
	contextLimit, err := c.GetModelContextLimit()
	if err != nil || contextLimit == 0 {
		contextLimit = 16000 // Conservative default
	}

	// Rough estimation: 1 token ≈ 4 characters
	inputTokens := 0

	// Estimate tokens from messages
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}

	// Estimate tokens from tools (tools descriptions can be large)
	inputTokens += len(tools) * 200 // Rough estimate per tool

	// Reserve buffer for safety and leave room for response
	maxOutput := contextLimit - inputTokens - 1000 // 1000 token safety buffer

	// Ensure reasonable bounds
	if maxOutput > 4096 {
		maxOutput = 4096 // Cap at 4K for most responses
	} else if maxOutput < 1000 {
		maxOutput = 1000 // Minimum useful response size
	}

	return maxOutput
}

func (c *OpenAIClient) CheckConnection() error {
	// Simple way to check connection - list models endpoint
	req, err := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OpenAI API is not accessible: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid OpenAI API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *OpenAIClient) SetDebug(debug bool) {
	c.debug = debug
}

func (c *OpenAIClient) SetModel(model string) error {
	c.model = model
	return nil
}

func (c *OpenAIClient) GetModel() string {
	return c.model
}

func (c *OpenAIClient) GetProvider() string {
	return "openai"
}

func (c *OpenAIClient) GetModelContextLimit() (int, error) {
	model := c.model

	// Try to get context length from model info API first
	models, err := getOpenAIModels()
	if err == nil {
		for _, modelInfo := range models {
			if modelInfo.ID == model && modelInfo.ContextLength > 0 {
				return modelInfo.ContextLength, nil
			}
		}
	}

	// Fallback to hardcoded limits if API doesn't provide context length (2025 updated)
	switch {
	// GPT-5 series (2025) - up to 272K context
	case strings.Contains(model, "gpt-5"):
		return 272000, nil // GPT-5 supports up to 272K context
	// O3 series (2025) - large context models
	case strings.Contains(model, "o3-mini"):
		return 200000, nil // O3-mini supports ~200K context
	case strings.Contains(model, "o3"):
		return 200000, nil // O3 models support large context
	// O1 series - reasoning models
	case strings.Contains(model, "o1"):
		return 128000, nil // O1 models support 128K context
	// GPT-4o series - multimodal models
	case strings.Contains(model, "gpt-4o"):
		return 128000, nil // GPT-4o supports 128K context
	// GPT-4 series
	case strings.Contains(model, "gpt-4-turbo"):
		return 128000, nil // GPT-4 Turbo supports 128K context
	case strings.Contains(model, "gpt-4"):
		return 8192, nil // Base GPT-4 supports 8K context
	// GPT-3.5 series
	case strings.Contains(model, "gpt-3.5-turbo"):
		return 16385, nil // GPT-3.5-turbo supports ~16K context
	// ChatGPT models
	case strings.Contains(model, "chatgpt"):
		return 128000, nil // ChatGPT models typically support large context
	default:
		return 16000, nil // Conservative default for unknown models
	}
}

// SupportsVision checks if the current model supports vision
func (c *OpenAIClient) SupportsVision() bool {
	visionModel := c.GetVisionModel()
	return visionModel != ""
}

// GetVisionModel returns the vision model for OpenAI
func (c *OpenAIClient) GetVisionModel() string {
	// Return default vision model
	return "gpt-4o"
}

// SendVisionRequest sends a vision-enabled chat request
func (c *OpenAIClient) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
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

// OpenAIPricingTier represents different OpenAI pricing tiers
type OpenAIPricingTier string

const (
	StandardTier OpenAIPricingTier = "standard"
	BatchTier    OpenAIPricingTier = "batch"
	FlexTier     OpenAIPricingTier = "flex"
)

// calculateOpenAICostWithCaching calculates cost using current OpenAI pricing with cache and tier support (September 2025)
func (c *OpenAIClient) calculateOpenAICostWithCaching(promptTokens, completionTokens, cachedTokens int) float64 {
	return c.calculateOpenAICostWithTier(promptTokens, completionTokens, cachedTokens, StandardTier)
}

// calculateOpenAICostWithTier calculates cost with specific pricing tier support
func (c *OpenAIClient) calculateOpenAICostWithTier(promptTokens, completionTokens, cachedTokens int, tier OpenAIPricingTier) float64 {
	// Current OpenAI pricing per 1M tokens (September 2025 - from screenshots)
	pricingMap := map[string]struct {
		InputPer1M       float64 // Standard input price per 1M tokens
		CachedInputPer1M float64 // Cached input price per 1M tokens
		OutputPer1M      float64 // Output price per 1M tokens
		BatchMultiplier  float64 // Batch API discount multiplier (typically 0.5 = 50% off)
		FlexMultiplier   float64 // Flex processing multiplier (typically 0.6 = 40% off)
	}{
		// GPT-5 series (current as of September 2025) - Values are cost per 1M tokens
		"gpt-5":                 {0.625, 0.3125, 5.00, 0.5, 0.6}, // $0.625/$0.3125/$5.00 per 1M
		"gpt-5-2025-08-07":      {0.625, 0.3125, 5.00, 0.5, 0.6},
		"gpt-5-mini":            {0.125, 0.0625, 1.00, 0.5, 0.6}, // $0.125/$0.0625/$1.00 per 1M
		"gpt-5-mini-2025-08-07": {0.125, 0.0625, 1.00, 0.5, 0.6},
		"gpt-5-nano":            {0.025, 0.0125, 0.20, 0.5, 0.6}, // $0.025/$0.0125/$0.20 per 1M
		"gpt-5-nano-2025-08-07": {0.025, 0.0125, 0.20, 0.5, 0.6},

		// O3 series (current pricing) - Values are cost per 1M tokens
		"o3":      {1.00, 0.25, 4.00, 0.5, 0.6},  // $1.00/$0.25/$4.00 per 1M
		"o3-mini": {0.55, 0.138, 2.20, 0.5, 0.6}, // $0.55/$0.138/$2.20 per 1M

		// O4-mini (from screenshot)
		"o4-mini": {0.55, 0.138, 2.20, 0.5, 0.6}, // $0.55/$0.138/$2.20 per 1M

		// GPT-4o series (per-1K pricing converted to per-1M for consistency)
		"gpt-4o":                 {5.0, 2.5, 15.0, 0.5, 0.6}, // $5.00/$2.50/$15.00 per 1M
		"gpt-4o-2024-05-13":      {5.0, 2.5, 15.0, 0.5, 0.6},
		"gpt-4o-2024-08-06":      {2.5, 1.25, 10.0, 0.5, 0.6}, // $2.50/$1.25/$10.00 per 1M
		"gpt-4o-2024-11-20":      {2.5, 1.25, 10.0, 0.5, 0.6},
		"gpt-4o-mini":            {0.15, 0.075, 0.6, 0.5, 0.6}, // $0.15/$0.075/$0.60 per 1M
		"gpt-4o-mini-2024-07-18": {0.15, 0.075, 0.6, 0.5, 0.6},

		// O1 series (from screenshot) - Values are cost per 1M tokens
		"o1":                 {1.00, 0.25, 4.00, 0.5, 0.6}, // $1.00/$0.25/$4.00 per 1M
		"o1-2024-12-17":      {1.00, 0.25, 4.00, 0.5, 0.6},
		"o1-mini":            {0.55, 0.138, 2.20, 0.5, 0.6}, // $0.55/$0.138/$2.20 per 1M
		"o1-mini-2024-09-12": {0.55, 0.138, 2.20, 0.5, 0.6},

		// GPT-4 series (legacy pricing in per-1K format for compatibility)
		"gpt-4-turbo": {10.0, 5.0, 30.0, 0.5, 0.6},
		"gpt-4":       {30.0, 15.0, 60.0, 0.5, 0.6},

		// GPT-3.5 series (legacy pricing)
		"gpt-3.5-turbo": {2.0, 1.0, 2.0, 0.5, 0.6},

		// Image models (from screenshot - per 1M tokens)
		"gpt-image-1":  {10000.0, 2500.0, 40000.0, 0.5, 0.6}, // $10.00/$2.50/$40.00 per 1M
		"gpt-realtime": {5000.0, 400.0, 0.0, 0.5, 0.6},       // $5.00/$0.40 per 1M (no output cost)
	}

	// Look up pricing for the specific model
	if pricing, exists := pricingMap[c.model]; exists {
		// Calculate regular input tokens (excluding cached)
		regularInputTokens := promptTokens - cachedTokens

		// Determine if pricing is per 1M or per 1K tokens based on scale
		var inputRate, cachedRate, outputRate float64
		var divisor float64

		// Most new models use per-1M pricing (values > 50), legacy models use per-1K
		if pricing.InputPer1M > 50 {
			// Per-1M token pricing (new models like GPT-5, O3, O4-mini)
			inputRate = pricing.InputPer1M
			cachedRate = pricing.CachedInputPer1M // Already includes the discount
			outputRate = pricing.OutputPer1M
			divisor = 1000000 // Convert tokens to millions
		} else {
			// Per-1K token pricing (legacy models like GPT-4o, GPT-4)
			inputRate = pricing.InputPer1M
			cachedRate = pricing.CachedInputPer1M // Already includes the discount
			outputRate = pricing.OutputPer1M
			divisor = 1000 // Convert tokens to thousands
		}

		// Apply tier pricing multiplier
		var tierMultiplier float64
		switch tier {
		case BatchTier:
			tierMultiplier = pricing.BatchMultiplier // 50% off for batch
		case FlexTier:
			tierMultiplier = pricing.FlexMultiplier // 40% off for flex
		default:
			tierMultiplier = 1.0 // Standard pricing
		}

		// Calculate costs with tier multiplier applied
		inputCost := float64(regularInputTokens) * inputRate * tierMultiplier / divisor
		cachedCost := float64(cachedTokens) * cachedRate * tierMultiplier / divisor
		outputCost := float64(completionTokens) * outputRate * tierMultiplier / divisor

		return inputCost + cachedCost + outputCost
	}

	// Fallback for unknown models - use GPT-4o-mini pricing as conservative estimate
	regularInputTokens := promptTokens - cachedTokens
	tierMultiplier := 1.0
	if tier == BatchTier {
		tierMultiplier = 0.5
	} else if tier == FlexTier {
		tierMultiplier = 0.6
	}

	fallbackInputCost := float64(regularInputTokens) * 0.15 * tierMultiplier / 1000
	fallbackCachedCost := float64(cachedTokens) * 0.075 * tierMultiplier / 1000 // Pre-discounted cached rate
	fallbackOutputCost := float64(completionTokens) * 0.6 * tierMultiplier / 1000
	return fallbackInputCost + fallbackCachedCost + fallbackOutputCost
}

// CalculateOpenAICostBatch calculates OpenAI API cost using Batch pricing tier (50% discount)
func (c *OpenAIClient) CalculateOpenAICostBatch(promptTokens, completionTokens, cachedTokens int) float64 {
	return c.calculateOpenAICostWithTier(promptTokens, completionTokens, cachedTokens, BatchTier)
}

// CalculateOpenAICostFlex calculates OpenAI API cost using Flex processing tier (40% discount)
func (c *OpenAIClient) CalculateOpenAICostFlex(promptTokens, completionTokens, cachedTokens int) float64 {
	return c.calculateOpenAICostWithTier(promptTokens, completionTokens, cachedTokens, FlexTier)
}

// CalculateOpenAICostStandard calculates OpenAI API cost using Standard pricing tier
func (c *OpenAIClient) CalculateOpenAICostStandard(promptTokens, completionTokens, cachedTokens int) float64 {
	return c.calculateOpenAICostWithTier(promptTokens, completionTokens, cachedTokens, StandardTier)
}

// SendChatRequestStream sends a streaming chat request
func (c *OpenAIClient) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	// Calculate appropriate max_tokens based on model and context
	maxTokens := c.calculateMaxTokens(messages, tools)

	req := OpenAIRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   true, // Enable streaming
	}

	// Only include temperature for models that support it (not GPT-5 models)
	if !strings.Contains(c.model, "gpt-5") {
		temp := 0.1 // Low for consistency
		req.Temperature = &temp
	}

	// Only include max_tokens for models that support it (not GPT-5 or o1 models)
	if !strings.Contains(c.model, "gpt-5") && !strings.Contains(c.model, "o1") {
		req.MaxTokens = maxTokens
	}

	// Only include reasoning parameter for o1 models that support it
	if reasoning != "" && (strings.Contains(c.model, "o1") || strings.Contains(c.model, "reasoning")) {
		req.Reasoning = reasoning
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.debug {
		fmt.Printf("OpenAI Streaming Request: %s\n", string(reqBody))
	}

	httpReq, err := http.NewRequest("POST", OpenAIURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Create response builder
	builder := NewStreamingResponseBuilder(callback)

	// Create SSE reader
	sseReader := NewSSEReader(resp.Body, func(event, data string) error {
		if data == "" {
			return nil
		}

		chunk, err := ParseSSEData(data)
		if err != nil {
			if err == io.EOF {
				// Stream complete
				return nil
			}
			return err
		}

		return builder.ProcessChunk(chunk)
	})

	// Read the stream
	if err := sseReader.Read(); err != nil {
		return nil, fmt.Errorf("failed to read stream: %w", err)
	}

	// Get the final response
	response := builder.GetResponse()

	// Calculate estimated cost if not provided
	if response.Usage.EstimatedCost == 0 && response.Usage.TotalTokens > 0 {
		cachedTokens := response.Usage.PromptTokensDetails.CachedTokens
		response.Usage.EstimatedCost = c.calculateOpenAICostWithCaching(
			response.Usage.PromptTokens,
			response.Usage.CompletionTokens,
			cachedTokens,
		)
	}

	return response, nil
}
