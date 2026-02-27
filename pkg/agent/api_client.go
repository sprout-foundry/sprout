package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/logging"
	"github.com/alantheprice/ledit/pkg/utils"
)

// LogAPIResponse saves the accumulated streaming response to .ledit/lastResponse.json
func LogAPIResponse(content string, streaming bool) {
	// Create the response structure
	response := map[string]interface{}{
		"content":   content,
		"streaming": streaming,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		// If we can't marshal, create a simple text representation
		jsonData = []byte(fmt.Sprintf("Failed to marshal response: %v\nContent length: %d\nStreaming: %v\nTimestamp: %s",
			err, len(content), streaming, time.Now().Format(time.RFC3339)))
	}

	// Ensure .ledit directory exists
	leditDir := filepath.Join(os.Getenv("HOME"), ".ledit")
	if err := os.MkdirAll(leditDir, 0755); err != nil {
		// If we can't create the directory, we can't log
		return
	}

	// Write to lastResponse.json
	filePath := filepath.Join(leditDir, "lastResponse.json")
	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		// If we can't write the file, we can't log
		return
	}
	logging.WriteLocalCopy("lastResponse.json", jsonData)
}

func logChatResponseDetailed(resp *api.ChatResponse, provider string, streaming bool, iteration int) {
	if os.Getenv("LEDIT_LOG_API_RESPONSES") == "" || resp == nil {
		return
	}

	payload := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"provider":  provider,
		"model":     resp.Model,
		"streaming": streaming,
		"iteration": iteration,
		"response":  resp,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Join(os.Getenv("HOME"), ".ledit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	filename := fmt.Sprintf("api_response_%s.json", time.Now().Format("20060102_150405.000000000"))
	_ = os.WriteFile(filepath.Join(dir, filename), data, 0o644)
	logging.WriteLocalCopy(filename, data)
}

// APIClient handles all LLM API communication with retry logic
type APIClient struct {
	agent             *Agent
	rateLimiter       *utils.RateLimitBackoff
	maxRetries        int
	baseRetryDelay    time.Duration
	connectionTimeout time.Duration // Time to establish connection
	firstChunkTimeout time.Duration // Time to receive first response chunk
	chunkTimeout      time.Duration // Max time between chunks in streaming
	overallTimeout    time.Duration // Total request timeout
}

// RateLimitExceededError indicates repeated rate limit failures even after retries
type RateLimitExceededError struct {
	Attempts  int
	LastError error
}

func (e *RateLimitExceededError) Error() string {
	if e.LastError == nil {
		return fmt.Sprintf("rate limit exceeded after %d attempt(s)", e.Attempts)
	}
	return fmt.Sprintf("rate limit exceeded after %d attempt(s): %v", e.Attempts, e.LastError)
}

func (e *RateLimitExceededError) Unwrap() error {
	return e.LastError
}

// NewAPIClient creates a new API client
func NewAPIClient(agent *Agent) *APIClient {
	client := &APIClient{
		agent:          agent,
		rateLimiter:    utils.NewRateLimitBackoff(),
		maxRetries:     3,
		baseRetryDelay: time.Second,
	}

	client.rateLimiter.SetOutputFunc(client.printRateLimitMessage)

	// Set timeouts from configuration or defaults
	client.setTimeoutsFromConfig()

	return client
}

// setTimeoutsFromConfig applies timeout settings from configuration
func (ac *APIClient) setTimeoutsFromConfig() {
	// Default timeout values (apply to all providers)
	// Increased to handle large file writes and slow token generation
	connectionTimeoutSec := 300 // 5 minutes to establish connection
	firstChunkTimeoutSec := 600 // 10 minutes for first response (was 300)
	chunkTimeoutSec := 600      // 10 minutes between chunks (was 300)
	overallTimeoutSec := 1800   // 30 minutes total (was 600)

	// Get timeout config if available
	if config := ac.agent.GetConfig(); config != nil && config.APITimeouts != nil {
		if config.APITimeouts.ConnectionTimeoutSec > 0 {
			connectionTimeoutSec = config.APITimeouts.ConnectionTimeoutSec
		}
		if config.APITimeouts.FirstChunkTimeoutSec > 0 {
			firstChunkTimeoutSec = config.APITimeouts.FirstChunkTimeoutSec
		}
		if config.APITimeouts.ChunkTimeoutSec > 0 {
			chunkTimeoutSec = config.APITimeouts.ChunkTimeoutSec
		}
		if config.APITimeouts.OverallTimeoutSec > 0 {
			overallTimeoutSec = config.APITimeouts.OverallTimeoutSec
		}
	}

	// Convert to time.Duration
	ac.connectionTimeout = time.Duration(connectionTimeoutSec) * time.Second
	ac.firstChunkTimeout = time.Duration(firstChunkTimeoutSec) * time.Second
	ac.chunkTimeout = time.Duration(chunkTimeoutSec) * time.Second
	ac.overallTimeout = time.Duration(overallTimeoutSec) * time.Second

	// Provider-specific adjustments
	if ac.agent != nil && strings.EqualFold(ac.agent.GetProvider(), "lmstudio") {
		// LM Studio: 5 minute timeout for all operations
		ac.connectionTimeout = 300 * time.Second
		ac.firstChunkTimeout = 300 * time.Second
		ac.chunkTimeout = 300 * time.Second
	}

	if ac.agent != nil && strings.EqualFold(ac.agent.GetProvider(), "ollama") {
		// Ollama: 5 minute timeout for all operations
		ac.connectionTimeout = 300 * time.Second
		ac.firstChunkTimeout = 300 * time.Second
		ac.chunkTimeout = 300 * time.Second
	}

	if ac.agent != nil && strings.EqualFold(ac.agent.GetProvider(), "openai") {
		// OpenAI: 3 minute timeout for all operations
		ac.connectionTimeout = 180 * time.Second
		ac.firstChunkTimeout = 180 * time.Second
		ac.chunkTimeout = 180 * time.Second
	}

	if ac.agent != nil && strings.EqualFold(ac.agent.GetProvider(), "zai") {
		// ZAI: Use timeouts that accommodate file operations and long-running tasks
		// File writes/edits can take time to process and generate chunks
		// Match the provider's configured streaming timeout of 320 seconds
		ac.connectionTimeout = 120 * time.Second // 2 minutes to establish connection
		ac.firstChunkTimeout = 320 * time.Second // 320 seconds for first chunk (matches provider config)
		ac.chunkTimeout = 320 * time.Second      // 320 seconds between chunks (matches provider config)
	}

	if ac.agent != nil && strings.EqualFold(ac.agent.GetProvider(), "openrouter") {
		// OpenRouter: Use extended timeouts for high-latency models
		// Some models routed through OpenRouter can have significant latency
		// especially after tool execution when processing large tool results
		ac.connectionTimeout = 120 * time.Second // 2 minutes to establish connection
		ac.firstChunkTimeout = 600 * time.Second // 10 minutes for first chunk
		ac.chunkTimeout = 300 * time.Second      // 5 minutes between chunks
		ac.overallTimeout = 900 * time.Second    // 15 minutes total (allows for tool execution + final response)
	}

	if ac.agent.debug {
		ac.agent.debugLog("DEBUG: API Timeouts - Connection: %v, First Chunk: %v, Chunk: %v, Overall: %v\n",
			ac.connectionTimeout, ac.firstChunkTimeout, ac.chunkTimeout, ac.overallTimeout)
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
			ac.agent.debugLog("DEBUG: APIClient attempt %d/%d\n", retry, ac.maxRetries)
		}

		// Send request with diagnostic timing
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient starting sendRequest at %s\n", time.Now().Format("15:04:05.000"))
		}
		resp, err = ac.sendRequest(messages, tools, reasoning)
		if ac.agent.debug {
			if err == nil {
				ac.agent.debugLog("DEBUG: APIClient completed sendRequest successfully at %s\n", time.Now().Format("15:04:05.000"))
			} else {
				ac.agent.debugLog("DEBUG: APIClient sendRequest failed at %s with error: %v\n", time.Now().Format("15:04:05.000"), err)
			}
		}
		if err == nil {
			// Track metrics from successful API response
			if resp != nil {
				ac.agent.TrackMetricsFromResponse(
					resp.Usage.PromptTokens,
					resp.Usage.CompletionTokens,
					resp.Usage.TotalTokens,
					resp.Usage.EstimatedCost,
					resp.Usage.PromptTokensDetails.CachedTokens,
				)
			}
			break // Success
		}

		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient error on attempt %d: %v\n", retry, err)
		}

		// Handle error with retry logic
		if !ac.shouldRetry(err, retry) {
			if ac.agent.debug {
				ac.agent.debugLog("DEBUG: APIClient not retrying error: %v\n", err)
			}
			if ac.isRateLimit(err.Error()) {
				return nil, &RateLimitExceededError{Attempts: retry + 1, LastError: err}
			}
			return nil, err
		}

		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient retrying due to: %v\n", err)
		}

		// Calculate backoff delay
		sleepTime := ac.calculateBackoff(err, retry, retryDelay)
		time.Sleep(sleepTime)
		retryDelay *= 2
	}

	if err != nil && ac.isRateLimit(err.Error()) {
		return nil, &RateLimitExceededError{Attempts: ac.maxRetries + 1, LastError: err}
	}

	return resp, err
}

// sendRequest sends a single request to the LLM
func (ac *APIClient) sendRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Estimate and store the current request's token count before sending
	ac.agent.currentContextTokens = ac.estimateRequestTokens(messages, tools)

	// Optional context breakdown diagnostic
	if os.Getenv("LEDIT_CONTEXT_DIAG") != "" {
		ac.printContextBreakdown(messages, tools)
	}

	if ac.agent.streamingEnabled {
		return ac.sendStreamingRequest(messages, tools, reasoning)
	}
	return ac.sendRegularRequest(messages, tools, reasoning)
}

// printContextBreakdown logs a per-message breakdown to help diagnose large first-turn context
func (ac *APIClient) printContextBreakdown(messages []api.Message, tools []api.Tool) {
	if ac.agent == nil || !ac.agent.debug {
		return
	}
	totalChars := 0
	totalMsgTokens := 0
	ac.agent.debugLog("\n🔎 Context Breakdown (messages=%d, tools=%d)\n", len(messages), len(tools))
	for i, m := range messages {
		chars := len(m.Content) + len(m.ReasoningContent)
		tokens := chars / 4 // rough estimate
		totalChars += chars
		totalMsgTokens += tokens
		// Detect likely base context JSON by simple heuristic
		tag := ""
		c := strings.TrimSpace(m.Content)
		if m.Role == "system" && strings.HasPrefix(c, "{") && strings.Contains(c, "\"repo_root\"") && strings.Contains(c, "\"files\"") {
			tag = " [base-context]"
		}
		// Preview first 160 chars single-line
		preview := c
		if len(preview) > 160 {
			preview = preview[:160] + "…"
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		ac.agent.debugLog("  %2d) role=%s chars=%d est_tokens=%d%s | %s\n", i, m.Role, chars, tokens, tag, preview)
	}
	// Tools estimate mirroring estimateRequestTokens
	toolTokens := 0
	for _, t := range tools {
		toolTokens += len(t.Function.Name) / 4
		toolTokens += len(t.Function.Description) / 4
		toolTokens += 200
	}
	ac.agent.debugLog("  Messages: chars=%d est_tokens=%d\n", totalChars, totalMsgTokens)
	ac.agent.debugLog("  Tools: count=%d est_tokens~%d\n", len(tools), toolTokens)
	ac.agent.debugLog("  Total est_tokens=%d (what footer will display as prompt)\n\n", totalMsgTokens+toolTokens+100)
}

// sendStreamingRequest handles streaming API requests with timeouts
func (ac *APIClient) sendStreamingRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Create context with overall timeout
	ctx, cancel := context.WithTimeout(context.Background(), ac.overallTimeout)
	defer cancel()

	// Channel to receive the result
	resultChan := make(chan struct {
		resp *api.ChatResponse
		err  error
	}, 1)

	// Track streaming activity for timeout detection
	chunkReceived := make(chan bool, 10) // Buffer to prevent blocking

	// Enhanced callback with timeout tracking
	streamCallback := func(content string) {
		// Notify that we received a chunk
		select {
		case chunkReceived <- true:
		default:
			// Channel full, that's ok - we just want to signal activity
		}

		// Accumulate content (sanitize to prevent ANSI contamination)
		if ac.agent.debug {
			// Debug: Log raw content being written to streaming buffer
			if strings.Contains(content, "\x1b[") || strings.Contains(content, "\x1b(") {
				ac.agent.debugLog("🚨 ANSI DETECTED in streaming content: %q\n", content)
			}
		}
		// Sanitize content before storing in streaming buffer
		sanitizedContent := sanitizeStreamingContent(content)
		if sanitizedContent != content && ac.agent.debug {
			ac.agent.debugLog("🧹 Sanitized streaming content, removed ANSI codes\n")
		}
		ac.agent.streamingBuffer.WriteString(sanitizedContent)

		// Publish stream chunk for real-time UI updates
		ac.agent.PublishStreamChunk(sanitizedContent)

		// Call user callback or default output
		if ac.agent.streamingCallback != nil {
			ac.agent.streamingCallback(content)
		} else if ac.agent.outputMutex != nil {
			ac.agent.outputMutex.Lock()
			fmt.Print(content)
			ac.agent.outputMutex.Unlock()
		}
	}

	// Start the API call in a goroutine
	go func() {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient calling client.SendChatRequestStream at %s\n", time.Now().Format("15:04:05.000"))
		}
		resp, err := ac.agent.client.SendChatRequestStream(messages, tools, reasoning, streamCallback)
		if ac.agent.debug {
			if err == nil {
				ac.agent.debugLog("DEBUG: APIClient client.SendChatRequestStream completed at %s\n", time.Now().Format("15:04:05.000"))
			} else {
				ac.agent.debugLog("DEBUG: APIClient client.SendChatRequestStream failed at %s: %v\n", time.Now().Format("15:04:05.000"), err)
			}
		}

		resultChan <- struct {
			resp *api.ChatResponse
			err  error
		}{resp, err}
	}()

	// Monitor for timeouts and completion
	firstChunkTimer := time.NewTimer(ac.firstChunkTimeout)
	chunkTimer := time.NewTimer(ac.chunkTimeout)
	defer firstChunkTimer.Stop()
	defer chunkTimer.Stop()

	firstChunkReceived := false

	for {
		select {
		case <-ac.agent.interruptCtx.Done():
			return nil, fmt.Errorf("request interrupted by user")

		case <-ctx.Done():
			ac.displayTimeoutError("Request timed out", ac.overallTimeout)
			return nil, fmt.Errorf("API request timed out after %v", ac.overallTimeout)

		case <-firstChunkTimer.C:
			if !firstChunkReceived {
				ac.displayTimeoutError("No response from API", ac.firstChunkTimeout)
				return nil, fmt.Errorf("no response received within %v", ac.firstChunkTimeout)
			}

		case <-chunkTimer.C:
			ac.displayTimeoutError("API stopped responding", ac.chunkTimeout)
			return nil, fmt.Errorf("no data received for %v", ac.chunkTimeout)

		case <-chunkReceived:
			if !firstChunkReceived {
				firstChunkReceived = true
				firstChunkTimer.Stop()
			}
			// Track activity for debugging if needed
			// Reset chunk timeout
			chunkTimer.Reset(ac.chunkTimeout)

		case result := <-resultChan:
			// Ensure streaming output is flushed
			if ac.agent.outputMutex != nil {
				ac.agent.outputMutex.Lock()
				if result.err != nil {
					fmt.Print("\r\033[K") // Clear line on error
				}
				os.Stdout.Sync()
				ac.agent.outputMutex.Unlock()
			}

			// Log the accumulated streaming response for debugging
			if ac.agent.streamingEnabled {
				LogAPIResponse(ac.agent.streamingBuffer.String(), true)
				logChatResponseDetailed(result.resp, ac.agent.client.GetProvider(), true, ac.agent.currentIteration)
			}

			if result.err != nil {
				if !ac.isRateLimit(result.err.Error()) {
					ac.displayAPIError(result.err)
				}
				return result.resp, result.err
			}

			// Note: Tool execution and response processing is handled by the main conversation handler
			// The streaming handler only manages the streaming output and timeout

			return result.resp, result.err
		}
	}
}

// sendRegularRequest handles non-streaming API requests with timeout
func (ac *APIClient) sendRegularRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Special case for OpenAI token tracking
	if ac.agent.GetProvider() == "openai" && ac.agent.currentIteration == 0 {
		ac.showTokenTrackingMessage()
	}

	// Create context with timeout (use overall timeout for regular requests)
	ctx, cancel := context.WithTimeout(context.Background(), ac.overallTimeout)
	defer cancel()

	// Channel to receive the result
	resultChan := make(chan struct {
		resp *api.ChatResponse
		err  error
	}, 1)

	// Start the API call in a goroutine
	go func() {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient calling client.SendChatRequest at %s\n", time.Now().Format("15:04:05.000"))
		}
		resp, err := ac.agent.client.SendChatRequest(messages, tools, reasoning)
		if ac.agent.debug {
			if err == nil {
				ac.agent.debugLog("DEBUG: APIClient client.SendChatRequest completed at %s\n", time.Now().Format("15:04:05.000"))
			} else {
				ac.agent.debugLog("DEBUG: APIClient client.SendChatRequest failed at %s: %v\n", time.Now().Format("15:04:05.000"), err)
			}
		}

		resultChan <- struct {
			resp *api.ChatResponse
			err  error
		}{resp, err}
	}()

	// Wait for result or timeout
	select {
	case <-ac.agent.interruptCtx.Done():
		return nil, fmt.Errorf("request interrupted by user")

	case <-ctx.Done():
		ac.displayTimeoutError("Request timed out", ac.overallTimeout)
		return nil, fmt.Errorf("API request timed out after %v", ac.overallTimeout)

	case result := <-resultChan:
		logChatResponseDetailed(result.resp, ac.agent.client.GetProvider(), false, ac.agent.currentIteration)
		if result.err != nil {
			if !ac.isRateLimit(result.err.Error()) {
				ac.displayAPIError(result.err)
			}
		}
		return result.resp, result.err
	}
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
			ac.agent.debugLog("DEBUG: shouldRetry - rate limit detected: %v\n", err)
		}
		return ac.handleRateLimit(err, attempt)
	}

	// Check other retryable errors
	isRetryable := ac.isRetryableError(errStr)
	withinMaxRetries := attempt < ac.maxRetries
	result := isRetryable && withinMaxRetries

	if ac.agent.debug {
		ac.agent.debugLog("DEBUG: shouldRetry - error: %v, isRetryable: %v, attempt: %d/%d, result: %v\n",
			err, isRetryable, attempt, ac.maxRetries, result)
	}

	return result
}

// isRateLimit checks if error is a real rate limit (more precise detection)
func (ac *APIClient) isRateLimit(errStr string) bool {
	if ac.rateLimiter == nil {
		return false
	}

	return ac.rateLimiter.IsRateLimitError(errors.New(errStr), nil)
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
	ac.rateLimiter.WaitWithProgress(backoffDelay, ac.agent.GetProvider())

	return true
}

func (ac *APIClient) printRateLimitMessage(msg string) {
	if ac.agent != nil {
		ac.agent.PrintLineAsync(msg)
		return
	}
	fmt.Print(msg)
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
	message := "📊 Using non-streaming mode for accurate token tracking..."
	ac.agent.PrintLine(message)
	ac.agent.PrintLine("")
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

// displayTimeoutError shows a user-friendly timeout error
func (ac *APIClient) displayTimeoutError(message string, timeout time.Duration) {
	errorMsg := fmt.Sprintf("🔌 TIMEOUT ERROR: %s (waited %v)\n\nThis usually means the API is taking too long to respond.\nPossible causes:\n- High model latency (common with large models or complex queries)\n- Network issues\n- API overloaded\n\nThe request has been aborted. Try:\n- Using a faster model\n- Reducing query complexity\n- Checking your network connection", message, timeout)
	// Route through agent so interactive console places it in content area
	ac.agent.PrintLine(errorMsg)
}

// displayAPIError shows a user-friendly API error
func (ac *APIClient) displayAPIError(err error) {
	providerName := ac.agent.GetProvider()
	errorMsg := fmt.Sprintf("🚨 %s API Error: %v", strings.Title(providerName), err)
	// Route through agent so interactive console places it in content area
	ac.agent.PrintLine(errorMsg)
}

// sanitizeStreamingContent removes ANSI escape sequences from streaming content
func sanitizeStreamingContent(content string) string {
	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[mGKHJABCD]`)
	cleaned := ansiRegex.ReplaceAllString(content, "")

	// Remove other potential ANSI sequences
	ansiRegex2 := regexp.MustCompile(`\x1b\([0-9;]*[AB]`)
	cleaned = ansiRegex2.ReplaceAllString(cleaned, "")

	// Remove any remaining escape characters
	cleaned = strings.ReplaceAll(cleaned, "\x1b", "")

	return cleaned
}
