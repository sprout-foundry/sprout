package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Per-turn checkpoint summary builders.
//
// These produce the structured text body that turn_checkpoints.go stores
// alongside each completed turn. The summaries are sprout-specific (they
// depend on sprout's "Tool call result for <name>:" formatting) and serve
// the local turn-checkpoint history feature, not the inter-turn structural
// compaction that now lives in seed.
//
// The two summarizers were originally methods on sprout's ConversationOptimizer.
// The optimizer dependency was incidental — they only needed message-walking
// and content-shape detection — so they moved to free functions when the
// optimizer became a seed-backed wrapper.

// maxSummaryEntries bounds the number of bullet points retained in a
// turn-checkpoint summary.
const maxSummaryEntries = 10

// maxSummaryEntryChars bounds each bullet's length. Long bullets are
// truncated head-style with an ellipsis.
const maxSummaryEntryChars = 180

// turnCompactionContext captures the latest user request and durable
// assistant note within a summary window so the wrapped output can echo
// them explicitly. Used by the structural-summary wrapping helper.
type turnCompactionContext struct {
	latestUserRequest   string
	latestAssistantNote string
}

// buildTurnCheckpointGoSummary produces the Go-based, rule-derived bullet
// list summarizing a window of messages. It is the deterministic equivalent
// of an LLM-summary call and is suitable for per-turn checkpoint storage
// (where calling an LLM on every turn would be wasteful).
func buildTurnCheckpointGoSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}

	ctx := extractTurnCompactionContext(messages)
	entries := make([]string, 0, maxSummaryEntries)
	seen := make(map[string]struct{}, maxSummaryEntries)
	add := func(entry string) {
		entry = normalizeSummaryEntry(entry)
		if entry == "" {
			return
		}
		if _, ok := seen[entry]; ok {
			return
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}

	for _, msg := range messages {
		if len(entries) >= maxSummaryEntries {
			break
		}
		switch msg.Role {
		case "user":
			add("User request: " + msg.Content)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				add(summarizeAssistantToolCalls(msg))
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if looksLikeDurableAssistantState(content) {
				add("Assistant outcome: " + content)
			}
		case "tool":
			summary, _ := summarizeToolMessage(msg)
			if summary == "" {
				continue
			}
			lowered := strings.ToLower(msg.Content)
			if strings.Contains(lowered, "error") || strings.Contains(lowered, "failed") {
				summary += " [error]"
			}
			add(summary)
		}
	}

	if len(entries) == 0 {
		return ""
	}

	body := strings.Join(entries, "\n")
	return wrapTurnCheckpointSummary(messages, body, ctx)
}

// buildTurnCheckpointActionableSummary produces a more detailed,
// action-oriented summary of a single turn. Used by the actionable-summary
// branch of checkpoint recording when the caller wants a richer structured
// report than the rule-based Go summary above.
func buildTurnCheckpointActionableSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var userRequest string
	actions := make([]string, 0, 12)
	assistantNotes := make([]string, 0, 5)
	fileChanges := make([]string, 0, 12)

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if userRequest == "" {
				userRequest = msg.Content
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				s := summarizeAssistantToolCalls(msg)
				if s != "" && len(actions) < cap(actions) {
					actions = append(actions, s)
				}
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if len(assistantNotes) < cap(assistantNotes) {
				assistantNotes = append(assistantNotes, content)
			}
		case "tool":
			s, _ := summarizeToolMessage(msg)
			if s == "" {
				continue
			}
			header := strings.SplitN(msg.Content, "\n", 2)[0]
			isFileChange := strings.Contains(header, "Tool call result for edit_file:") ||
				strings.Contains(header, "Tool call result for write_file:") ||
				strings.Contains(header, "Tool call result for write_structured_file:") ||
				strings.Contains(header, "Tool call result for patch_structured_file:")
			if isFileChange {
				if len(fileChanges) < cap(fileChanges) {
					fileChanges = append(fileChanges, s)
				}
			} else if len(actions) < cap(actions) {
				actions = append(actions, s)
			}
		}
	}

	var b strings.Builder
	if userRequest != "" {
		if len(userRequest) > 300 {
			userRequest = userRequest[:297] + "..."
		}
		b.WriteString("User request: ")
		b.WriteString(userRequest)
		b.WriteString("\n\n")
	}
	if len(actions) > 0 || len(fileChanges) > 0 {
		b.WriteString("Actions taken:\n")
		for _, a := range actions {
			b.WriteString("- ")
			b.WriteString(a)
			b.WriteString("\n")
		}
		for _, fc := range fileChanges {
			b.WriteString("- ")
			b.WriteString(fc)
			b.WriteString(" [file change]\n")
		}
		b.WriteString("\n")
	}
	if len(assistantNotes) > 0 {
		b.WriteString("State notes:\n")
		for _, note := range assistantNotes {
			if len(note) > 200 {
				note = note[:197] + "..."
			}
			b.WriteString("- ")
			b.WriteString(note)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(fileChanges) > 0 {
		b.WriteString("Files modified in this turn:\n")
		for _, fc := range fileChanges {
			b.WriteString("- ")
			b.WriteString(fc)
			b.WriteString("\n")
		}
	}

	result := strings.TrimSpace(b.String())
	if words := strings.Fields(result); len(words) > 300 {
		result = strings.Join(words[:297], " ") + "..."
	}
	return result
}

// extractTurnCompactionContext walks the message slice and pulls the most
// recent user request and the most recent durable-looking assistant note so
// they can be echoed in the wrapped summary header.
func extractTurnCompactionContext(messages []api.Message) turnCompactionContext {
	var ctx turnCompactionContext
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			ctx.latestUserRequest = normalizeSummaryEntry(msg.Content)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content != "" && looksLikeDurableAssistantState(content) {
				ctx.latestAssistantNote = normalizeSummaryEntry(content)
			}
		}
	}
	return ctx
}

// wrapTurnCheckpointSummary frames the raw bullet body with the standard
// "Compacted earlier conversation state:" header used by sprout's turn
// checkpoints. The shape matches what seed's structural compaction emits
// (see seed/core/structural_compaction.go) so both paths look the same to
// downstream consumers and to the model.
func wrapTurnCheckpointSummary(messages []api.Message, body string, ctx turnCompactionContext) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("Compacted earlier conversation state:\n")
	fmt.Fprintf(&b, "- Summarized %d earlier messages to preserve context headroom.\n", len(messages))
	if ctx.latestUserRequest != "" {
		// Past-tense framing: this is historical, not a live instruction.
		// Previously this asserted "work was still in progress" unconditionally,
		// which caused the model to occasionally re-anchor on the compacted
		// request as the active task even when newer messages had moved on.
		// Completion state is left to the model's reading of recent messages.
		b.WriteString("- Earlier compacted user request (historical): ")
		b.WriteString(ctx.latestUserRequest)
		b.WriteString("\n")
	}
	if ctx.latestAssistantNote != "" {
		b.WriteString("- Earlier compacted assistant state: ")
		b.WriteString(ctx.latestAssistantNote)
		b.WriteString("\n")
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			b.WriteString(line)
		} else {
			b.WriteString("- ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	b.WriteString("- Use newer messages for the exact current step-by-step state.")
	return strings.TrimSpace(b.String())
}

// normalizeSummaryEntry collapses whitespace and truncates oversized
// entries so each bullet stays within maxSummaryEntryChars.
func normalizeSummaryEntry(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	entry = strings.Join(strings.Fields(entry), " ")
	if len(entry) > maxSummaryEntryChars {
		entry = entry[:maxSummaryEntryChars-3] + "..."
	}
	return entry
}

// looksLikeDurableAssistantState heuristically detects assistant messages
// that describe a durable outcome (file changes, build/test results,
// resolved errors) versus chatter. Used to filter what gets quoted in a
// summary header.
func looksLikeDurableAssistantState(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	keywords := []string{
		"fixed", "updated", "changed", "implemented", "added", "removed",
		"found", "verified", "build", "test", "lint", "error", "failed",
		"pass", "resolved", "refactored",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Short assistant messages are usually status updates worth keeping.
	return len(content) < 220
}
