package agent

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// estimateRequestTokens estimates tokens for messages using the centralized estimator.
func estimateRequestTokens(messages []api.Message, tools []api.Tool) int {
	return api.EstimateInputTokens(messages, tools)
}

// deriveUsageMetrics extracts token/cost metrics from a chat response, falling back
// to heuristic estimation when the provider does not include usage data.
// cacheWriteTokens is the number of prompt tokens written to the provider cache
// on this request (0 when the provider does not report it or when estimating).
func deriveUsageMetrics(
	resp *api.ChatResponse,
	messages []api.Message,
	tools []api.Tool,
) (promptTokens, completionTokens, totalTokens int, cost float64, cachedTokens, cacheWriteTokens int, estimated bool) {
	if resp != nil && resp.Usage.TotalTokens > 0 {
		writeTokens := 0
		if resp.Usage.CacheWriteTokens != nil {
			writeTokens = *resp.Usage.CacheWriteTokens
		}
		return resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens,
			resp.Usage.EstimatedCost, resp.Usage.CachedTokens, writeTokens, false
	}

	// Estimate prompt tokens from the full message set (including tools).
	promptTokens = estimateRequestTokens(messages, tools)

	// Estimate completion tokens from the assistant message content.
	if resp != nil && len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		completionTokens = EstimateTokens(choice.Message.Content)
	} else {
		completionTokens = EstimateTokens("placeholder")
	}

	totalTokens = promptTokens + completionTokens
	return promptTokens, completionTokens, totalTokens, 0, 0, 0, true
}

// collapseSystemMessagesToFront merges all system messages into a single message
// at the front of the message list, preserving the order of non-system messages.
func collapseSystemMessagesToFront(messages []api.Message) []api.Message {
	if len(messages) <= 1 {
		return messages
	}

	firstSystemIndex := -1
	var systemParts []string
	nonSystem := make([]api.Message, 0, len(messages))

	for i, msg := range messages {
		if msg.Role != "system" {
			nonSystem = append(nonSystem, msg)
			continue
		}

		if firstSystemIndex == -1 {
			firstSystemIndex = i
		}
		if content := msg.Content; content != "" {
			systemParts = append(systemParts, content)
		}
	}

	if firstSystemIndex <= 0 && len(systemParts) <= 1 {
		return messages
	}

	merged := api.Message{Role: "system", Content: joinMessageContent(systemParts, "\n\n")}
	result := make([]api.Message, 0, len(nonSystem)+1)
	result = append(result, merged)
	result = append(result, nonSystem...)
	return result
}

// stripImagesFromMessages creates a copy of messages with all image data removed,
// returning the stripped copy and whether any images were found.
func stripImagesFromMessages(messages []api.Message) ([]api.Message, bool) {
	if messages == nil {
		return nil, false
	}

	hadImages := false
	result := make([]api.Message, len(messages))
	for i, msg := range messages {
		if len(msg.Images) > 0 {
			hadImages = true
			msg.Images = nil
		}
		result[i] = msg
	}
	return result, hadImages
}

// estimateCompletionTokensFromResponse estimates completion tokens from a chat response.
func estimateCompletionTokensFromResponse(resp *api.ChatResponse) int {
	if resp == nil || len(resp.Choices) == 0 {
		return 0
	}
	content := resp.Choices[0].Message.Content
	completionTokens := EstimateTokens(content)
	if resp.Choices[0].Message.ReasoningContent != "" {
		completionTokens += EstimateTokens(resp.Choices[0].Message.ReasoningContent)
	}
	return completionTokens
}

// joinMessageContent joins a slice of message content strings with the given separator.
func joinMessageContent(parts []string, sep string) string {
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return joinStrings(filtered, sep)
}

// joinStrings joins a slice of strings with the given separator.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
