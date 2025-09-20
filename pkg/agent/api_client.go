package agent

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/utils"
)

// APIClient handles all LLM API communication with retry logic
type APIClient struct {
	agent          *Agent
	rateLimiter    *utils.RateLimitBackoff
	maxRetries     int
	baseRetryDelay time.Duration
}

// NewAPIClient creates a new API client
func NewAPIClient(agent *Agent) *APIClient {
	return &APIClient{
		agent:          agent,
		rateLimiter:    utils.NewRateLimitBackoff(),
		maxRetries:     3,
		baseRetryDelay: time.Second,
	}
}

// SendWithRetry sends a request to the LLM with retry logic
func (ac *APIClient) SendWithRetry(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	var resp *api.ChatResponse
	var err error
	retryDelay := ac.baseRetryDelay

	// Reset streaming buffer
	ac.agent.streamingBuffer.Reset()

	for retry := 0; retry <= ac.maxRetries; retry++ {
		if ac.agent.debug {
			fmt.Printf("DEBUG: APIClient attempt %d/%d\n", retry, ac.maxRetries)
		}

		// Send request
		resp, err = ac.sendRequest(messages, tools, reasoning)
		if err == nil {
			break // Success
		}

		if ac.agent.debug {
			fmt.Printf("DEBUG: APIClient error on attempt %d: %v\n", retry, err)
		}

		// Handle error with retry logic
		if !ac.shouldRetry(err, retry) {
			if ac.agent.debug {
				fmt.Printf("DEBUG: APIClient not retrying error: %v\n", err)
			}
			return nil, err
		}

		if ac.agent.debug {
			fmt.Printf("DEBUG: APIClient retrying due to: %v\n", err)
		}

		// Calculate backoff delay
		sleepTime := ac.calculateBackoff(err, retry, retryDelay)
		time.Sleep(sleepTime)
		retryDelay *= 2
	}

	return resp, err
}

// sendRequest sends a single request to the LLM
func (ac *APIClient) sendRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Estimate and store the current request's token count before sending
	ac.agent.currentContextTokens = ac.estimateRequestTokens(messages, tools)

	if ac.agent.streamingEnabled {
		return ac.sendStreamingRequest(messages, tools, reasoning)
	}
	return ac.sendRegularRequest(messages, tools, reasoning)
}

// sendStreamingRequest handles streaming API requests
func (ac *APIClient) sendStreamingRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	streamCallback := func(content string) {
		// Accumulate content
		ac.agent.streamingBuffer.WriteString(content)

		// Call user callback or default output
		if ac.agent.streamingCallback != nil {
			ac.agent.streamingCallback(content)
		} else if ac.agent.outputMutex != nil {
			ac.agent.outputMutex.Lock()
			fmt.Print(content)
			ac.agent.outputMutex.Unlock()
		}
	}

	resp, err := ac.agent.client.SendChatRequestStream(messages, tools, reasoning, streamCallback)

	// Ensure streaming output is flushed
	if ac.agent.outputMutex != nil {
		ac.agent.outputMutex.Lock()
		if err != nil {
			fmt.Print("\r\033[K") // Clear line on error
		}
		os.Stdout.Sync()
		ac.agent.outputMutex.Unlock()
	}

	return resp, err
}

// sendRegularRequest handles non-streaming API requests
func (ac *APIClient) sendRegularRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Special case for OpenAI token tracking
	if ac.agent.GetProvider() == "openai" && ac.agent.currentIteration == 0 {
		ac.showTokenTrackingMessage()
	}

	return ac.agent.client.SendChatRequest(messages, tools, reasoning)
}

// shouldRetry determines if an error is retryable
func (ac *APIClient) shouldRetry(err error, attempt int) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Check for rate limits
	if ac.isRateLimit(errStr) {
		if ac.agent.debug {
			fmt.Printf("DEBUG: shouldRetry - rate limit detected: %v\n", err)
		}
		return ac.handleRateLimit(err, attempt)
	}

	// Check other retryable errors
	isRetryable := ac.isRetryableError(errStr)
	withinMaxRetries := attempt < ac.maxRetries
	result := isRetryable && withinMaxRetries

	if ac.agent.debug {
		fmt.Printf("DEBUG: shouldRetry - error: %v, isRetryable: %v, attempt: %d/%d, result: %v\n",
			err, isRetryable, attempt, ac.maxRetries, result)
	}

	return result
}

// isRateLimit checks if error is a rate limit
func (ac *APIClient) isRateLimit(errStr string) bool {
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "usage limit")
}

// handleRateLimit handles rate limit errors with proper backoff
func (ac *APIClient) handleRateLimit(err error, attempt int) bool {
	// Log the rate limit
	ac.rateLimiter.LogRateLimit(ac.agent.GetProvider(), ac.agent.GetModel(),
		ac.agent.totalTokens, err, nil)

	// Check if we should retry
	if !ac.rateLimiter.ShouldRetry(attempt) {
		return false
	}

	// Calculate and wait for backoff
	backoffDelay := ac.rateLimiter.CalculateBackoffDelay(nil, attempt)

	// Show progress to user
	if ac.agent.outputMutex != nil {
		ac.agent.outputMutex.Lock()
		ac.rateLimiter.WaitWithProgress(backoffDelay, ac.agent.GetProvider())
		ac.agent.outputMutex.Unlock()
	} else {
		ac.rateLimiter.WaitWithProgress(backoffDelay, ac.agent.GetProvider())
	}

	return true
}

// isRetryableError checks if an error should be retried
func (ac *APIClient) isRetryableError(errStr string) bool {
	// Never retry 502 errors - these are server-side issues
	if strings.Contains(errStr, "502") || strings.Contains(errStr, "upstream error") {
		return false
	}

	return strings.Contains(errStr, "stream error") ||
		strings.Contains(errStr, "INTERNAL_ERROR") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "timeout")
}

// calculateBackoff calculates the backoff delay
func (ac *APIClient) calculateBackoff(err error, attempt int, baseDelay time.Duration) time.Duration {
	// For rate limits, this is handled separately
	if ac.isRateLimit(err.Error()) {
		return 0 // Already handled
	}

	// Exponential backoff with jitter
	jitter := time.Duration(rand.Float64() * float64(baseDelay/2))
	return baseDelay + jitter
}

// showTokenTrackingMessage shows OpenAI token tracking message
func (ac *APIClient) showTokenTrackingMessage() {
	message := "📊 Using non-streaming mode for accurate token tracking...\n\n"
	if ac.agent.outputMutex != nil {
		ac.agent.outputMutex.Lock()
		fmt.Print(message)
		ac.agent.outputMutex.Unlock()
	} else {
		fmt.Print(message)
	}
}

// estimateRequestTokens estimates the token count for the current request
func (ac *APIClient) estimateRequestTokens(messages []api.Message, tools []api.Tool) int {
	tokenEstimate := 0

	// Estimate tokens for messages (rough approximation: 1 token ≈ 4 characters)
	for _, msg := range messages {
		tokenEstimate += len(msg.Content) / 4
		if msg.ReasoningContent != "" {
			tokenEstimate += len(msg.ReasoningContent) / 4
		}
	}

	// Estimate tokens for tools (JSON serialization overhead + descriptions)
	for _, tool := range tools {
		// Tool name and description
		tokenEstimate += len(tool.Function.Name) / 4
		tokenEstimate += len(tool.Function.Description) / 4

		// Parameters are typically JSON schema - estimate ~200 tokens per tool
		tokenEstimate += 200
	}

	// Add some overhead for API formatting
	tokenEstimate += 100

	return tokenEstimate
}
