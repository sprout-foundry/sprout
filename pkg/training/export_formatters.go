// Package training provides utilities for exporting session data into
// training-ready formats (ShareGPT, OpenAI fine-tuning JSONL, Alpaca).
package training

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Export options / result
// ---------------------------------------------------------------------------

// export_formatters.go — the message normalization and the ShareGPT / OpenAI
// / Alpaca format builders, split out of export.go.

// flattenStandardMessages cleans and prepares a raw message slice for
// training export.
//
// System messages are stripped unless opts.IncludeSystem.
//
// When opts.StructuredTools is true, the OpenAI function-calling schema is
// preserved verbatim:
//   - assistant messages keep their ToolCalls arrays (not flattened to text)
//   - tool messages keep role:"tool" and their ToolCallID
//
// No deduplication is applied in this mode — structured messages must never
// be merged.
//
// When opts.StructuredTools is false (default), tool calls and results are
// flattened to text for models that don't understand function-calling:
//   - assistant ToolCalls are converted to readable text via toolCallsToText
//     (when opts.NoToolResults is true; otherwise left intact but still
//     non-structured for export purposes)
//   - tool messages are kept as role:"tool" through this function so the
//     format builders can decide how to render them. (They carry
//     placeholders when opts.NoToolResults is true.)
//
// Consecutive same-role messages are merged via deduplicateConsecutive,
// which never merges messages carrying the tool markers.
func flattenStandardMessages(messages []api.Message, opts ExportOptions) []api.Message {
	var result []api.Message
	for _, m := range messages {
		if m.Role == "system" && !opts.IncludeSystem {
			continue
		}

		if opts.StructuredTools {
			// Structured mode: keep everything as-is (assistant tool_calls,
			// tool role, tool_call_id). No flattening.
			result = append(result, m)
			continue
		}

		// Non-structured mode.
		if m.Role == "tool" {
			if opts.NoToolResults {
				// Compress to a placeholder but keep role:"tool" so format
				// builders can convert to user-role with a prefix.
				toolName := inferToolName(m)
				charCount := len(m.Content)
				result = append(result, api.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("[tool result: %s, %d chars]", toolName, charCount),
					ToolCallID: m.ToolCallID,
				})
			} else {
				result = append(result, m)
			}
			continue
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && opts.NoToolResults {
			text := toolCallsToText(m.ToolCalls, m.Content)
			result = append(result, api.Message{
				Role:    "assistant",
				Content: text,
			})
			continue
		}
		result = append(result, m)
	}

	// Pre-scan all message content for remote usernames (from /home/<user>
	// paths on other machines) so they can be redacted even when they appear
	// without the full path context (e.g. in command output).
	var allContent []string
	for i := range result {
		allContent = append(allContent, result[i].Content)
		for j := range result[i].ToolCalls {
			allContent = append(allContent, result[i].ToolCalls[j].Function.Arguments)
		}
	}
	SetRemoteUsernames(mergeUsernames(remoteUsernamesForRedaction, CollectRemoteUsernames(allContent)))

	// Apply credential redaction to all message content after cleaning.
	for i := range result {
		result[i].Content = RedactContent(result[i].Content)
	}

	// Redact PII in tool call arguments (file paths, usernames, etc.).
	for i := range result {
		for j := range result[i].ToolCalls {
			result[i].ToolCalls[j].Function.Arguments = RedactContent(result[i].ToolCalls[j].Function.Arguments)
		}
	}

	// Skip deduplication in structured mode — structured messages must
	// never be merged.
	if opts.StructuredTools {
		return result
	}
	return deduplicateConsecutive(result)
}

// deduplicateConsecutive merges consecutive messages with the same role.
//
// It guards against merging messages that carry tool markers
// (toolResultMarker or toolCallMarker): a tool-result message converted to
// user-role must never be merged with an adjacent real user message, and a
// tool-call-flattened assistant message must never be merged with an
// adjacent plain assistant message. This preserves conversation flow.
func deduplicateConsecutive(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return nil
	}
	deduped := []api.Message{messages[0]}
	for i := 1; i < len(messages); i++ {
		last := deduped[len(deduped)-1]
		cur := messages[i]
		if cur.Role == last.Role && !hasToolMarker(last) && !hasToolMarker(cur) {
			merged := last
			merged.Content = strings.TrimSpace(merged.Content + "\n\n" + strings.TrimSpace(cur.Content))
			deduped[len(deduped)-1] = merged
		} else {
			deduped = append(deduped, cur)
		}
	}
	return deduped
}

// hasToolMarker reports whether a message's content contains one of the tool
// markers that should prevent deduplication merging.
func hasToolMarker(m api.Message) bool {
	return strings.Contains(m.Content, toolResultMarker) ||
		strings.Contains(m.Content, toolCallMarker) ||
		strings.Contains(m.Content, "[tool result:")
}

// normalizeRole maps internal roles to training-friendly roles.
func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case "system", "user", "assistant":
		return role
	default:
		return "user"
	}
}

// ---------------------------------------------------------------------------
// Format builders
// ---------------------------------------------------------------------------

func buildShareGPT(states []agent.ConversationState, opts ExportOptions) ([]ShareGPTConversation, error) {
	conversations := make([]ShareGPTConversation, 0, len(states))
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		var msgs []ShareGPTMessage
		for _, m := range cleaned {
			msgs = append(msgs, toShareGPTMessage(m, opts))
		}

		if len(msgs) == 0 {
			continue
		}

		conversations = append(conversations, ShareGPTConversation{
			ID:       state.SessionID,
			Messages: msgs,
			Metadata: ShareGPTMetadata{
				SessionID:   state.SessionID,
				SessionName: state.Name,
				Source:      "sprout",
				TotalCost:   state.TotalCost,
				WorkingDir:  RedactContent(state.WorkingDirectory),
			},
		})
	}
	return conversations, nil
}

func buildOpenAI(states []agent.ConversationState, opts ExportOptions) ([]OpenAITrainingExample, error) {
	var examples []OpenAITrainingExample
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		var msgs []OpenAIMessage
		for _, m := range cleaned {
			msgs = append(msgs, toOpenAIMessage(m, opts))
		}

		if len(msgs) == 0 {
			continue
		}

		// Each session becomes one multi-turn training example.
		examples = append(examples, OpenAITrainingExample{Messages: msgs})

		// When IncludeSubagents is set, extract single-task examples
		// from run_subagent / run_parallel_subagents tool calls. These
		// are extracted from the raw (unflattened) messages because the
		// flattening step may convert tool messages to user-role text.
		if opts.IncludeSubagents {
			subExs := extractSubagentExamples(state)
			examples = append(examples, subExs...)
		}
	}
	return examples, nil
}

// toShareGPTMessage converts a cleaned api.Message into a ShareGPTMessage.
//
// In structured mode, tool messages keep role:"tool" with their
// tool_call_id, and assistant messages keep their tool_calls arrays.
//
// In non-structured mode, tool messages are converted to user-role messages
// with a "[Tool Result] " prefix so the conversation flow is preserved and
// the model learns to react to tool outputs. Tool messages are never
// dropped.
func toShareGPTMessage(m api.Message, opts ExportOptions) ShareGPTMessage {
	if m.Role == "tool" {
		if opts.StructuredTools {
			// Structured mode: keep role:"tool" and tool_call_id intact.
			return ShareGPTMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			}
		}
		// Non-structured: convert to user-role with prefix.
		return ShareGPTMessage{
			Role:    "user", // tool results become user messages for training
			Content: toolResultMarker + " " + m.Content,
		}
	}
	return ShareGPTMessage{
		Role:       normalizeRole(m.Role),
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
}

// toOpenAIMessage converts a cleaned api.Message into an OpenAIMessage.
// The conversion logic mirrors toShareGPTMessage — see its docs.
func toOpenAIMessage(m api.Message, opts ExportOptions) OpenAIMessage {
	if m.Role == "tool" {
		if opts.StructuredTools {
			return OpenAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			}
		}
		return OpenAIMessage{
			Role:    "user", // tool results become user messages for training
			Content: toolResultMarker + " " + m.Content,
		}
	}
	return OpenAIMessage{
		Role:       normalizeRole(m.Role),
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
}

func buildAlpaca(states []agent.ConversationState, opts ExportOptions) ([]AlpacaExample, error) {
	var examples []AlpacaExample
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		if len(cleaned) == 0 {
			continue
		}

		example := alpacaFromConversation(cleaned)
		if example != nil {
			examples = append(examples, *example)
		}
	}
	return examples, nil
}

// alpacaFromConversation heuristically converts a cleaned conversation into
// a single Alpaca example:
//   - First user message → instruction
//   - Intermediate conversation context → input
//   - Last assistant message → output
func alpacaFromConversation(messages []api.Message) *AlpacaExample {
	var firstUser, lastAssistant string
	for _, m := range messages {
		if m.Role == "user" && firstUser == "" {
			firstUser = strings.TrimSpace(m.Content)
		}
		if m.Role == "assistant" {
			lastAssistant = strings.TrimSpace(m.Content)
		}
	}

	if firstUser == "" || lastAssistant == "" {
		return nil
	}

	trimmedFirst := firstUser
	// Build the "input" from intermediate conversation context.
	var inputParts []string
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		trimmed := strings.TrimSpace(m.Content)
		if trimmed == trimmedFirst || trimmed == lastAssistant {
			continue
		}
		inputParts = append(inputParts, fmt.Sprintf("%s: %s", m.Role, m.Content))
	}

	return &AlpacaExample{
		Instruction: firstUser,
		Input:       strings.Join(inputParts, "\n"),
		Output:      lastAssistant,
	}
}

// ---------------------------------------------------------------------------
// Subagent example extraction
// ---------------------------------------------------------------------------
