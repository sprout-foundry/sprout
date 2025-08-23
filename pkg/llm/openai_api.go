package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// retryWithBackoffOpenAI executes an HTTP request with exponential backoff retry logic
// Handles 5xx errors, network errors, and specific 4xx errors that might be transient
func retryWithBackoffOpenAI(req *http.Request, client *http.Client) (*http.Response, error) {
	const maxRetries = 3
	const baseDelay = 100 * time.Millisecond

	var lastResp *http.Response
	var lastErr error

	// Buffer original request body to safely retry with fresh requests
	var originalBody []byte
	if req.Body != nil {
		originalBody, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(originalBody))
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request and reset the body for this attempt
		newReq := req.Clone(req.Context())
		if originalBody != nil {
			newReq.Body = io.NopCloser(bytes.NewReader(originalBody))
		}

		resp, err := client.Do(newReq)
		lastResp = resp
		lastErr = err

		if err != nil {
			// Network errors - retry with exponential backoff
			if attempt < maxRetries {
				delay := baseDelay * time.Duration(1<<attempt) // 100ms, 200ms, 400ms
				time.Sleep(delay)
				continue
			}
			return resp, err
		}

		// Check for retryable status codes
		shouldRetry := false
		switch resp.StatusCode {
		case 408: // Request Timeout
			shouldRetry = true
		case 429: // Too Many Requests
			shouldRetry = true
		case 500, 502, 503, 504: // Server errors
			shouldRetry = true
		}

		if shouldRetry && attempt < maxRetries {
			// Close response body before retry
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}

			// Exponential backoff with jitter
			delay := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(time.Now().UnixNano() % int64(delay) / 2) // Add up to 50% jitter
			totalDelay := delay + jitter

			time.Sleep(totalDelay)
			continue
		}

		// Success or non-retryable error
		return resp, err
	}

	return lastResp, lastErr
}

// callOpenAICompatibleStream calls OpenAI-compatible APIs and returns token usage information
func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) (*TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	// Debug: Log function entry
	logger.Logf("DEBUG: callOpenAICompatibleStream called with URL: %s, model: %s", apiURL, model)
	logger.Logf("DEBUG: Messages count: %d", len(messages))
	logger.Logf("DEBUG: Temperature: %f", cfg.Temperature)

	// Build request with optional temperature; retry once without it if provider rejects
	buildBody := func(includeTemp bool) ([]byte, error) {
		payload := map[string]interface{}{
			"model":    model,
			"messages": messages,
			"stream":   true,
		}
		if includeTemp {
			payload["temperature"] = cfg.Temperature
		}

		// Enable JSON mode when prompts explicitly require strict JSON output
		if ShouldUseJSONResponse(messages) {
			payload["response_format"] = map[string]any{"type": "json_object"}
		}

		return json.Marshal(payload)
	}

	tryOnce := func(reqBody []byte) (*http.Response, error) {
		// Debug: Log the actual JSON payload being sent
		logger.Logf("DEBUG: About to send HTTP request to: %s", apiURL)
		logger.Logf("DEBUG: Requested tokens: %d\n", EstimateTokens(string(reqBody)))
		logger.Logf("DEBUG: Request payload length: %d bytes", len(reqBody))
		logger.Logf("DEBUG: Request payload: %s", string(reqBody))

		// Check for detokenize field in the actual request body
		if strings.Contains(string(reqBody), "detokenize") {
			ui.Out().Printf("ERROR: Found 'detokenize' field in request to %s\n", apiURL)
		}

		req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
		if err != nil {
			ui.Out().Print(prompts.RequestCreationError(err))
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		// Create a custom transport to intercept the request
		transport := &http.Transport{}
		client := &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}

		// Log the final request details
		logger.Logf("DEBUG: Final request URL: %s", req.URL)
		logger.Logf("DEBUG: Final request method: %s", req.Method)
		logger.Logf("DEBUG: Final request headers: %v", req.Header)

		return retryWithBackoffOpenAI(req, client)
	}

	bodyWithTemp, err := buildBody(true)
	if err != nil {
		ui.Out().Print(prompts.RequestMarshalError(err))
		return nil, err
	}
	resp, err := tryOnce(bodyWithTemp)
	if err != nil {
		logger.Logf("DEBUG: HTTP request failed: %v", err)
		ui.Out().Print(prompts.HTTPRequestError(err))
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lower := strings.ToLower(string(raw))
		if strings.Contains(lower, "temperature") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "response_format") {
			// Retry without temperature
			bodyNoTemp, merr := buildBody(false)
			if merr != nil {
				ui.Out().Print(prompts.RequestMarshalError(merr))
				return nil, merr
			}
			if r2, r2err := tryOnce(bodyNoTemp); r2err == nil {
				resp = r2
			} else {
				ui.Out().Print(prompts.HTTPRequestError(r2err))
				return nil, r2err
			}
		} else {
			msg := prompts.APIError(string(raw), resp.StatusCode)
			ui.Out().Print(msg)
			return nil, fmt.Errorf("%s", msg)
		}
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := prompts.APIError(string(body), resp.StatusCode)
		ui.Out().Print(msg)
		return nil, fmt.Errorf("%s", msg)
	}

	// For streaming responses, we need to make a separate call to get usage data
	// since streaming doesn't include usage in the stream
	usage, err := getUsageFromNonStreamingCall(apiURL, apiKey, model, messages, cfg, timeout)
	if err != nil {
		// If we can't get usage, fall back to estimation
		est := estimateUsageFromMessages(messages)
		est.Estimated = true
		usage = est
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "data: ")
		if line == "[DONE]" {
			break
		}

		var openAIResp OpenAIResponse
		if err := json.Unmarshal([]byte(line), &openAIResp); err != nil {
			// Don't print error for every line, just continue
			continue
		}

		if len(openAIResp.Choices) > 0 {
			content := openAIResp.Choices[0].Delta.Content
			if _, err := writer.Write([]byte(content)); err != nil {
				return usage, err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ui.Out().Print(prompts.ResponseBodyError(err))
		return usage, err
	}

	return usage, nil
}

// getUsageFromNonStreamingCall makes a non-streaming call to get usage information
func getUsageFromNonStreamingCall(apiURL, apiKey, model string, messages []prompts.Message, cfg *config.Config, timeout time.Duration) (*TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	buildBody := func(includeTemp bool) ([]byte, error) {
		payload := map[string]interface{}{
			"model":      model,
			"messages":   messages,
			"stream":     false,
			"max_tokens": 1, // Minimal tokens to get usage data
		}
		if includeTemp {
			payload["temperature"] = cfg.Temperature
		}
		return json.Marshal(payload)
	}

	tryOnce := func(reqBody []byte) (*http.Response, error) {
		// Debug: Log the actual JSON payload being sent
		logger.Logf("DEBUG: Usage Request Payload: %s", string(reqBody))

		req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: timeout}
		return retryWithBackoffOpenAI(req, client)
	}

	bodyWithTemp, err := buildBody(true)
	if err != nil {
		return nil, err
	}
	resp, err := tryOnce(bodyWithTemp)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lower := strings.ToLower(string(raw))
		if strings.Contains(lower, "temperature") || strings.Contains(lower, "unsupported") {
			// Retry without temperature
			bodyNoTemp, merr := buildBody(false)
			if merr != nil {
				return nil, merr
			}
			if r2, r2err := tryOnce(bodyNoTemp); r2err == nil {
				resp = r2
			} else {
				return nil, r2err
			}
		} else {
			return nil, fmt.Errorf("failed to get usage data: %d", resp.StatusCode)
		}
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get usage data: %d", resp.StatusCode)
	}

	var usageResp OpenAIUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, err
	}

	return &usageResp.Usage, nil
}

// estimateUsageFromMessages provides a fallback estimation when actual usage data isn't available
func estimateUsageFromMessages(messages []prompts.Message) *TokenUsage {
	var promptTokens, completionTokens int

	for _, msg := range messages {
		// Estimate prompt tokens
		promptTokens += GetMessageTokens(msg.Role, GetMessageText(msg.Content))
	}

	// Estimate completion tokens (roughly 1/3 of prompt tokens for typical responses)
	completionTokens = promptTokens / 3

	return &TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}
