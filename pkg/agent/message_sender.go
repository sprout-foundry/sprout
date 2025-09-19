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

// MessageSender handles sending messages to the LLM with retry logic
type MessageSender struct {
	agent       *Agent
	rateLimiter *utils.RateLimitBackoff
}

// NewMessageSender creates a new message sender
func NewMessageSender(agent *Agent) *MessageSender {
	return &MessageSender{
		agent:       agent,
		rateLimiter: utils.NewRateLimitBackoff(),
	}
}

// SendMessage sends a message to the LLM with retry logic
func (ms *MessageSender) SendMessage(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	var resp *api.ChatResponse
	var err error
	maxRetries := 3
	retryDelay := time.Second

	// Reset streaming buffer
	ms.agent.streamingBuffer.Reset()

	for retry := 0; retry <= maxRetries; retry++ {
		// Update progress monitor
		ms.updateProgress(retry)

		// Send request
		if ms.agent.streamingEnabled {
			resp, err = ms.sendStreamingRequest(messages, tools, reasoning)
		} else {
			resp, err = ms.sendRegularRequest(messages, tools, reasoning)
		}

		// Check if successful
		if err == nil {
			break
		}

		// Check if error is retryable
		if !ms.shouldRetry(err, retry) {
			return nil, err
		}

		// Calculate backoff
		sleepTime := ms.calculateBackoff(err, retry, retryDelay)
		if sleepTime > 0 {
			time.Sleep(sleepTime)
		}
		retryDelay *= 2
	}

	return resp, err
}

// shouldRetry determines if an error should be retried
func (ms *MessageSender) shouldRetry(err error, attempt int) bool {
	errStr := err.Error()

	// Check for rate limits
	if ms.isRateLimit(errStr) {
		// Log the rate limit
		ms.rateLimiter.LogRateLimit(ms.agent.GetProvider(), ms.agent.GetModel(),
			ms.agent.totalTokens, err, nil)

		// Check if we should retry
		if !ms.rateLimiter.ShouldRetry(attempt) {
			return false
		}

		// Calculate and wait for backoff
		backoffDelay := ms.rateLimiter.CalculateBackoffDelay(nil, attempt)
		ms.showRateLimitProgress(backoffDelay)
		return true
	}

	// Check other retryable errors
	isRetryable := strings.Contains(errStr, "stream error") ||
		strings.Contains(errStr, "INTERNAL_ERROR") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "timeout")

	return isRetryable && attempt < 3
}

// isRateLimit checks if error is a rate limit
func (ms *MessageSender) isRateLimit(errStr string) bool {
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "usage limit")
}

// calculateBackoff calculates backoff delay for retries
func (ms *MessageSender) calculateBackoff(err error, attempt int, baseDelay time.Duration) time.Duration {
	// Rate limits are handled separately
	if ms.isRateLimit(err.Error()) {
		return 0
	}

	// Exponential backoff with jitter
	jitter := time.Duration(rand.Float64() * float64(baseDelay/2))
	return baseDelay + jitter
}

// sendStreamingRequest sends a streaming request
func (ms *MessageSender) sendStreamingRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	streamCallback := ms.createStreamCallback()

	resp, err := ms.agent.client.SendChatRequestStream(messages, tools, reasoning, streamCallback)

	// Ensure streaming output is flushed
	ms.flushStreamingOutput(err)

	return resp, err
}

// sendRegularRequest sends a non-streaming request
func (ms *MessageSender) sendRegularRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Special case for OpenAI token tracking
	if ms.agent.GetProvider() == "openai" && ms.agent.currentIteration == 0 {
		ms.showTokenTrackingMessage()
	}

	return ms.agent.client.SendChatRequest(messages, tools, reasoning)
}

// createStreamCallback creates the streaming callback function
func (ms *MessageSender) createStreamCallback() api.StreamCallback {
	return func(content string) {

		// Accumulate content
		ms.agent.streamingBuffer.WriteString(content)

		// Call user callback or default output
		if ms.agent.streamingCallback != nil {
			ms.agent.streamingCallback(content)
		} else if ms.agent.outputMutex != nil {
			ms.agent.outputMutex.Lock()
			fmt.Print(content)
			ms.agent.outputMutex.Unlock()
		}
	}
}

// Helper methods

func (ms *MessageSender) updateProgress(retry int) {
	// Progress monitoring removed
}

func (ms *MessageSender) showRateLimitProgress(delay time.Duration) {
	if ms.agent.outputMutex != nil {
		ms.agent.outputMutex.Lock()
		ms.rateLimiter.WaitWithProgress(delay, ms.agent.GetProvider())
		ms.agent.outputMutex.Unlock()
	} else {
		ms.rateLimiter.WaitWithProgress(delay, ms.agent.GetProvider())
	}
}

func (ms *MessageSender) flushStreamingOutput(err error) {
	if ms.agent.outputMutex != nil {
		ms.agent.outputMutex.Lock()
		if err != nil {
			fmt.Print("\r\033[K") // Clear line on error
		}
		os.Stdout.Sync()
		ms.agent.outputMutex.Unlock()
	}
}

func (ms *MessageSender) showTokenTrackingMessage() {
	message := "📊 Using non-streaming mode for accurate token tracking...\n\n"
	if ms.agent.outputMutex != nil {
		ms.agent.outputMutex.Lock()
		fmt.Print(message)
		ms.agent.outputMutex.Unlock()
	} else {
		fmt.Print(message)
	}
}
