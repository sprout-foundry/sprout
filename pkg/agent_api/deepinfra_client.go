package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DeepInfraURL = "https://api.deepinfra.com/v1/openai/chat/completions"
	DefaultModel = "deepseek-ai/DeepSeek-V3.1"

	// Model types for different use cases
	AgentModel = "deepseek-ai/DeepSeek-V3.1"                 // Primary agent model
	CoderModel = "qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" // Coding-specific model
	FastModel  = "google/gemini-2.5-flash"                   // Fast, model for commits and simple tasks (DeepInfra default)

	// Local models (all use the same model for local inference)
	LocalModel = "gpt-oss:20b"
)

// IsGPTOSSModel checks if a model uses the GPT-OSS family and requires harmony syntax
func IsGPTOSSModel(model string) bool {
	return strings.HasPrefix(model, "openai/gpt-oss")
}

// Types moved to types.go

type Client struct {
	httpClient *http.Client
	apiToken   string
	debug      bool
	model      string

	// TPS tracking using the proper tracker
	tpsTracker *TPSTracker
}

func NewClient() (*Client, error) {
	return NewClientWithModel(DefaultModel)
}

func NewClientWithModel(model string) (*Client, error) {
	token := os.Getenv("DEEPINFRA_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}

	// Use default model if none specified
	if model == "" {
		model = DefaultModel
	}

	// Get timeout from environment variable or use default
	timeout := 120 * time.Second // Default: 2 minutes (reduced from 5)
	if timeoutEnv := os.Getenv("LEDIT_API_TIMEOUT"); timeoutEnv != "" {
		if duration, err := time.ParseDuration(timeoutEnv); err == nil {
			timeout = duration
		} else {
			// Try parsing as seconds if duration parsing fails
			var seconds int
			if _, err := fmt.Sscanf(timeoutEnv, "%d", &seconds); err == nil && seconds > 0 {
				timeout = time.Duration(seconds) * time.Second
			}
		}
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		apiToken:   token,
		debug:      false, // Will be set later via SetDebug
		model:      model,
		tpsTracker: NewTPSTracker(),
	}, nil
}

func (c *Client) SendChatRequest(req ChatRequest) (*ChatResponse, error) {
	var finalReq ChatRequest

	// Use harmony format only for GPT-OSS models
	if IsGPTOSSModel(req.Model) {
		// Convert to ENHANCED harmony format
		var formatter *HarmonyFormatter
		if req.Reasoning != "" {
			formatter = NewHarmonyFormatterWithReasoning(req.Reasoning)
		} else {
			formatter = NewHarmonyFormatter()
		}

		// Configure harmony options based on request
		opts := &HarmonyOptions{
			ReasoningLevel: req.Reasoning,
			EnableAnalysis: false, // Disable analysis channel to reduce excessive reasoning
		}
		if opts.ReasoningLevel == "" {
			opts.ReasoningLevel = "medium" // Reduced from "high" to "medium"
		}

		harmonyText := formatter.FormatMessagesForCompletion(req.Messages, req.Tools, opts)

		// Create a single message with harmony-formatted text
		finalReq = ChatRequest{
			Model:     req.Model,
			Messages:  []Message{{Role: "user", Content: harmonyText}},
			MaxTokens: req.MaxTokens,
			Reasoning: req.Reasoning,
			// Note: Don't include Tools in harmony format - they're embedded in the text
		}
	} else {
		// Use standard OpenAI format for other models
		finalReq = req
	}

	reqBody, err := json.Marshal(finalReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Implement retry logic with smarter timeout detection
	maxRetries := 2 // Reduced from 3 since we have shorter timeout
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		httpReq, err := http.NewRequest("POST", DeepInfraURL, bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

		// Log the request for debugging
		if c.debug && attempt == 0 {
			log.Printf("DeepInfra Request URL: %s", DeepInfraURL)
			log.Printf("DeepInfra Request Headers: %v", httpReq.Header)
			log.Printf("DeepInfra Request Body: %s", string(reqBody))
		}

		// Track request timing
		start := time.Now()
		resp, err := c.httpClient.Do(httpReq)
		duration := time.Since(start)

		if err != nil {
			lastErr = err
			errStr := err.Error()

			// Check if this is a timeout
			isTimeout := strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "deadline exceeded") ||
				strings.Contains(errStr, "Client.Timeout exceeded")

			if isTimeout {
				// Log timing information
				if c.debug {
					log.Printf("⏱️ Request timeout after %v (attempt %d/%d, timeout setting: %v)",
						duration, attempt+1, maxRetries+1, c.httpClient.Timeout)
				}

				// For timeouts, only retry if we haven't already tried
				if attempt < maxRetries {
					// Wait a bit before retrying (shorter wait for timeouts)
					time.Sleep(time.Duration(attempt+1) * time.Second)
					continue
				}
			}

			// For non-timeout errors, check if retryable
			isRetryable := strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "EOF") ||
				strings.Contains(errStr, "broken pipe")

			if isRetryable && attempt < maxRetries {
				// Exponential backoff
				time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
				continue
			}

			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		// Log the response for debugging
		respBody, _ := io.ReadAll(resp.Body)
		if c.debug {
			log.Printf("DeepInfra Response Status: %s (duration: %v)", resp.Status, duration)
			log.Printf("DeepInfra Response Headers: %v", resp.Header)
			log.Printf("DeepInfra Response Body: %s", string(respBody))
		}

		// Handle rate limiting with retry
		if resp.StatusCode == 429 && attempt < maxRetries {
			// Check for Retry-After header
			retryAfter := resp.Header.Get("Retry-After")
			waitTime := time.Duration(5+attempt*5) * time.Second // Default: 5s, 10s, 15s

			if retryAfter != "" {
				if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil {
					waitTime = seconds
				}
			}

			if c.debug {
				log.Printf("⏳ Rate limited, waiting %v before retry (attempt %d/%d)", waitTime, attempt+1, maxRetries+1)
			}

			time.Sleep(waitTime)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
		}

		var chatResp ChatResponse
		if err := json.Unmarshal(respBody, &chatResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Post-process harmony responses
		if IsGPTOSSModel(req.Model) {
			formatter := NewHarmonyFormatter()
			// Strip return token from responses before returning to agent
			for i, choice := range chatResp.Choices {
				chatResp.Choices[i].Message.Content = formatter.StripReturnToken(choice.Message.Content)
			}
		}

		// Calculate tokens per second for this request
		if chatResp.Usage.CompletionTokens > 0 && duration.Seconds() > 0 {
			// Use the TPSTracker
			if c.tpsTracker != nil {
				tps := c.tpsTracker.RecordRequest(duration, chatResp.Usage.CompletionTokens)
				if c.debug {
					log.Printf("📊 TPS: %.1f tokens/s (completion: %d tokens, duration: %v)", tps, chatResp.Usage.CompletionTokens, duration)
				}
			}
		}

		// Success!
		if c.debug && attempt > 0 {
			log.Printf("✅ Request succeeded after %d retries", attempt)
		}

		return &chatResp, nil
	}

	// All retries exhausted
	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}

func (c *Client) GetModel() string {
	return c.model
}

// GetLastTPS returns the most recent TPS measurement
func (c *Client) GetLastTPS() float64 {
	if c.tpsTracker != nil {
		return c.tpsTracker.GetCurrentTPS()
	}
	return 0.0
}

// GetAverageTPS returns the average TPS across all requests
func (c *Client) GetAverageTPS() float64 {
	if c.tpsTracker != nil {
		return c.tpsTracker.GetAverageTPS()
	}
	return 0.0
}

// GetTPSStats returns comprehensive TPS statistics
func (c *Client) GetTPSStats() map[string]float64 {
	if c.tpsTracker != nil {
		stats := c.tpsTracker.GetStats()
		// Convert to float64 map for interface compatibility
		result := make(map[string]float64)
		for k, v := range stats {
			if f, ok := v.(float64); ok {
				result[k] = f
			}
		}
		return result
	}
	return map[string]float64{}
}

// ResetTPSStats clears all TPS tracking data
func (c *Client) ResetTPSStats() {
	if c.tpsTracker != nil {
		c.tpsTracker.Reset()
	}
}

// SendChatRequestStream sends a streaming chat request and processes chunks via callback
func (c *Client) SendChatRequestStream(req ChatRequest, callback StreamCallback) (*ChatResponse, error) {
	// Enable streaming
	req.Stream = true

	var finalReq ChatRequest

	// Use harmony format only for GPT-OSS models
	if IsGPTOSSModel(req.Model) {
		// Convert to ENHANCED harmony format
		var formatter *HarmonyFormatter
		if req.Reasoning != "" {
			formatter = NewHarmonyFormatterWithReasoning(req.Reasoning)
		} else {
			formatter = NewHarmonyFormatter()
		}

		// Configure harmony options based on request
		opts := &HarmonyOptions{
			ReasoningLevel: req.Reasoning,
			EnableAnalysis: false,
		}
		if opts.ReasoningLevel == "" {
			opts.ReasoningLevel = "medium"
		}

		harmonyText := formatter.FormatMessagesForCompletion(req.Messages, req.Tools, opts)

		finalReq = ChatRequest{
			Model:     req.Model,
			Messages:  []Message{{Role: "user", Content: harmonyText}},
			MaxTokens: req.MaxTokens,
			Reasoning: req.Reasoning,
			Stream:    true,
		}
	} else {
		finalReq = req
	}

	reqBody, err := json.Marshal(finalReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", DeepInfraURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)
	httpReq.Header.Set("Accept", "text/event-stream") // Important for SSE

	// Log the request for debugging
	if c.debug {
		log.Printf("DeepInfra Streaming Request URL: %s", DeepInfraURL)
		log.Printf("DeepInfra Streaming Request Headers: %v", httpReq.Header)
		log.Printf("DeepInfra Streaming Request Body: %s", string(reqBody))
	}

	// Track request timing
	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	if c.debug {
		log.Printf("DeepInfra Streaming Response Status: %s (initial response time: %v)", resp.Status, duration)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Create response builder
	builder := NewStreamingResponseBuilder(callback)

	// Create SSE reader
	sseReader := NewSSEReader(resp.Body, func(event, data string) error {
		// Parse SSE data
		chunk, err := ParseSSEData(data)
		if err != nil {
			if err == io.EOF {
				// Stream complete
				return err
			}
			// Log parse errors but continue
			if c.debug {
				log.Printf("Failed to parse SSE chunk: %v", err)
			}
			return nil
		}

		// Process chunk
		if err := builder.ProcessChunk(chunk); err != nil {
			return fmt.Errorf("failed to process chunk: %w", err)
		}

		return nil
	})

	// Read the stream
	if err := sseReader.Read(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read SSE stream: %w", err)
	}

	// Get the final response
	response := builder.GetResponse()

	// Post-process harmony responses
	if IsGPTOSSModel(req.Model) {
		formatter := NewHarmonyFormatter()
		// Strip return token from responses before returning to agent
		for i, choice := range response.Choices {
			response.Choices[i].Message.Content = formatter.StripReturnToken(choice.Message.Content)
		}
	}

	if c.debug {
		log.Printf("✅ Streaming request completed (total time: %v)", time.Since(start))
		log.Printf("📊 Final usage - Tokens: %d, Cost: $%.6f", response.Usage.TotalTokens, response.Usage.EstimatedCost)
	}

	return response, nil
}
