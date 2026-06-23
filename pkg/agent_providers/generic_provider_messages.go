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

	// Inject cache_control breakpoints for providers that support prompt-prefix
	// caching (Anthropic via OpenRouter). Anthropic allows up to 4 breakpoints
	// per request. We use 3 of them:
	//
	//  1. System message — the largest static block; caches the system prompt.
	//  2. Last tool definition — caches the tool schema prefix (applied in
	//     buildChatRequest, not here).
	//  3. Last conversation message — caches the entire growing conversation
	//     prefix so that on the NEXT turn (or next tool-call iteration within
	//     the same turn), everything up to this point is a cache hit instead
	//     of being reprocessed from scratch. This is the highest-impact
	//     breakpoint for agentic workloads where the history grows every turn.
	//
	// Anthropic checks all previously cached prefixes on each request and uses
	// the longest match, so the last-message breakpoint from turn N becomes a
	// cache hit on turn N+1.
	// See: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
	if p.config.Conversion.CacheControl {
		// Breakpoint 1: system message.
		for i := range converted {
			if role, ok := converted[i]["role"].(string); ok && role == "system" {
				converted[i]["cache_control"] = map[string]interface{}{
					"type": "ephemeral",
				}
				break // only mark the first (and typically only) system message
			}
		}

		// Breakpoint 3 (of 4): last conversation message.
		// Skip if the conversation is too short (< 2 messages) or if the last
		// message is already the system message (avoid double-marking).
		if len(converted) >= 2 {
			lastIdx := len(converted) - 1
			if _, hasCacheControl := converted[lastIdx]["cache_control"]; !hasCacheControl {
				converted[lastIdx]["cache_control"] = map[string]interface{}{
					"type": "ephemeral",
				}
			}
		}
	}

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
