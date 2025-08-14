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
)

// callOpenAICompatibleStream calls OpenAI-compatible APIs and returns token usage information
func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) (*TokenUsage, error) {
	// Debug logging removed for cleaner output

	reqBody, err := json.Marshal(map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"temperature": cfg.Temperature, // Use config value
		// "max_tokens":  cfg.MaxTokens,   // Use config value
		// "top_p":       cfg.TopP,        // Use config value
		// "presence_penalty": cfg.PresencePenalty, // Use config value
		// "frequency_penalty": cfg.FrequencyPenalty, // Use config value
		"stream": true,
	})
	if err != nil {
		fmt.Print(prompts.RequestMarshalError(err))
		return nil, err
	}

	// Debug logging removed for cleaner output

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Print(prompts.RequestCreationError(err))
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout}
	// Debug logging removed for cleaner output
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("DEBUG: HTTP request failed: %v\n", err)
		fmt.Print(prompts.HTTPRequestError(err))
		return nil, err
	}
	defer resp.Body.Close()

	// Debug logging removed for cleaner output
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Debug logging removed for cleaner output
		fmt.Print(prompts.APIError(string(body), resp.StatusCode))
		return nil, fmt.Errorf(prompts.APIError(string(body), resp.StatusCode))
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
		if strings.HasPrefix(line, "data: ") {
			line = line[6:]
		}
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
		fmt.Print(prompts.ResponseBodyError(err))
		return usage, err
	}

	return usage, nil
}

// getUsageFromNonStreamingCall makes a non-streaming call to get usage information
func getUsageFromNonStreamingCall(apiURL, apiKey, model string, messages []prompts.Message, cfg *config.Config, timeout time.Duration) (*TokenUsage, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"temperature": cfg.Temperature,
		"stream":      false,
		"max_tokens":  1, // Minimal tokens to get usage data
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
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
