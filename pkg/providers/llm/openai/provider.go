package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// Provider implements the OpenAI LLM provider
type Provider struct {
	config     *types.ProviderConfig
	httpClient *http.Client
}

// Factory implements the ProviderFactory interface for OpenAI
type Factory struct{}

// GetName returns the provider name
func (f *Factory) GetName() string {
	return "openai"
}

// Create creates a new OpenAI provider instance
func (f *Factory) Create(config *types.ProviderConfig) (interfaces.LLMProvider, error) {
	if err := f.Validate(config); err != nil {
		return nil, err
	}

	return &Provider{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Second,
		},
	}, nil
}

// Validate validates the OpenAI provider configuration
func (f *Factory) Validate(config *types.ProviderConfig) error {
	if config == nil {
		return fmt.Errorf("configuration is required")
	}

	if config.APIKey == "" {
		return fmt.Errorf("API key is required for OpenAI provider")
	}

	if config.Model == "" {
		return fmt.Errorf("model is required for OpenAI provider")
	}

	// Set defaults
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}

	if config.Timeout == 0 {
		config.Timeout = 60 // 60 seconds default
	}

	return nil
}

// GetName returns the provider name
func (p *Provider) GetName() string {
	return "openai"
}

// GetModels returns available models for OpenAI
func (p *Provider) GetModels(ctx context.Context) ([]types.ModelInfo, error) {
	// For now, return static list. Could be made dynamic by calling OpenAI API
	return []types.ModelInfo{
		{
			Name:           "gpt-4",
			Provider:       "openai",
			MaxTokens:      8192,
			SupportsTools:  true,
			SupportsImages: false,
		},
		{
			Name:           "gpt-4-turbo",
			Provider:       "openai",
			MaxTokens:      128000,
			SupportsTools:  true,
			SupportsImages: true,
		},
		{
			Name:           "gpt-3.5-turbo",
			Provider:       "openai",
			MaxTokens:      16384,
			SupportsTools:  true,
			SupportsImages: false,
		},
	}, nil
}

// GenerateResponse generates a response from OpenAI
func (p *Provider) GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	requestBody, err := p.buildRequest(messages, options)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build request: %w", err)
	}

	startTime := time.Now()
	resp, err := p.makeRequest(ctx, requestBody)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(responseData))
	}

	var apiResponse OpenAIResponse
	if err := json.Unmarshal(responseData, &apiResponse); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices in response")
	}

	content := apiResponse.Choices[0].Message.Content
	usage := types.TokenUsage{
		PromptTokens:     apiResponse.Usage.PromptTokens,
		CompletionTokens: apiResponse.Usage.CompletionTokens,
		TotalTokens:      apiResponse.Usage.TotalTokens,
	}

	metadata := &types.ResponseMetadata{
		Model:      options.Model,
		TokenUsage: usage,
		Duration:   time.Since(startTime),
		Provider:   "openai",
	}

	return content, metadata, nil
}

// GenerateResponseStream generates a streaming response from OpenAI
func (p *Provider) GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error) {
	options.Stream = true

	requestBody, err := p.buildRequest(messages, options)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	startTime := time.Now()
	resp, err := p.makeRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseData, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(responseData))
	}

	// Handle streaming response
	scanner := bufio.NewScanner(resp.Body)
	totalTokens := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				break
			}

			var chunk OpenAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // Skip malformed chunks
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				if _, err := writer.Write([]byte(chunk.Choices[0].Delta.Content)); err != nil {
					return nil, fmt.Errorf("failed to write to stream: %w", err)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading stream: %w", err)
	}

	metadata := &types.ResponseMetadata{
		Model:      options.Model,
		TokenUsage: types.TokenUsage{TotalTokens: totalTokens, Estimated: true},
		Duration:   time.Since(startTime),
		Provider:   "openai",
	}

	return metadata, nil
}

// IsAvailable checks if the provider is available
func (p *Provider) IsAvailable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.BaseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key")
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error: %d", resp.StatusCode)
	}

	return nil
}

// EstimateTokens provides a rough estimate of token count
func (p *Provider) EstimateTokens(messages []types.Message) (int, error) {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content) + len(msg.Role) + 10 // Add some overhead
	}

	// Rough estimate: ~4 characters per token
	return totalChars / 4, nil
}

// CalculateCost calculates the cost for given token usage based on OpenAI pricing
func (p *Provider) CalculateCost(usage types.TokenUsage) float64 {
	// OpenAI pricing (approximate, as of 2024)
	var inputCostPer1K, outputCostPer1K float64

	model := p.config.Model
	if strings.Contains(model, "gpt-4") {
		inputCostPer1K = 0.03  // $0.03 per 1K prompt tokens
		outputCostPer1K = 0.06 // $0.06 per 1K completion tokens
	} else if strings.Contains(model, "gpt-3.5") {
		inputCostPer1K = 0.001  // $0.001 per 1K prompt tokens
		outputCostPer1K = 0.002 // $0.002 per 1K completion tokens
	} else {
		// Default to GPT-4 pricing for unknown models
		inputCostPer1K = 0.03
		outputCostPer1K = 0.06
	}

	inputCost := float64(usage.PromptTokens) * inputCostPer1K / 1000.0
	outputCost := float64(usage.CompletionTokens) * outputCostPer1K / 1000.0

	return inputCost + outputCost
}

// buildRequest builds the OpenAI API request
func (p *Provider) buildRequest(messages []types.Message, options types.RequestOptions) ([]byte, error) {
	model := options.Model
	if model == "" {
		model = p.config.Model
	}

	openaiMessages := make([]OpenAIMessage, len(messages))
	for i, msg := range messages {
		openaiMessages[i] = OpenAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	request := OpenAIRequest{
		Model:    model,
		Messages: openaiMessages,
		Stream:   options.Stream,
	}

	if options.MaxTokens > 0 {
		request.MaxTokens = &options.MaxTokens
	}

	if options.Temperature > 0 {
		request.Temperature = &options.Temperature
	}

	return json.Marshal(request)
}

// makeRequest makes an HTTP request to the OpenAI API
func (p *Provider) makeRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	// Add custom headers if configured
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	return p.httpClient.Do(req)
}

// OpenAI API types
type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []OpenAIStreamChoice `json:"choices"`
}

type OpenAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type OpenAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
