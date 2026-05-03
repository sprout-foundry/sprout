package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/logging"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// LogAPIResponse saves the accumulated streaming response to .sprout/lastResponse.json
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

	// Ensure .sprout directory exists
	sproutDir := filepath.Join(os.Getenv("HOME"), ".sprout")
	if err := os.MkdirAll(sproutDir, 0755); err != nil {
		// If we can't create the directory, we can't log
		return
	}

	// Write to lastResponse.json
	filePath := filepath.Join(sproutDir, "lastResponse.json")
	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		// If we can't write the file, we can't log
		return
	}
	logging.WriteLocalCopy("lastResponse.json", jsonData)
}

func logChatResponseDetailed(resp *api.ChatResponse, provider string, streaming bool, iteration int) {
	if configuration.GetEnvSimple("LOG_API_RESPONSES") == "" || resp == nil {
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

	dir := filepath.Join(os.Getenv("HOME"), ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	filename := fmt.Sprintf("api_response_%s.json", time.Now().Format("20060102_150405.000000000"))
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		log.Printf("[debug] failed to write API response dump %s: %v", filename, err)
	}
	logging.WriteLocalCopy(filename, data)
}

// APIClient handles all LLM API communication with retry logic
type APIClient struct {
	agent                   *Agent
	rateLimiter             *utils.RateLimitBackoff
	maxRetries              int
	baseRetryDelay          time.Duration
	connectionTimeout       time.Duration                        // Time to establish connection
	firstChunkTimeout       time.Duration                        // Time to receive first response chunk
	chunkTimeout            time.Duration                        // Max time between chunks in streaming
	overallTimeout          time.Duration                        // Total request timeout
	prepareMessagesCallback func(tools []api.Tool) []api.Message // Callback to re-prepare messages after compaction
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
	// Determine if thinking should be disabled
	disableThinking := false
	if ac.agent != nil {
		disableThinking = ac.agent.shouldDisableThinking()
	}

	var resp *api.ChatResponse
	var err error
	retryDelay := ac.baseRetryDelay

	// Reset streaming buffer
	ac.agent.output.GetStreamingBuffer().Reset()
	ac.agent.output.GetReasoningBuffer().Reset()

	for retry := 0; retry <= ac.maxRetries; retry++ {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient attempt %d/%d\n", retry, ac.maxRetries)
		}

		// Send request with diagnostic timing
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient starting sendRequest at %s\n", time.Now().Format("15:04:05.000"))
		}
		resp, err = ac.sendRequest(messages, tools, reasoning, disableThinking)
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
				promptTokens, completionTokens, totalTokens, estimatedCost, cachedTokens, estimatedUsage :=
					ac.deriveUsageMetrics(resp, messages, tools)
				ac.agent.TrackMetricsFromResponse(
					promptTokens,
					completionTokens,
					totalTokens,
					estimatedCost,
					cachedTokens,
				)
				if estimatedUsage {
					ac.agent.MarkEstimatedTokenUsageResponse()
				}
			}
			break // Success
		}

		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient error on attempt %d: %v\n", retry, err)
		}

		// Check for context limit error - trigger compaction and re-prepare messages
		if ac.isContextLimitError(err) {
			current := ac.extractContextLimitTokenPair(err)
			if current.prompt > 0 && current.limit > 0 {
				ac.agent.PrintLineAsync(fmt.Sprintf("[~] Request exceeds model context window (%d/%d tokens). Compacting conversation and retrying...", current.prompt, current.limit))
			} else {
				ac.agent.PrintLineAsync("[~] Request exceeds model context window. Compacting conversation and retrying...")
			}

			if ac.agent.debug {
				ac.agent.debugLog("DEBUG: context limit error detected, triggering compaction\n")
			}
			compacted := ac.agent.TriggerCompaction()
			if !compacted && ac.prepareMessagesCallback == nil {
				return nil, agenterrors.NewContextError("context window exceeded and no compaction strategy was available", err)
			}
			// Re-prepare messages after compaction
			if ac.prepareMessagesCallback != nil {
				messages = ac.prepareMessagesCallback(tools)
			}
			// Continue to retry with new compacted messages
			continue
		}

		// Check for prefill incompatibility with thinking mode - strip leading assistant and retry
		if ac.isPrefillIncompatibilityError(err) {
			stripped := stripLeadingAssistantPrefillFromMessages(messages)
			if len(stripped) != len(messages) {
				if ac.agent.debug {
					ac.agent.debugLog("DEBUG: prefill/thinking incompatibility detected, stripped %d leading assistant message(s)\n", len(messages)-len(stripped))
				}
				ac.agent.PrintLineAsync("[~] Retrying without assistant prefill (incompatible with thinking mode)")
				messages = stripped
				continue
			}
			// Nothing to strip, this error can't be recovered - fall through to fail
		}

		// Check for image-not-supported error - strip images and retry once
		if ac.isImageNotSupportedError(err) {
			stripped, hadImages := stripImagesFromMessages(messages)
			if hadImages {
				if ac.agent.debug {
					ac.agent.debugLog("DEBUG: image-not-supported error, retrying without images\n")
				}
				ac.agent.PrintLineAsync("[img] Model does not support image input; retrying without images")
				messages = stripped
				continue
			}
		}

		// Handle error with retry logic
		if !ac.shouldRetry(err, retry) {
			if ac.agent.debug {
				ac.agent.debugLog("DEBUG: APIClient not retrying error: %v\n", err)
			}
			if ac.isRateLimit(err) {
				// Rate limit reached — return error. The rotation counter was
				// already advanced by NextKey() during the failed resolve(),
				// so the next call will naturally use a different key.
				return nil, &RateLimitExceededError{Attempts: retry + 1, LastError: err}
			}
			// Classify the error for better error handling
			if ac.isContextLimitError(err) {
				return nil, agenterrors.NewContextError("failed to make API request", err)
			}
			return nil, agenterrors.NewTransientError("failed to make API request", err)
		}

		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient retrying due to: %v\n", err)
		}

		// Calculate backoff delay
		sleepTime := ac.calculateBackoff(err, retry, retryDelay)
		time.Sleep(sleepTime)
		retryDelay *= 2
	}

	if err != nil && ac.isRateLimit(err) {
		return nil, &RateLimitExceededError{Attempts: ac.maxRetries + 1, LastError: err}
	}

	if err != nil {
		// Classify the error for better error handling
		if ac.isContextLimitError(err) {
			return nil, agenterrors.NewContextError("send request", err)
		}
		return nil, agenterrors.NewTransientError("send request", err)
	}
	return resp, nil
}

// sendRequest sends a single request to the LLM
func (ac *APIClient) sendRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	// Proactive per-provider rate limiting to prevent cascading 429s
	// when multiple agents share the same provider.
	limiter := utils.GetProviderRateLimiter(ac.agent.GetProvider())
	if err := limiter.Wait(ac.agent.interruptCtx); err != nil {
		return nil, agenterrors.NewTransientError("rate limit wait canceled", err)
	}

	// Estimate and store the current request's token count before sending
	ac.agent.state.SetCurrentContextTokens(ac.estimateRequestTokens(messages, tools))

	// Optional context breakdown diagnostic
	if configuration.GetEnvSimple("CONTEXT_DIAG") != "" {
		ac.printContextBreakdown(messages, tools)
	}

	if ac.agent.output.IsStreamingEnabled() {
		return ac.sendStreamingRequest(messages, tools, reasoning, disableThinking)
	}
	return ac.sendRegularRequest(messages, tools, reasoning, disableThinking)
}

// printContextBreakdown logs a per-message breakdown to help diagnose large first-turn context
func (ac *APIClient) printContextBreakdown(messages []api.Message, tools []api.Tool) {
	if ac.agent == nil || !ac.agent.debug {
		return
	}
	totalMsgTokens := 0
	ac.agent.debugLog("\n[search] Context Breakdown (messages=%d, tools=%d)\n", len(messages), len(tools))
	for i, m := range messages {
		tokens := api.EstimateTokens(m.Content) + api.EstimateTokens(m.ReasoningContent) + api.MessageOverheadTokens
		for _, tc := range m.ToolCalls {
			tokens += api.EstimateTokens(tc.ID)
			tokens += api.EstimateTokens(tc.Type)
			tokens += api.EstimateTokens(tc.Function.Name)
			tokens += api.EstimateTokens(tc.Function.Arguments)
			tokens += api.ToolCallOverheadTokens
		}
		if m.ToolCallId != "" {
			tokens += api.EstimateTokens(m.ToolCallId)
			tokens += api.ToolCallIDOverheadTokens
		}
		for _, img := range m.Images {
			tokens += api.ImageMessageOverheadTokens
			tokens += api.EstimateTokens(img.URL)
			tokens += api.EstimateTokens(img.Type)
			tokens += api.EstimateTokens(img.Base64)
		}
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
		ac.agent.debugLog("  %2d) role=%s est_tokens=%d%s | %s\n", i, m.Role, tokens, tag, preview)
	}
	toolTokens := len(tools) * api.ToolTokenEstimate
	total := totalMsgTokens + toolTokens + api.SystemInstructionBuffer
	ac.agent.debugLog("  Messages: est_tokens=%d\n", totalMsgTokens)
	ac.agent.debugLog("  Tools: count=%d est_tokens=%d\n", len(tools), toolTokens)
	ac.agent.debugLog("  Total est_tokens=%d (what footer will display as prompt)\n\n", total)
}

// sendStreamingRequest handles streaming API requests with timeouts
func (ac *APIClient) sendStreamingRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
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

	// Enhanced callback with timeout tracking and content type
	streamCallback := func(content string, contentType string) {
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
				ac.agent.debugLog("[!!] ANSI DETECTED in streaming content: %q\n", content)
			}
		}
		// Sanitize content before storing in streaming buffer
		sanitizedContent := sanitizeStreamingContent(content)
		if sanitizedContent != content && ac.agent.debug {
			ac.agent.debugLog("[clean] Sanitized streaming content, removed ANSI codes\n")
		}
		if contentType == "reasoning" {
			ac.agent.output.GetReasoningBuffer().WriteString(sanitizedContent)
		} else {
			ac.agent.output.GetStreamingBuffer().WriteString(sanitizedContent)
		}

		// Route through OutputRouter (single source: publishes event + writes terminal)
		// This replaces the old dual-write pattern of PublishStreamChunk + streamingCallback
		ac.agent.PublishStreamChunk(sanitizedContent, contentType)
	}

	// Start the API call in a goroutine
	go func() {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: APIClient calling client.SendChatRequestStream at %s\n", time.Now().Format("15:04:05.000"))
		}
		resp, err := ac.agent.client.SendChatRequestStream(messages, tools, reasoning, disableThinking, streamCallback)
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
			return nil, agenterrors.NewTransientError("request interrupted by user", nil)

		case <-ctx.Done():
			ac.displayTimeoutError("Request timed out", ac.overallTimeout)
			return nil, agenterrors.NewTransientError(fmt.Sprintf("API request timed out after %s", ac.overallTimeout), nil)

		case <-firstChunkTimer.C:
			if !firstChunkReceived {
				ac.displayTimeoutError("No response from API", ac.firstChunkTimeout)
				return nil, agenterrors.NewTransientError(fmt.Sprintf("no response received within %s", ac.firstChunkTimeout), nil)
			}

		case <-chunkTimer.C:
			ac.displayTimeoutError("API stopped responding", ac.chunkTimeout)
			return nil, agenterrors.NewTransientError(fmt.Sprintf("no data received for %s", ac.chunkTimeout), nil)

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
			if mu := ac.agent.output.GetOutputMutex(); mu != nil {
				mu.Lock()
				if result.err != nil {
					fmt.Print("\r\033[K") // Clear line on error
				}
				os.Stdout.Sync()
				mu.Unlock()
			}

			// Log the accumulated streaming response for debugging
			if ac.agent.output.IsStreamingEnabled() {
				LogAPIResponse(ac.agent.output.GetStreamingBuffer().String(), true)
				logChatResponseDetailed(result.resp, ac.agent.client.GetProvider(), true, ac.agent.state.GetCurrentIteration())
			}

			if result.err != nil {
				if !ac.isRateLimit(result.err) && !ac.isContextLimitError(result.err) {
					ac.displayAPIError(result.err)
				}
				return result.resp, fmt.Errorf("failed to execute streaming API request: %w", result.err)
			}

			// Note: Tool execution and response processing is handled by the main conversation handler
			// The streaming handler only manages the streaming output and timeout

			return result.resp, nil
		}
	}
}

// sendRegularRequest handles non-streaming API requests with timeout
func (ac *APIClient) sendRegularRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	// Special case for OpenAI token tracking
	if ac.agent.GetProvider() == "openai" && ac.agent.state.GetCurrentIteration() == 0 {
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
		resp, err := ac.agent.client.SendChatRequest(messages, tools, reasoning, disableThinking)
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
		return nil, agenterrors.NewTransientError("request interrupted by user", nil)

	case <-ctx.Done():
		ac.displayTimeoutError("Request timed out", ac.overallTimeout)
		return nil, agenterrors.NewTransientError(fmt.Sprintf("API request timed out after %s", ac.overallTimeout), nil)

	case result := <-resultChan:
		logChatResponseDetailed(result.resp, ac.agent.client.GetProvider(), false, ac.agent.state.GetCurrentIteration())
		if result.err != nil {
			if !ac.isRateLimit(result.err) && !ac.isContextLimitError(result.err) {
				ac.displayAPIError(result.err)
			}
			return result.resp, fmt.Errorf("failed to execute regular API request: %w", result.err)
		}
		return result.resp, nil
	}
}

// shouldRetry determines if an error is retryable
func (ac *APIClient) shouldRetry(err error, attempt int) bool {
	if err == nil {
		return false
	}

	// Check typed AgentError categories first — this enables intelligent retry decisions
	if cat, ok := agenterrors.GetCategory(err); ok {
		switch cat {
		case agenterrors.CategoryTransient, agenterrors.CategoryContext:
			// Transient and context errors are retryable
			return attempt < ac.maxRetries
		case agenterrors.CategoryRateLimited:
			if ac.agent.debug {
				ac.agent.debugLog("DEBUG: shouldRetry - typed rate limit detected: %v\n", err)
			}
			return ac.handleRateLimit(err, attempt)
		case agenterrors.CategorySecurity, agenterrors.CategoryInvalidInput, agenterrors.CategoryPermanent:
			// These should never be retried
			if ac.agent.debug {
				ac.agent.debugLog("DEBUG: shouldRetry - non-retryable category %s: %v\n", cat, err)
			}
			return false
		case agenterrors.CategoryProvider:
			// Provider errors: retryable depends on the specific cause
			if agenterrors.IsRetryable(err) && attempt < ac.maxRetries {
				return true
			}
			return false
		}
	}

	if ac.isRateLimit(err) {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: shouldRetry - rate limit detected: %v\n", err)
		}
		return ac.handleRateLimit(err, attempt)
	}

	// Check other retryable errors
	isRetryable := ac.isRetryableError(err.Error())
	withinMaxRetries := attempt < ac.maxRetries
	result := isRetryable && withinMaxRetries

	if ac.agent.debug {
		ac.agent.debugLog("DEBUG: shouldRetry - error: %v, isRetryable: %v, attempt: %d/%d, result: %v\n",
			err, isRetryable, attempt, ac.maxRetries, result)
	}

	return result
}

// isRateLimit checks if error is a real rate limit (more precise detection)
func (ac *APIClient) isRateLimit(err error) bool {
	if ac.rateLimiter == nil {
		return false
	}

	return ac.rateLimiter.IsRateLimitError(err, nil)
}

// handleRateLimit handles rate limit errors with proper backoff.
// When the provider has multiple keys (pool), it refreshes the cached API key
// so the next request uses a different key. RefreshAPIKey calls resolve() →
// NextKey(), which auto-advances the rotation counter — no additional Advance()
// is needed.
func (ac *APIClient) handleRateLimit(err error, attempt int) bool {
	// Log the rate limit
	ac.rateLimiter.LogRateLimit(ac.agent.GetProvider(), ac.agent.GetModel(),
		ac.agent.state.GetTotalTokens(), err, nil)

	// Check if we should retry
	if !ac.rateLimiter.ShouldRetry(attempt) {
		return false
	}

	// If the provider has multiple keys, refresh the cached key so the next
	// resolve picks a different one. (No-op if single key or env var.)
	provider := ac.agent.GetProvider()
	poolSize, poolErr := credentials.GetPoolSize(provider)
	if poolErr == nil && poolSize > 1 {
		if ac.agent.debug {
			ac.agent.debugLog("DEBUG: rotating key for provider %q (pool size %d) after rate limit on attempt %d\n", provider, poolSize, attempt)
		}
		// Refresh the cached key in the provider client so the next request uses it.
		// resolve() → NextKey() naturally advances the rotation counter, so no
		// explicit Advance() call is needed — NextKey's auto-advance handles it.
		if refreshable, ok := ac.agent.client.(interface{ RefreshAPIKey() error }); ok {
			if refreshErr := refreshable.RefreshAPIKey(); refreshErr != nil {
				log.Printf("[agent] Warning: failed to refresh API key after rotation for %q: %v\n", provider, refreshErr)
			}
		}
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
	// Retry gateway errors (502, upstream errors) - these are often transient infrastructure issues
	if strings.Contains(errStr, "502") || strings.Contains(errStr, "upstream error") {
		return true
	}

	return strings.Contains(errStr, "stream error") ||
		strings.Contains(errStr, "INTERNAL_ERROR") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "timeout")
}

// isImageNotSupportedError checks if an error indicates the model doesn't support image input
func (ac *APIClient) isImageNotSupportedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "image input is not supported") ||
		strings.Contains(errStr, "does not support image") ||
		strings.Contains(errStr, "vision is not supported") ||
		strings.Contains(errStr, "multimodal is not supported")
}

// stripImagesFromMessages returns a copy of messages with all Images fields cleared.
// Returns the stripped messages and whether any images were actually present.
func stripImagesFromMessages(messages []api.Message) ([]api.Message, bool) {
	hasImages := false
	out := make([]api.Message, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].Images) > 0 {
			hasImages = true
			out[i].Images = nil
		}
	}
	return out, hasImages
}

// isContextLimitError checks if an error indicates the context window limit was exceeded
func (ac *APIClient) isContextLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Check for common context limit error patterns from various providers
	return strings.Contains(errStr, "context window exceeds") ||
		strings.Contains(errStr, "context window over") ||
		strings.Contains(errStr, "context_limit") ||
		strings.Contains(errStr, "context exceeds") ||
		strings.Contains(errStr, "max context") ||
		strings.Contains(errStr, "available context size") ||
		strings.Contains(errStr, "exceed_context_size_error") ||
		strings.Contains(errStr, "maximum context length") ||
		(strings.Contains(errStr, "token limit") && strings.Contains(errStr, "exceeded")) ||
		(strings.Contains(errStr, "request") && strings.Contains(errStr, "exceeds") && strings.Contains(errStr, "context"))
}

type contextLimitTokenPair struct {
	prompt int
	limit  int
}

func (ac *APIClient) extractContextLimitTokenPair(err error) contextLimitTokenPair {
	if err == nil {
		return contextLimitTokenPair{}
	}

	errStr := err.Error()
	rePrompt := regexp.MustCompile(`"n_prompt_tokens"\s*:\s*(\d+)`)
	reCtx := regexp.MustCompile(`"n_ctx"\s*:\s*(\d+)`)

	result := contextLimitTokenPair{}
	if matches := rePrompt.FindStringSubmatch(errStr); len(matches) == 2 {
		_, _ = fmt.Sscanf(matches[1], "%d", &result.prompt)
	}
	if matches := reCtx.FindStringSubmatch(errStr); len(matches) == 2 {
		_, _ = fmt.Sscanf(matches[1], "%d", &result.limit)
	}

	return result
}

// isPrefillIncompatibilityError checks if an error indicates prefill is incompatible with thinking mode
func (ac *APIClient) isPrefillIncompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "prefill") ||
		strings.Contains(errStr, "enable_thinking")
}

// stripLeadingAssistantPrefillFromMessages removes leading assistant messages
// (compaction summaries) without tool_calls that appear after system messages.
// Returns the stripped message slice.
func stripLeadingAssistantPrefillFromMessages(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	start := 0
	for start < len(messages) && messages[start].Role == "system" {
		start++
	}
	if start >= len(messages) {
		return messages
	}

	stripped := 0
	for start < len(messages) && messages[start].Role == "assistant" && len(messages[start].ToolCalls) == 0 {
		stripped++
		start++
	}

	if stripped == 0 {
		return messages
	}

	result := make([]api.Message, 0, len(messages)-stripped)
	result = append(result, messages[:start-stripped]...)
	result = append(result, messages[start:]...)
	return result
}

// calculateBackoff calculates the backoff delay
func (ac *APIClient) calculateBackoff(err error, attempt int, baseDelay time.Duration) time.Duration {
	// For rate limits, this is handled separately
	if ac.isRateLimit(err) {
		return 0 // Already handled
	}

	// Exponential backoff with jitter
	jitter := time.Duration(rand.Float64() * float64(baseDelay/2))
	return baseDelay + jitter
}

// showTokenTrackingMessage shows OpenAI token tracking message
func (ac *APIClient) showTokenTrackingMessage() {
	message := "[chart] Using non-streaming mode for accurate token tracking..."
	ac.agent.PrintLine(message)
	ac.agent.PrintLine("")
}

// estimateRequestTokens estimates the token count for the current request
func (ac *APIClient) estimateRequestTokens(messages []api.Message, tools []api.Tool) int {
	return api.EstimateInputTokens(messages, tools)
}

func estimateCompletionTokensFromResponse(resp *api.ChatResponse) int {
	if resp == nil {
		return 0
	}
	total := 0
	for _, choice := range resp.Choices {
		total += EstimateTokens(choice.Message.Content)
		total += EstimateTokens(choice.Message.ReasoningContent)
	}
	return total
}

func (ac *APIClient) deriveUsageMetrics(resp *api.ChatResponse, messages []api.Message, tools []api.Tool) (promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens int, estimatedUsage bool) {
	if resp == nil {
		return 0, 0, 0, 0, 0, false
	}

	promptTokens = resp.Usage.PromptTokens
	completionTokens = resp.Usage.CompletionTokens
	totalTokens = resp.Usage.TotalTokens
	cachedTokens = resp.Usage.PromptTokensDetails.CachedTokens

	// OpenRouter returns cost directly in the "cost" field
	// Other providers may return "estimated_cost" or nothing
	if resp.Usage.Cost > 0 {
		estimatedCost = resp.Usage.Cost
	} else {
		estimatedCost = resp.Usage.EstimatedCost
	}

	hasProviderUsage := promptTokens > 0 || completionTokens > 0 || totalTokens > 0
	if hasProviderUsage {
		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}
		// If provider returned token counts but no EstimatedCost, calculate fallback cost
		if estimatedCost == 0 && (promptTokens > 0 || completionTokens > 0) {
			estimatedCost = ac.calculateFallbackCost(promptTokens, completionTokens)
		}
		return promptTokens, completionTokens, totalTokens, estimatedCost, cachedTokens, false
	}

	// Provider did not return usage; compute a best-effort estimate.
	promptTokens = ac.estimateRequestTokens(messages, tools)
	completionTokens = estimateCompletionTokensFromResponse(resp)
	totalTokens = promptTokens + completionTokens
	return promptTokens, completionTokens, totalTokens, estimatedCost, cachedTokens, true
}

// calculateFallbackCost calculates cost when provider doesn't return EstimatedCost
func (ac *APIClient) calculateFallbackCost(promptTokens, completionTokens int) float64 {
	model := ac.agent.GetModel()
	provider := ac.agent.GetProvider()

	// Get input and output cost per million tokens based on model/provider
	inputCostPerM, outputCostPerM := getModelPricing(model, provider)

	inputCost := float64(promptTokens) * inputCostPerM / 1000000
	outputCost := float64(completionTokens) * outputCostPerM / 1000000

	return inputCost + outputCost
}

// getModelPricing returns estimated input and output cost per million tokens for a given model/provider.
// Prices are approximate as of April 2025 and may not reflect current provider rates.
// This is only used as a fallback when providers don't return cost data; prefer provider-reported costs.
func getModelPricing(model, provider string) (inputCostPerM float64, outputCostPerM float64) {
	// Default pricing (conservative estimate)
	inputCostPerM = 1.0  // $1 per million input tokens
	outputCostPerM = 2.0 // $2 per million output tokens

	modelLower := strings.ToLower(model)
	providerLower := strings.ToLower(provider)

	// DeepInfra-specific pricing
	if providerLower == "deepinfra" {
		if strings.Contains(modelLower, "deepseek-v3") || strings.Contains(modelLower, "deepseek-v2") {
			inputCostPerM = 0.27   // $0.27 per million input
			outputCostPerM = 1.10   // $1.10 per million output
		} else if strings.Contains(modelLower, "deepseek-r1") {
			inputCostPerM = 0.55    // $0.55 per million input
			outputCostPerM = 2.19   // $2.19 per million output
		} else if strings.Contains(modelLower, "llama-3.3") || strings.Contains(modelLower, "llama-3.1-70b") {
			inputCostPerM = 0.88    // $0.88 per million input
			outputCostPerM = 0.88  // $0.88 per million output
		} else if strings.Contains(modelLower, "llama-3.1-405b") {
			inputCostPerM = 5.00   // $5.00 per million input
			outputCostPerM = 5.00  // $5.00 per million output
		} else if strings.Contains(modelLower, "qwen-2.5") {
			inputCostPerM = 0.30   // $0.30 per million input
			outputCostPerM = 0.60  // $0.60 per million output
		} else if strings.Contains(modelLower, "qwen3-coder") {
			inputCostPerM = 0.30   // $0.30 per million input
			outputCostPerM = 0.60  // $0.60 per million output
		} else if strings.Contains(modelLower, "mistral") {
			inputCostPerM = 0.24   // $0.24 per million input
			outputCostPerM = 0.24  // $0.24 per million output
		}
		return
	}

	// OpenRouter-specific pricing
	if providerLower == "openrouter" {
		if strings.Contains(modelLower, "deepseek-chat") || strings.Contains(modelLower, "deepseek-r1") {
			inputCostPerM = 0.55   // $0.55 per million input
			outputCostPerM = 2.19   // $2.19 per million output
		} else if strings.Contains(modelLower, "gpt-4o") {
			inputCostPerM = 2.50    // $2.50 per million input
			outputCostPerM = 10.00  // $10.00 per million output
		} else if strings.Contains(modelLower, "gpt-4-turbo") {
			inputCostPerM = 10.00   // $10.00 per million input
			outputCostPerM = 30.00  // $30.00 per million output
		} else if strings.Contains(modelLower, "gpt-4") && !strings.Contains(modelLower, "gpt-4o") && !strings.Contains(modelLower, "gpt-4-turbo") {
			inputCostPerM = 30.00   // $30 per million input
			outputCostPerM = 60.00  // $60 per million output
		} else if strings.Contains(modelLower, "claude-3.5-sonnet") || strings.Contains(modelLower, "claude-3-sonnet") {
			inputCostPerM = 3.00    // $3.00 per million input
			outputCostPerM = 15.00  // $15.00 per million output
		} else if strings.Contains(modelLower, "claude-3-opus") {
			inputCostPerM = 15.00   // $15.00 per million input
			outputCostPerM = 75.00  // $75.00 per million output
		} else if strings.Contains(modelLower, "claude-3-haiku") {
			inputCostPerM = 0.25    // $0.25 per million input
			outputCostPerM = 1.25   // $1.25 per million output
		} else if strings.Contains(modelLower, "llama-3.1-405b") {
			inputCostPerM = 5.00    // $5.00 per million input
			outputCostPerM = 5.00   // $5.00 per million output
		} else if strings.Contains(modelLower, "llama-3.1-70b") {
			inputCostPerM = 0.88    // $0.88 per million input
			outputCostPerM = 0.88   // $0.88 per million output
		} else if strings.Contains(modelLower, "llama-3.1-8b") {
			inputCostPerM = 0.18    // $0.18 per million input
			outputCostPerM = 0.18   // $0.18 per million output
		}
		return
	}

	// OpenAI-specific pricing
	if providerLower == "openai" {
		if strings.Contains(modelLower, "gpt-4o") {
			inputCostPerM = 2.50    // $2.50 per million input
			outputCostPerM = 10.00   // $10.00 per million output
		} else if strings.Contains(modelLower, "gpt-4-turbo") {
			inputCostPerM = 10.00   // $10.00 per million input
			outputCostPerM = 30.00   // $30.00 per million output
		} else if strings.Contains(modelLower, "gpt-4") && !strings.Contains(modelLower, "gpt-4o") && !strings.Contains(modelLower, "gpt-4-turbo") {
			inputCostPerM = 30.00    // $30 per million input
			outputCostPerM = 60.00   // $60 per million output
		} else if strings.Contains(modelLower, "gpt-3.5-turbo") {
			inputCostPerM = 0.50     // $0.50 per million input
			outputCostPerM = 1.50    // $1.50 per million output
		}
		return
	}

	// Generic model-based pricing
	if strings.Contains(modelLower, "gpt-oss") {
		inputCostPerM = 0.30    // $0.30 per million input
		outputCostPerM = 0.60   // $0.60 per million output
	} else if strings.Contains(modelLower, "qwen3-coder") || strings.Contains(modelLower, "qwen-coder") {
		inputCostPerM = 0.30    // $0.30 per million input
		outputCostPerM = 0.60   // $0.60 per million output
	} else if strings.Contains(modelLower, "llama") {
		inputCostPerM = 0.36    // $0.36 per million input
		outputCostPerM = 0.36  // $0.36 per million output
	} else if strings.Contains(modelLower, "deepseek") {
		inputCostPerM = 0.55    // $0.55 per million input
		outputCostPerM = 2.19  // $2.19 per million output
	}

	return
}

// displayTimeoutError shows a user-friendly timeout error
func (ac *APIClient) displayTimeoutError(message string, timeout time.Duration) {
	errorMsg := fmt.Sprintf("[plug] TIMEOUT ERROR: %s (waited %v)\n\nThis usually means the API is taking too long to respond.\nPossible causes:\n- High model latency (common with large models or complex queries)\n- Network issues\n- API overloaded\n\nThe request has been aborted. Try:\n- Using a faster model\n- Reducing query complexity\n- Checking your network connection", message, timeout)
	// Route through agent so interactive console places it in content area
	ac.agent.PrintLine(errorMsg)
}

// displayAPIError shows a user-friendly API error
func (ac *APIClient) displayAPIError(err error) {
	providerName := ac.agent.GetProvider()
	errorMsg := fmt.Sprintf("[!!] %s API Error: %v", strings.Title(providerName), err)
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
