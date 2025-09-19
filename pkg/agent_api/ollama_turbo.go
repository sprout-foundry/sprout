package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OllamaTurboClient handles Ollama.com Turbo API requests
type OllamaTurboClient struct {
	*TPSBase
	httpClient *http.Client
	model      string
	apiKey     string
	debug      bool
}

// OllamaTurboModel represents a model available on Ollama Turbo
type OllamaTurboModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OllamaTurboModelsResponse represents the models list response
type OllamaTurboModelsResponse struct {
	Object string             `json:"object"`
	Data   []OllamaTurboModel `json:"data"`
}

// NewOllamaTurboClient creates a new Ollama Turbo client
func NewOllamaTurboClient(model string) (*OllamaTurboClient, error) {
	apiKey := os.Getenv("OLLAMA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OLLAMA_API_KEY environment variable is required for Ollama Turbo")
	}

	timeout := 120 * time.Second // Default: 2 minutes (same as other providers)

	client := &OllamaTurboClient{
		TPSBase: NewTPSBase(),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		model:  model,
		apiKey: apiKey,
		debug:  false,
	}
	return client, nil
}

// ListModels returns available models from Ollama Turbo
func (c *OllamaTurboClient) ListModels() ([]OllamaTurboModel, error) {
	req, err := http.NewRequest("GET", "https://ollama.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama Turbo API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var modelsResp OllamaTurboModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	return modelsResp.Data, nil
}

// SendChatRequest sends a chat request to Ollama Turbo
func (c *OllamaTurboClient) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Always use streaming for better user experience
	return c.SendChatRequestStream(messages, tools, reasoning, nil)
}

// SendChatRequestStream sends a streaming chat request to Ollama Turbo
func (c *OllamaTurboClient) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	req := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	if reasoning != "" {
		req.Reasoning = reasoning
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://ollama.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Track request timing
	startTime := time.Now()

	if c.debug {
		fmt.Printf("DEBUG: Sending request to Ollama Turbo with model %s\n", c.model)
		fmt.Printf("DEBUG: Request body: %s\n", string(jsonData))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama Turbo API error (status %d): %s", resp.StatusCode, string(body))
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

	// Calculate durations
	totalDuration := time.Since(startTime)
	tokenGenDuration := builder.GetTokenGenerationDuration()

	// Use token generation duration for TPS if available, otherwise use total duration
	tpsDuration := tokenGenDuration
	if tpsDuration == 0 {
		tpsDuration = totalDuration
	}

	// Track TPS
	if c.GetTracker() != nil && response.Usage.CompletionTokens > 0 {
		c.GetTracker().RecordRequest(tpsDuration, response.Usage.CompletionTokens)
	}

	// Ollama Turbo is free so set cost to 0
	response.Usage.EstimatedCost = 0.0

	return response, nil
}

// CheckConnection checks if the Ollama Turbo API is accessible
func (c *OllamaTurboClient) CheckConnection() error {
	models, err := c.ListModels()
	if err != nil {
		return fmt.Errorf("failed to connect to Ollama Turbo: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available on Ollama Turbo")
	}

	return nil
}

// SetDebug sets the debug flag
func (c *OllamaTurboClient) SetDebug(debug bool) {
	c.debug = debug
}

// SetModel sets the model
func (c *OllamaTurboClient) SetModel(model string) error {
	c.model = model
	return nil
}

// GetModel returns the current model
func (c *OllamaTurboClient) GetModel() string {
	return c.model
}

// GetProvider returns the provider name
func (c *OllamaTurboClient) GetProvider() string {
	return "ollama-turbo"
}

// GetModelContextLimit returns the context limit for the current model
func (c *OllamaTurboClient) GetModelContextLimit() (int, error) {
	// Turbo models have specific context limits
	switch c.model {
	case "gpt-oss:20b":
		return 128000, nil
	case "gpt-oss:120b":
		return 256000, nil
	case "deepseek-v3.1:671b":
		return 161000, nil
	default:
		return 128000, nil // Default for unknown models
	}
}

// SupportsVision checks if the current model supports vision
func (c *OllamaTurboClient) SupportsVision() bool {
	// Currently, Ollama Turbo models don't support vision
	return false
}

// GetVisionModel returns the vision model for Ollama Turbo
func (c *OllamaTurboClient) GetVisionModel() string {
	// No vision models available yet
	return ""
}

// SendVisionRequest sends a vision-enabled chat request
func (c *OllamaTurboClient) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Fallback to regular chat request
	return c.SendChatRequest(messages, tools, reasoning)
}

// NewOllamaTurboClientWrapper creates an Ollama Turbo client wrapper for backwards compatibility
func NewOllamaTurboClientWrapper(model string) (ClientInterface, error) {
	return NewOllamaTurboClient(model)
}
