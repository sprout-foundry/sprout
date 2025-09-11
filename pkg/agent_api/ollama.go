package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	OllamaURL   = "http://localhost:11434/v1/chat/completions"
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
	// Convert to ENHANCED harmony format
	var formatter *HarmonyFormatter
	if reasoning != "" {
		formatter = NewHarmonyFormatterWithReasoning(reasoning)
	} else {
		formatter = NewHarmonyFormatter()
	}
	
	// Configure harmony options
	opts := &HarmonyOptions{
		ReasoningLevel: reasoning,
		EnableAnalysis: true,
	}
	if opts.ReasoningLevel == "" {
		opts.ReasoningLevel = "high"
	}
	
	harmonyText := formatter.FormatMessagesForCompletion(messages, tools, opts)

	// Create a single message with harmony-formatted text
	req := map[string]interface{}{
		"model":      c.model,
		"messages":   []Message{{Role: "user", Content: harmonyText}},
		"max_tokens": 30000,
		// Note: Don't include tools in harmony format - they're embedded in the text
	}

	// Add reasoning effort if provided (Ollama uses reasoning_effort, not reasoning)
	if reasoning != "" {
		req["reasoning_effort"] = reasoning
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Log the request for debugging
	if c.debug {
		log.Printf("Ollama Request URL: %s", c.baseURL)
		log.Printf("Ollama Request Headers: %v", httpReq.Header)
		log.Printf("Ollama Request Body: %s", string(reqBody))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Log the response for debugging
	respBody, _ := io.ReadAll(resp.Body)
	if c.debug {
		log.Printf("Ollama Response Status: %s", resp.Status)
		log.Printf("Ollama Response Headers: %v", resp.Header)
		log.Printf("Ollama Response Body: %s", string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Set cost to 0 for local inference
	chatResp.Usage.EstimatedCost = 0.0

	// Strip return token from GPT-OSS model responses
	for i, choice := range chatResp.Choices {
		chatResp.Choices[i].Message.Content = formatter.StripReturnToken(choice.Message.Content)
	}

	return &chatResp, nil
}

func (c *LocalOllamaClient) CheckConnection() error {
	// Check if Ollama is running and gpt-oss model is available
	checkURL := "http://localhost:11434/api/tags"

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
		return 32000, nil  // Conservative default for other local models
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
	return GetVisionModelForProvider(OllamaClientType)
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
