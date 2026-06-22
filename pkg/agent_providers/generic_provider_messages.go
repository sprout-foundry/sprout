package providers

import (
	"encoding/json"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// convertMessages converts messages according to provider configuration.
// It also merges consecutive same-role messages (e.g. two user messages in a row)
// which can occur when an API call fails and the user retries — no assistant
// response is inserted between attempts. Most providers reject such sequences.
func (p *GenericProvider) convertMessages(messages []api.Message, reasoning string) []map[string]interface{} {
	reasoningField := p.config.Conversion.ReasoningContentField
	skipReasoningHistory := p.shouldSkipReasoningContentHistory()

	// Build converted messages, merging consecutive same-role messages.
	// Merging accumulates content with a newline separator. For tool_calls
	// or tool_call_id, merging is not attempted — the duplicate user messages
	// case only involves plain text content.
	converted := make([]map[string]interface{}, 0, len(messages))
	var pendingRole string
	var pendingContent string
	var pendingReasoning string // preserved for compatible providers

	flush := func() {
		if pendingRole == "" {
			return
		}
		entry := map[string]interface{}{
			"role":    pendingRole,
			"content": pendingContent,
		}
		if !skipReasoningHistory && pendingReasoning != "" && reasoningField != "" {
			entry[reasoningField] = pendingReasoning
		}
		converted = append(converted, entry)
		pendingRole = ""
		pendingContent = ""
		pendingReasoning = ""
	}

	for _, msg := range messages {
		// Tool messages carry tool_call_id and must preserve individual identity.
		// Assistant messages with tool_calls likewise must not be merged.
		isMergeable := (msg.Role == "user") ||
			(msg.Role == "assistant" && len(msg.ToolCalls) == 0)

		if isMergeable && msg.Role == pendingRole {
			// Same role — append content
			if pendingContent != "" && msg.Content != "" {
				pendingContent += "\n"
			}
			pendingContent += msg.Content
			// Keep first non-empty reasoning content on merge
			if pendingReasoning == "" && msg.ReasoningContent != "" {
				pendingReasoning = msg.ReasoningContent
			}
			continue
		}

		// Role changed or non-mergeable — flush pending and handle this message
		flush()

		if !isMergeable {
			// Emit directly without buffering
			content := interface{}(msg.Content)
			if len(msg.Images) > 0 {
				content = p.buildMultiModalContent(msg.Content, msg.Images)
			}
			convertedMsg := map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			}
			if msg.ToolCallID != "" && p.config.Conversion.IncludeToolCallID {
				convertedMsg["tool_call_id"] = msg.ToolCallID
			}
			if msg.Role == "tool" && p.config.Conversion.ConvertToolRoleToUser {
				convertedMsg["role"] = "user"
			}
			if !skipReasoningHistory && msg.ReasoningContent != "" && reasoningField != "" {
				convertedMsg[reasoningField] = msg.ReasoningContent
			}
			if len(msg.ToolCalls) > 0 {
				convertedMsg["tool_calls"] = p.convertToolCalls(msg.ToolCalls)
			}
			converted = append(converted, convertedMsg)
			continue
		}

		// Start buffering a mergeable message
		pendingRole = msg.Role
		if len(msg.Images) > 0 {
			// Multi-modal content — emit immediately, don't buffer
			content := p.buildMultiModalContent(msg.Content, msg.Images)
			converted = append(converted, map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			})
			pendingRole = ""
		} else {
			pendingContent = msg.Content
			pendingReasoning = msg.ReasoningContent
		}
	}
	flush()

	_ = reasoning // reasoning effort is sent via provider/model-specific request params, not message fields

	return converted
}

func (p *GenericProvider) shouldSkipReasoningContentHistory() bool {
	// MiniMax expects reasoning_details to be a structured array, not a plain string.
	// Replaying historical ReasoningContent verbatim causes type mismatch 400s.
	if strings.EqualFold(p.config.Name, "minimax") &&
		strings.EqualFold(p.config.Conversion.ReasoningContentField, "reasoning_details") {
		return true
	}

	// ZAI (GLM models) may reject stale reasoning_content in message history when
	// the current request doesn't explicitly enable thinking, causing 400 errors.
	// Applies to both the general API ("zai") and the GLM Coding Plan ("zai-coding").
	if (strings.EqualFold(p.config.Name, "zai") || strings.EqualFold(p.config.Name, "zai-coding")) &&
		p.config.Conversion.ReasoningContentField != "" {
		return true
	}

	return false
}

func (p *GenericProvider) convertToolCalls(toolCalls []api.ToolCall) interface{} {
	if !p.config.Conversion.ArgumentsAsJSON {
		// For providers like Minimax that expect arguments as string,
		// ensure the JSON string is properly formatted and escaped
		converted := make([]map[string]interface{}, 0, len(toolCalls))
		for _, tc := range toolCalls {
			// Validate and clean the arguments JSON string
			arguments := tc.Function.Arguments
			if arguments != "" {
				// Try to parse and re-marshal to ensure it's valid JSON
				var parsed interface{}
				if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
					// Re-marshal to ensure proper formatting and escaping
					if remarshaled, err := json.Marshal(parsed); err == nil {
						arguments = string(remarshaled)
					}
					// If re-marshaling fails, keep original (it was valid)
				} else {
					// If parsing fails, fall back to empty object
					arguments = "{}"
				}
			}

			toolCallType := tc.Type
			// Force tool call type if specified (needed for providers like Mistral)
			if p.config.Conversion.ForceToolCallType != "" {
				toolCallType = p.config.Conversion.ForceToolCallType
			}

			converted = append(converted, map[string]interface{}{
				"id":   tc.ID,
				"type": toolCallType,
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": arguments,
				},
			})
		}
		return converted
	}

	// For providers that expect arguments as JSON object (original behavior)
	converted := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		function := map[string]interface{}{
			"name": tc.Function.Name,
		}

		if tc.Function.Arguments != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
				function["arguments"] = parsed
			} else {
				function["arguments"] = tc.Function.Arguments
			}
		}

		toolCallType := tc.Type
		// Force tool call type if specified (needed for providers like Mistral)
		if p.config.Conversion.ForceToolCallType != "" {
			toolCallType = p.config.Conversion.ForceToolCallType
		}

		converted = append(converted, map[string]interface{}{
			"id":       tc.ID,
			"type":     toolCallType,
			"function": function,
		})
	}

	return converted
}

// getModelCompletionLimit returns the max completion token limit for the current model/provider.
func (p *GenericProvider) getModelCompletionLimit() int {
	// First honor explicit config overrides.
	if limit := p.config.GetMaxCompletionLimit(p.model); limit > 0 {
		return limit
	}

	// Then apply provider/model-specific known limits.
	provider := strings.ToLower(p.config.Name)
	model := strings.ToLower(p.model)

	switch provider {
	case "openrouter":
		if strings.Contains(model, "gpt-5") {
			return 128000
		}
	case "minimax":
		if strings.Contains(model, "minimax-m2") {
			return 196608
		}
	}

	return 0
}
