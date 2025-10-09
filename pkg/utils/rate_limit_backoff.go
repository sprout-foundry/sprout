package utils

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitBackoff handles rate limit detection and backoff calculations
type RateLimitBackoff struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	BufferTime time.Duration
	outputFn   func(string)
}

// NewRateLimitBackoff creates a new rate limit backoff handler with sensible defaults
func NewRateLimitBackoff() *RateLimitBackoff {
	return &RateLimitBackoff{
		MaxRetries: 3,
		BaseDelay:  2 * time.Second,
		MaxDelay:   60 * time.Second,
		BufferTime: 2 * time.Second,
		outputFn:   func(msg string) { fmt.Print(msg) },
	}
}

// SetOutputFunc overrides the default output function for user-facing messages
func (rlb *RateLimitBackoff) SetOutputFunc(fn func(string)) {
	if fn == nil {
		rlb.outputFn = func(msg string) { fmt.Print(msg) }
		return
	}
	rlb.outputFn = fn
}

func (rlb *RateLimitBackoff) print(msg string) {
	if rlb.outputFn != nil {
		rlb.outputFn(msg)
		return
	}
	fmt.Print(msg)
}

func containsRateLimitPhrases(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "requests per minute") ||
		strings.Contains(s, "rpm exceeded") ||
		strings.Contains(s, "rate exceeded") ||
		strings.Contains(s, "quota exceeded") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "insufficient_quota") ||
		strings.Contains(s, "insufficient quota") ||
		(strings.Contains(s, "quota") && strings.Contains(s, "exceeded")) ||
		strings.Contains(s, "current quota")
}

// IsRateLimitError checks if an error or HTTP response indicates a rate limit
func (rlb *RateLimitBackoff) IsRateLimitError(err error, resp *http.Response) bool {
	// HTTP 429 is generally a reliable indicator
	if resp != nil && resp.StatusCode == 429 {
		return true
	}

	if err != nil {
		errStr := strings.ToLower(err.Error())
		// Treat any reference to HTTP 429 as a rate limit, even if the phrase
		// "too many requests" is absent. Providers often format as "status 429".
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "http 429") || strings.Contains(errStr, "status 429") {
			return true
		}

		// Treat any reference to HTTP 403 as potentially a rate limit (OpenAI specific)
		if strings.Contains(errStr, "403") || strings.Contains(errStr, "http 403") || strings.Contains(errStr, "status 403") {
			return containsRateLimitPhrases(errStr)
		}

		// Otherwise match common phrasing used by providers
		return containsRateLimitPhrases(errStr)
	}

	return false
}

// CalculateBackoffDelay calculates how long to wait before retrying
func (rlb *RateLimitBackoff) CalculateBackoffDelay(resp *http.Response, attempt int) time.Duration {
	// First try to use rate limit headers if available
	if resp != nil {
		if delay := rlb.parseRateLimitHeaders(resp); delay > 0 {
			return delay
		}
	}

	// Fallback to exponential backoff
	return rlb.exponentialBackoff(attempt)
}

// parseRateLimitHeaders attempts to parse rate limit headers from various providers
func (rlb *RateLimitBackoff) parseRateLimitHeaders(resp *http.Response) time.Duration {
	// Try different header formats used by various providers

	// OpenRouter format (X-RateLimit-Reset in milliseconds)
	if resetHeader := resp.Header.Get("X-RateLimit-Reset"); resetHeader != "" {
		if resetTime, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
			resetAt := time.Unix(resetTime/1000, (resetTime%1000)*1000000)
			waitTime := time.Until(resetAt)
			if waitTime > 0 {
				return rlb.capDelay(waitTime + rlb.BufferTime)
			}
		}
	}

	// OpenAI format (X-RateLimit-Reset-Tokens, X-RateLimit-Reset-Requests in seconds)
	if resetHeader := resp.Header.Get("X-RateLimit-Reset-Tokens"); resetHeader != "" {
		if resetTime, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
			resetAt := time.Unix(resetTime, 0)
			waitTime := time.Until(resetAt)
			if waitTime > 0 {
				return rlb.capDelay(waitTime + rlb.BufferTime)
			}
		}
	}

	// Retry-After header (in seconds)
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			waitTime := time.Duration(seconds) * time.Second
			return rlb.capDelay(waitTime + rlb.BufferTime)
		}
	}

	return 0 // No parseable headers found
}

// exponentialBackoff calculates exponential backoff delay
func (rlb *RateLimitBackoff) exponentialBackoff(attempt int) time.Duration {
	delay := rlb.BaseDelay * time.Duration(math.Pow(2, float64(attempt)))
	return rlb.capDelay(delay)
}

// capDelay ensures delay doesn't exceed maximum
func (rlb *RateLimitBackoff) capDelay(delay time.Duration) time.Duration {
	if delay > rlb.MaxDelay {
		return rlb.MaxDelay
	}
	if delay < 0 {
		return rlb.BaseDelay
	}
	return delay
}

// ShouldRetry determines if we should retry based on attempt count
func (rlb *RateLimitBackoff) ShouldRetry(attempt int) bool {
	return attempt < rlb.MaxRetries
}

// LogRateLimit logs rate limit information for analysis
func (rlb *RateLimitBackoff) LogRateLimit(provider, model string, totalTokens int, err error, resp *http.Response) {
	logger := GetLogger(false)

	// Extract rate limit details from headers
	rateLimitInfo := ""
	if resp != nil {
		if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
			rateLimitInfo += fmt.Sprintf(" Remaining: %s", remaining)
		}
		if limit := resp.Header.Get("X-RateLimit-Limit"); limit != "" {
			rateLimitInfo += fmt.Sprintf(" Limit: %s", limit)
		}
		if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
			rateLimitInfo += fmt.Sprintf(" Reset: %s", reset)
		}
	}

	errorMsg := "unknown"
	if err != nil {
		errorMsg = err.Error()
	}

	logger.LogProcessStep(fmt.Sprintf("🚨 RATE LIMIT: %s/%s | Tokens: %d | Error: %s%s",
		provider, model, totalTokens, errorMsg, rateLimitInfo))

	// Also log to run logger for structured data
	if rl := GetRunLogger(); rl != nil {
		fields := map[string]any{
			"provider":     provider,
			"model":        model,
			"total_tokens": totalTokens,
			"error":        errorMsg,
			"timestamp":    time.Now().Format(time.RFC3339),
		}

		if resp != nil {
			fields["status_code"] = resp.StatusCode
			if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
				fields["rate_limit_remaining"] = remaining
			}
			if limit := resp.Header.Get("X-RateLimit-Limit"); limit != "" {
				fields["rate_limit_limit"] = limit
			}
			if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
				fields["rate_limit_reset"] = reset
			}
		}

		rl.LogEvent("rate_limit_hit", fields)
	}
}

// WaitWithProgress waits for the specified duration while showing progress
func (rlb *RateLimitBackoff) WaitWithProgress(duration time.Duration, provider string) {
	if duration <= 0 {
		return
	}

	rlb.print(fmt.Sprintf("⏳ Rate limited by %s. Waiting %v before retry...\n", provider, duration.Round(time.Second)))

	// Show progress for long waits
	if duration > 10*time.Second {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		deadline := time.NewTimer(duration)
		defer deadline.Stop()

		start := time.Now()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start)
				remaining := duration - elapsed
				if remaining <= 0 {
					rlb.print("✅ Rate limit wait complete, retrying...\n")
					return
				}
				rlb.print(fmt.Sprintf("   Still waiting... %v remaining\n", remaining.Round(time.Second)))
			case <-deadline.C:
				rlb.print("✅ Rate limit wait complete, retrying...\n")
				return
			}
		}
	} else {
		time.Sleep(duration)
		rlb.print("✅ Rate limit wait complete, retrying...\n")
	}
}
