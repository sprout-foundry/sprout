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

	// Defense in depth: drop any tool-role messages whose tool_call_id has no
	// parent assistant tool_calls block. Strict-syntax providers (MiniMax,
	// DeepSeek) reject the whole request as "tool call result does not
	// follow tool call" otherwise. The same orphan can arise from manual
	// edits, restored sessions, or any other path that bypasses
	// BuildCheckpointCompactedMessages' guard. We only apply this to
	// strict-syntax providers because non-strict ones (OpenAI, Anthropic)
	// tolerate dropped tool results as long as the conversation stays
	// coherent.
	if p.requiresStrictToolCallSyntax() {
		converted = stripUnansweredToolCalls(converted)
		converted = dropOrphanToolResults(converted)
	}

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

// requiresStrictToolCallSyntax reports whether the configured provider
// enforces strict assistant/tool_call pairing for tool results. Returns
// true for MiniMax/DeepSeek and any other provider/model combo that the
// provider catalogue flags as strict.
func (p *GenericProvider) requiresStrictToolCallSyntax() bool {
	if p == nil || p.config == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(p.config.Name))
	if name == "minimax" || name == "deepseek" {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.model)), "minimax") {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.model)), "deepseek") {
		return true
	}
	return false
}

// dropOrphanToolResults walks a converted message slice and drops any
// tool-role message whose tool_call_id is not declared by the nearest
// preceding assistant message's tool_calls block. Used as a final
// invariant guard before sending to strict-syntax providers. Does NOT
// drop the parent assistant message — providers handle an empty trailing
// assistant fine, and dropping it risks removing important context (tool
// calls the model already saw, plus any reasoning attached to it).
//
// Parallel tool calls: an assistant may issue multiple tool_calls at once
// (e.g. c1, c2). The results come back as a contiguous block of tool
// messages: tool(c1), tool(c2). For the second tool message, the
// immediately-preceding entry is another tool message, not the assistant.
// The correct check is: walk backward past any consecutive tool messages
// to find the nearest assistant, then match tool_call_id against that
// assistant's tool_calls list.
func dropOrphanToolResults(converted []map[string]interface{}) []map[string]interface{} {
	if len(converted) == 0 {
		return converted
	}
	out := make([]map[string]interface{}, 0, len(converted))
	for i, entry := range converted {
		role, _ := entry["role"].(string)
		if role != "tool" {
			out = append(out, entry)
			continue
		}
		// An empty tool_call_id means the message can't be matched to a
		// specific parent — preserve it rather than treating it as a
		// guaranteed orphan. (Misconfigured providers with
		// IncludeToolCallID=false fall in this case; we don't want to
		// strip their tool results.)
		want, _ := entry["tool_call_id"].(string)
		if want == "" {
			out = append(out, entry)
			continue
		}
		// Find the nearest assistant by walking backward past any
		// interleaved tool messages (parallel tool-call block).
		orphan := true
		for j := i - 1; j >= 0; j-- {
			prev := converted[j]
			prevRole, _ := prev["role"].(string)
			if prevRole == "assistant" {
				tcs, _ := prev["tool_calls"].([]map[string]interface{})
				for _, tc := range tcs {
					if id, _ := tc["id"].(string); id == want {
						orphan = false
					}
				}
				break // matched or not; stop at the first assistant
			}
			if prevRole != "tool" {
				break // any other role boundary (user/system/...) breaks the block
			}
		}
		if orphan {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// stripUnansweredToolCalls removes tool_calls from assistant messages whose
// tool_call IDs have no corresponding tool result in the immediately
// following messages. Strict-syntax providers (MiniMax, DeepSeek) reject
// requests where an assistant message declares tool_calls but no tool
// results follow — HTTP 400 error 2013 "tool call result does not follow
// tool call". This happens when checkpoint compaction or structural
// compaction consumes tool results but leaves their parent assistant
// messages in the history.
//
// The function walks forward from each assistant+tool_calls message,
// collecting following tool results (contiguous block). If not every
// tool_call ID has a result, the tool_calls are stripped so the provider
// doesn't reject the message. The assistant content is preserved so the
// model retains the assistant's text contribution. Must run BEFORE
// dropOrphanToolResults — stripping tool_calls from a partial-parallel
// block can leave previously-matched tool results orphaned, and the
// dropOrphanToolResults pass cleans those up.
func stripUnansweredToolCalls(converted []map[string]interface{}) []map[string]interface{} {
	if len(converted) == 0 {
		return converted
	}

	result := make([]map[string]interface{}, 0, len(converted))

	for i := 0; i < len(converted); i++ {
		entry := converted[i]
		role, _ := entry["role"].(string)
		if role != "assistant" {
			result = append(result, entry)
			continue
		}

		tcs, hasTCs := entry["tool_calls"].([]map[string]interface{})
		if !hasTCs || len(tcs) == 0 {
			result = append(result, entry)
			continue
		}

		// Collect tool_call IDs declared by this assistant message.
		declaredIDs := make(map[string]bool, len(tcs))
		for _, tc := range tcs {
			if id, _ := tc["id"].(string); id != "" {
				declaredIDs[id] = false // false = no result found yet
			}
		}
		if len(declaredIDs) == 0 {
			result = append(result, entry)
			continue
		}

		// Scan forward through the contiguous tool-result block. Providers
		// with ConvertToolRoleToUser emit tool results as role "user" with
		// a tool_call_id field, so check both roles.
		for j := i + 1; j < len(converted); j++ {
			nextRole, _ := converted[j]["role"].(string)
			if nextRole == "tool" {
				// Standard tool role — always part of the result block.
			} else if nextRole == "user" {
				// ConvertToolRoleToUser case — only treat as a result if
				// it carries a tool_call_id. A plain user message ends the block.
				if _, hasTCID := converted[j]["tool_call_id"]; !hasTCID {
					break
				}
			} else {
				break
			}
			if tid, _ := converted[j]["tool_call_id"].(string); tid != "" {
				if _, ok := declaredIDs[tid]; ok {
					declaredIDs[tid] = true
				}
			}
		}

		// Check if all declared IDs have results.
		allAnswered := true
		for _, answered := range declaredIDs {
			if !answered {
				allAnswered = false
				break
			}
		}

		if allAnswered {
			result = append(result, entry)
			continue
		}

		// Some tool_calls are unanswered. Strip the tool_calls field
		// so the provider doesn't reject the message. Keep the content
		// so the assistant's text contribution survives.
		stripped := make(map[string]interface{}, len(entry))
		for k, v := range entry {
			if k == "tool_calls" {
				continue
			}
			stripped[k] = v
		}
		result = append(result, stripped)
	}

	return result
}
