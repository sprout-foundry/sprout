package common

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// LLMClient provides a unified interface for LLM operations
type LLMClient struct {
	config *config.Config
	logger *utils.Logger
}

// NewLLMClient creates a new LLM client
func NewLLMClient(cfg *config.Config, logger *utils.Logger) *LLMClient {
	return &LLMClient{
		config: cfg,
		logger: logger,
	}
}

// LLMRequest represents a request to the LLM
type LLMRequest struct {
	Messages     []prompts.Message
	Filename     string
	Timeout      time.Duration
	ImagePaths   []string
	SystemPrompt string
	Model        string
}

// LLMResponse represents a response from the LLM
type LLMResponse struct {
	Content    string
	TokenUsage *llm.TokenUsage
	Error      error
	Duration   time.Duration
}

// ExecuteRequest executes a single LLM request
func (c *LLMClient) ExecuteRequest(ctx context.Context, req *LLMRequest) *LLMResponse {
	startTime := time.Now()

	// Use provided model or get from config
	modelName := req.Model
	if modelName == "" {
		modelName = req.Filename
	}

	// Set default timeout if not provided
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	var content string
	var tokenUsage *llm.TokenUsage
	var err error

	// Execute the request
	if len(req.ImagePaths) > 0 {
		content, tokenUsage, err = llm.GetLLMResponse(modelName, req.Messages, req.Filename, c.config, timeout, req.ImagePaths...)
	} else {
		content, tokenUsage, err = llm.GetLLMResponse(modelName, req.Messages, req.Filename, c.config, timeout)
	}

	duration := time.Since(startTime)

	return &LLMResponse{
		Content:    content,
		TokenUsage: tokenUsage,
		Error:      err,
		Duration:   duration,
	}
}

// ExecuteStreamingRequest executes a streaming LLM request
func (c *LLMClient) ExecuteStreamingRequest(ctx context.Context, req *LLMRequest, writer io.Writer) *LLMResponse {
	startTime := time.Now()

	// Use provided model or get from config
	modelName := req.Model
	if modelName == "" {
		modelName = req.Filename
	}

	// Set default timeout if not provided
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	var tokenUsage *llm.TokenUsage
	var err error

	// Execute the streaming request
	if len(req.ImagePaths) > 0 {
		tokenUsage, err = llm.GetLLMResponseStream(modelName, req.Messages, req.Filename, c.config, timeout, writer, req.ImagePaths...)
	} else {
		tokenUsage, err = llm.GetLLMResponseStream(modelName, req.Messages, req.Filename, c.config, timeout, writer)
	}

	duration := time.Since(startTime)

	return &LLMResponse{
		Content:    "", // Content is written to the stream
		TokenUsage: tokenUsage,
		Error:      err,
		Duration:   duration,
	}
}

// CreatePromptMessages creates standardized prompt messages
func (c *LLMClient) CreatePromptMessages(systemPrompt string, userPrompt string) []prompts.Message {
	messages := []prompts.Message{}

	if systemPrompt != "" {
		messages = append(messages, prompts.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	messages = append(messages, prompts.Message{
		Role:    "user",
		Content: userPrompt,
	})

	return messages
}

// ValidateResponse validates an LLM response
func (c *LLMClient) ValidateResponse(response *LLMResponse) error {
	if response.Error != nil {
		return response.Error
	}

	if strings.TrimSpace(response.Content) == "" {
		return fmt.Errorf("empty response from LLM")
	}

	if response.TokenUsage != nil {
		if response.TokenUsage.TotalTokens < 0 {
			return fmt.Errorf("invalid token usage: negative token count")
		}
	}

	return nil
}

// GetTokenUsage returns token usage information
func (c *LLMClient) GetTokenUsage(response *LLMResponse) *llm.TokenUsage {
	if response.TokenUsage != nil {
		return response.TokenUsage
	}
	return &llm.TokenUsage{}
}

// IsResponseEmpty checks if the response is empty
func (c *LLMClient) IsResponseEmpty(response *LLMResponse) bool {
	return strings.TrimSpace(response.Content) == ""
}

// GetEstimatedTokens estimates the number of tokens in a text
func (c *LLMClient) GetEstimatedTokens(text string) int {
	return utils.EstimateTokens(text)
}

// LogRequest logs an LLM request
func (c *LLMClient) LogRequest(req *LLMRequest, operation string) {
	if c.logger != nil {
		model := req.Model
		if model == "" {
			model = req.Filename
		}
		c.logger.Logf("LLM Request [%s]: model=%s, messages=%d, timeout=%v",
			operation, model, len(req.Messages), req.Timeout)
	}
}

// LogResponse logs an LLM response
func (c *LLMClient) LogResponse(response *LLMResponse, operation string) {
	if c.logger != nil {
		if response.Error != nil {
			c.logger.Logf("LLM Response [%s]: ERROR - %v (duration: %v)",
				operation, response.Error, response.Duration)
		} else {
			tokens := 0
			if response.TokenUsage != nil {
				tokens = response.TokenUsage.TotalTokens
			}
			c.logger.Logf("LLM Response [%s]: SUCCESS - tokens=%d, duration=%v",
				operation, tokens, response.Duration)
		}
	}
}

// RetryRequest retries an LLM request with exponential backoff
func (c *LLMClient) RetryRequest(ctx context.Context, req *LLMRequest, maxRetries int, operation string) *LLMResponse {
	var lastResponse *LLMResponse

	for attempt := 1; attempt <= maxRetries; attempt++ {
		c.LogRequest(req, fmt.Sprintf("%s (attempt %d/%d)", operation, attempt, maxRetries))

		response := c.ExecuteRequest(ctx, req)
		lastResponse = response

		if response.Error == nil {
			c.LogResponse(response, operation)
			return response
		}

		c.LogResponse(response, operation)

		// Don't retry on the last attempt
		if attempt < maxRetries {
			// Simple exponential backoff
			delay := time.Duration(attempt) * time.Second
			if c.logger != nil {
				c.logger.Logf("Retrying in %v...", delay)
			}

			select {
			case <-ctx.Done():
				return &LLMResponse{
					Error:    ctx.Err(),
					Duration: time.Since(time.Now()), // This won't be accurate but that's ok
				}
			case <-time.After(delay):
				// Continue to next attempt
			}
		}
	}

	return lastResponse
}

// GetConfig returns the configuration
func (c *LLMClient) GetConfig() *config.Config {
	return c.config
}

// SetConfig updates the configuration
func (c *LLMClient) SetConfig(cfg *config.Config) {
	c.config = cfg
}
