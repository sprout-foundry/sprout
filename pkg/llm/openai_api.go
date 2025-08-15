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
)

// callOpenAICompatibleStream calls OpenAI-compatible APIs and returns token usage information
func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) (*TokenUsage, error) {
	// Build request with optional temperature; retry once without it if provider rejects
	buildBody := func(includeTemp bool) ([]byte, error) {
		payload := map[string]interface{}{
			"model":    model,
			"messages": messages,
			"stream":  true,
		}
		if includeTemp {
			payload["temperature"] = cfg.Temperature
		}
		return json.Marshal(payload)
	}

	tryOnce := func(reqBody []byte) (*http.Response, error) {
		req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
		if err != nil {
			ui.Out().Print(prompts.RequestCreationError(err))
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: timeout}
		return client.Do(req)
	}

	bodyWithTemp, err := buildBody(true)
	if err != nil {
		ui.Out().Print(prompts.RequestMarshalError(err))
		return nil, err
	}
	resp, err := tryOnce(bodyWithTemp)
	if err != nil {
		ui.Out().Printf("DEBUG: HTTP request failed: %v\n", err)
		ui.Out().Print(prompts.HTTPRequestError(err))
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
		usage = estimateUsageFromMessages(messages)
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
		req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: timeout}
		return client.Do(req)
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
