package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Layered compaction constants for graduated detail levels.
const (
	LayeredThreshold    = 30  // Minimum middle messages to trigger layered compaction
	MinLayerSize        = 10  // Minimum messages per layer
	BriefWordLimit      = 150 // Word limit for oldest (brief) layer
	SummaryWordLimit    = 250 // Word limit for middle (summary) layer
	DetailedWordLimit   = 350 // Word limit for newest (detailed) layer
)

// Observation masking constants for SP-024: replace consumed tool results
// with compact placeholders to prevent context bloat.
const (
	observationMaskMaxChars = 3000 // Only mask tool results larger than this
	observationMaskKeepLast = 5    // Keep the last N tool results unmasked
)

type compactionContext struct {
	latestUserRequest   string
	latestAssistantNote string
}

// buildActionableSummary creates a more detailed, actionable summary of a turn,
// designed for per-turn checkpoint use. It extracts the user's original request,
// actions taken (tool results), what the assistant reported, and current state.
// Kept under ~300 words.
func (co *ConversationOptimizer) buildActionableSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var userRequest string
	var actions []string
	var assistantNotes []string
	var fileChanges []string
	maxEntries := 12

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if userRequest == "" {
				userRequest = msg.Content
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				summary := summarizeAssistantToolCalls(msg)
				if summary != "" && len(actions) < maxEntries {
					actions = append(actions, summary)
				}
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if len(assistantNotes) < 5 {
				assistantNotes = append(assistantNotes, content)
			}
		case "tool":
			summary, _ := summarizeToolMessage(msg)
			if summary == "" {
				continue
			}
			// Detect file-changing tools from the content prefix
			header := strings.Split(msg.Content, "\n")[0]
			isFileChange := strings.Contains(header, "Tool call result for edit_file:") ||
				strings.Contains(header, "Tool call result for write_file:") ||
				strings.Contains(header, "Tool call result for write_structured_file:") ||
				strings.Contains(header, "Tool call result for patch_structured_file:")
			if isFileChange {
				if len(fileChanges) < maxEntries {
					fileChanges = append(fileChanges, summary)
				}
			} else {
				if len(actions) < maxEntries {
					actions = append(actions, summary)
				}
			}
		}
	}

	var b strings.Builder

	// User's original request
	if userRequest != "" {
		if len(userRequest) > 300 {
			userRequest = userRequest[:297] + "..."
		}
		b.WriteString("User request: ")
		b.WriteString(userRequest)
		b.WriteString("\n\n")
	}

	// Actions taken
	if len(actions) > 0 || len(fileChanges) > 0 {
		b.WriteString("Actions taken:")
		b.WriteString("\n")
		for _, action := range actions {
			b.WriteString("- ")
			b.WriteString(action)
			b.WriteString("\n")
		}
		for _, fc := range fileChanges {
			b.WriteString("- ")
			b.WriteString(fc)
			b.WriteString(" [file change]")
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Assistant notes about state
	if len(assistantNotes) > 0 {
		b.WriteString("State notes:")
		b.WriteString("\n")
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

	// Files changed summary
	if len(fileChanges) > 0 {
		b.WriteString("Files modified in this turn:")
		b.WriteString("\n")
		for _, fc := range fileChanges {
			b.WriteString("- ")
			b.WriteString(fc)
			b.WriteString("\n")
		}
	}

	result := strings.TrimSpace(b.String())
	// Keep under ~300 words
	words := strings.Fields(result)
	if len(words) > 300 {
		trimmed := strings.Join(words[:297], " ") + "..."
		result = trimmed
	}
	return result
}

// buildLLMCompactionSummary generates a compaction summary using the LLM.
// It falls back to buildGoCompactionSummary if the LLM client is unavailable or the call fails.
// Uses proportional truncation based on message count and is checkpoint-aware.
func (co *ConversationOptimizer) buildLLMCompactionSummary(messages []api.Message) string {
	if co.client == nil {
		return co.buildGoCompactionSummary(messages)
	}

	n := len(messages)
	context := co.extractCompactionContext(messages)

	// Build compact text representation of the middle messages
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("[user] ")
			b.WriteString(msg.Content)
			b.WriteString("\n")
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				b.WriteString("[assistant/tool_calls] ")
				b.WriteString(summarizeAssistantToolCalls(msg))
				b.WriteString("\n")
				continue
			}
			// Check if this is a checkpoint summary
			content := strings.TrimSpace(msg.Content)
			if co.isCheckpointSummary(content) {
				b.WriteString("[checkpoint summary] ")
				b.WriteString(content)
				b.WriteString("\n")
				continue
			}
			if content != "" {
				b.WriteString("[assistant] ")
				b.WriteString(content)
				b.WriteString("\n")
			}
		case "tool":
			summary, _ := summarizeToolMessage(msg)
			if summary != "" {
				b.WriteString("[tool] ")
				b.WriteString(summary)
				b.WriteString("\n")
			}
		}
	}
	compactText := b.String()
	if compactText == "" {
		return co.buildGoCompactionSummary(messages)
	}

	// Proportional truncation based on message count: maxChars = min(n * 400, 32000)
	maxChars := n * 400
	if maxChars > 32000 {
		maxChars = 32000
	}
	if len(compactText) > maxChars {
		compactText = compactText[:maxChars] + "\n[...truncated...]"
	}

	if co.printLine != nil {
		co.printLine(fmt.Sprintf("\n[~] Compacting conversation context (%d messages → LLM summary)...", n))
	}

	systemMsg := api.Message{
		Role: "system",
		Content: "You are a conversation context summarizer. Summarize the following conversation segment concisely as a reference note for the AI agent continuing this session.\n\n" +
			"Rules:\n" +
			"- Preserve: what files were read/modified, what errors occurred, what the current state was\n" +
			"- Explicitly preserve the latest user request that appears in the compacted segment\n" +
			"- Explicitly state whether the work was still in progress at the end of the compacted segment\n" +
			"- Do NOT add planning, suggestions, or \"next steps\"\n" +
			"- Respond in English only\n" +
			"- Keep under 600 words\n" +
			"- Use a neutral, factual tone",
	}
	userMsg := api.Message{
		Role:    "user",
		Content: compactText,
		}

	resp, err := co.client.SendChatRequest([]api.Message{systemMsg, userMsg}, nil, "", false)
	if err != nil {
		if co.debug && co.printLine != nil {
			co.printLine(fmt.Sprintf("\n[WARN] LLM compaction summary failed: %v, falling back to Go summary\n", err))
		}
		if co.printLine != nil {
			co.printLine(fmt.Sprintf("[WARN] LLM compaction failed (%v), using fallback summary", err))
		}
		return co.buildGoCompactionSummary(messages)
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		if co.debug && co.printLine != nil {
			co.printLine("\n[WARN] LLM compaction returned empty response, falling back to Go summary\n")
		}
		return co.buildGoCompactionSummary(messages)
	}

	llmSummary := strings.TrimSpace(resp.Choices[0].Message.Content)

	wordCount := len(strings.Fields(llmSummary))
	if co.printLine != nil {
		co.printLine(fmt.Sprintf("[OK] Context compacted: %d messages → %d-word LLM summary", n, wordCount))
	}

	return co.wrapCompactionSummary(messages, llmSummary, context)
}

// buildLLMCompactionSummaryWithLimit generates a compaction summary with a specified word limit
// and detail level hint. Used for layered compaction.
func (co *ConversationOptimizer) buildLLMCompactionSummaryWithLimit(messages []api.Message, maxWords int, detailLevel string) string {
	if co.client == nil {
		return co.buildGoCompactionSummary(messages)
	}

	n := len(messages)
	context := co.extractCompactionContext(messages)

	// Build compact text representation of the middle messages
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("[user] ")
			b.WriteString(msg.Content)
			b.WriteString("\n")
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				b.WriteString("[assistant/tool_calls] ")
				b.WriteString(summarizeAssistantToolCalls(msg))
				b.WriteString("\n")
				continue
			}
			// Check if this is a checkpoint summary
			content := strings.TrimSpace(msg.Content)
			if co.isCheckpointSummary(content) {
				b.WriteString("[checkpoint summary] ")
				b.WriteString(content)
				b.WriteString("\n")
				continue
			}
			if content != "" {
				b.WriteString("[assistant] ")
				b.WriteString(content)
				b.WriteString("\n")
			}
		case "tool":
			summary, _ := summarizeToolMessage(msg)
			if summary != "" {
				b.WriteString("[tool] ")
				b.WriteString(summary)
				b.WriteString("\n")
			}
		}
	}
	compactText := b.String()
	if compactText == "" {
		return co.buildGoCompactionSummary(messages)
	}

	// Proportional truncation based on message count
	maxChars := n * 400
	if maxChars > 32000 {
		maxChars = 32000
	}
	if len(compactText) > maxChars {
		compactText = compactText[:maxChars] + "\n[...truncated...]"
	}

	if co.printLine != nil {
		co.printLine(fmt.Sprintf("\n[~] Compacting %d messages → %s LLM summary (max %d words)...", n, detailLevel, maxWords))
	}

	systemMsg := api.Message{
		Role: "system",
		Content: fmt.Sprintf("You are a conversation context summarizer. Summarize the following conversation segment concisely as a reference note for the AI agent continuing this session.\n\n"+
			"Detail level: %s (target ~%d words)\n\n"+
			"Rules:\n"+
			"- Preserve: what files were read/modified, what errors occurred, what the current state was\n"+
			"- Explicitly preserve the latest user request that appears in the compacted segment\n"+
			"- Explicitly state whether the work was still in progress at the end of the compacted segment\n"+
			"- Do NOT add planning, suggestions, or \"next steps\"\n"+
			"- Respond in English only\n"+
			"- Keep under %d words\n"+
			"- Use a neutral, factual tone", detailLevel, maxWords, maxWords),
	}
	userMsg := api.Message{
		Role:    "user",
		Content: compactText,
	}

	resp, err := co.client.SendChatRequest([]api.Message{systemMsg, userMsg}, nil, "", false)
	if err != nil {
		if co.debug && co.printLine != nil {
			co.printLine(fmt.Sprintf("\n[WARN] LLM compaction summary failed: %v, falling back to Go summary\n", err))
		}
		if co.printLine != nil {
			co.printLine(fmt.Sprintf("[WARN] LLM compaction failed (%v), using fallback summary", err))
		}
		return co.buildGoCompactionSummary(messages)
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		if co.debug && co.printLine != nil {
			co.printLine("\n[WARN] LLM compaction returned empty response, falling back to Go summary\n")
		}
		return co.buildGoCompactionSummary(messages)
	}

	llmSummary := strings.TrimSpace(resp.Choices[0].Message.Content)

	wordCount := len(strings.Fields(llmSummary))
	if co.printLine != nil {
		co.printLine(fmt.Sprintf("[OK] %s compaction: %d messages → %d-word LLM summary", detailLevel, n, wordCount))
	}

	return co.wrapCompactionSummaryWithLevel(messages, llmSummary, context, detailLevel)
}

func (co *ConversationOptimizer) buildGoCompactionSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}

	limit := PruningConfig.Structural.MaxSummaryEntries
	if limit <= 0 {
		limit = 8
	}
	context := co.extractCompactionContext(messages)

	entries := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	addEntry := func(entry string) {
		entry = co.normalizeSummaryEntry(entry)
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
		switch msg.Role {
		case "user":
			addEntry("User request: " + msg.Content)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				addEntry(summarizeAssistantToolCalls(msg))
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if looksLikeDurableAssistantState(content) {
				addEntry("Assistant outcome: " + content)
			}
		case "tool":
			summary, _ := summarizeToolMessage(msg)
			if summary == "" {
				continue
			}
			if strings.Contains(strings.ToLower(msg.Content), "error") || strings.Contains(strings.ToLower(msg.Content), "failed") {
				summary += " [error]"
			}
			addEntry(summary)
		}
		if len(entries) >= limit {
			break
		}
	}

	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(entry)
		b.WriteString("\n")
	}
	return co.wrapCompactionSummary(messages, strings.TrimSpace(b.String()), context)
}

func (co *ConversationOptimizer) normalizeSummaryEntry(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	entry = strings.Join(strings.Fields(entry), " ")
	maxChars := PruningConfig.Structural.MaxEntryChars
	if maxChars <= 0 {
		maxChars = 180
	}
	if len(entry) > maxChars {
		entry = entry[:maxChars-3] + "..."
	}
	return entry
}

func (co *ConversationOptimizer) extractCompactionContext(messages []api.Message) compactionContext {
	var context compactionContext
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			context.latestUserRequest = co.normalizeSummaryEntry(msg.Content)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if looksLikeDurableAssistantState(content) {
				context.latestAssistantNote = co.normalizeSummaryEntry(content)
			}
		}
	}
	return context
}

func (co *ConversationOptimizer) wrapCompactionSummary(messages []api.Message, body string, context compactionContext) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	var result strings.Builder
	result.WriteString("Compacted earlier conversation state:\n")
	result.WriteString(fmt.Sprintf("- Summarized %d earlier messages to preserve context headroom.\n", len(messages)))
	if context.latestUserRequest != "" {
		result.WriteString("- Latest compacted user request: ")
		result.WriteString(context.latestUserRequest)
		result.WriteString("\n")
		// Checkpoint summaries intentionally default to "still in progress" so a
		// compacted completed turn is not mistaken for the current live task. Newer
		// full-fidelity messages remain the source of truth for exact completion state.
		result.WriteString("- Status at compaction time: work was still in progress; newer messages continue from this task.\n")
	}
	if context.latestAssistantNote != "" {
		result.WriteString("- Latest compacted assistant state: ")
		result.WriteString(context.latestAssistantNote)
		result.WriteString("\n")
	}

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			result.WriteString(line)
		} else {
			result.WriteString("- ")
			result.WriteString(line)
		}
		result.WriteString("\n")
	}
	result.WriteString("- Use newer messages for the exact current step-by-step state.")

	return strings.TrimSpace(result.String())
}

// wrapCompactionSummaryWithLevel wraps a summary with the standard header,
// adapted for layered compaction with a detail level indicator.
func (co *ConversationOptimizer) wrapCompactionSummaryWithLevel(messages []api.Message, body string, context compactionContext, detailLevel string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	var result strings.Builder

	// Add detail level indicator to the header
	switch detailLevel {
	case "brief":
		result.WriteString("Compacted earlier conversation state (brief):\n")
	case "summary":
		result.WriteString("Compacted earlier conversation state (summary):\n")
	case "detailed":
		result.WriteString("Compacted earlier conversation state (detailed):\n")
	default:
		result.WriteString("Compacted earlier conversation state:\n")
	}

	result.WriteString(fmt.Sprintf("- Summarized %d earlier messages to preserve context headroom.\n", len(messages)))
	if context.latestUserRequest != "" {
		result.WriteString("- Latest compacted user request: ")
		result.WriteString(context.latestUserRequest)
		result.WriteString("\n")
		result.WriteString("- Status at compaction time: work was still in progress; newer messages continue from this task.\n")
	}
	if context.latestAssistantNote != "" {
		result.WriteString("- Latest compacted assistant state: ")
		result.WriteString(context.latestAssistantNote)
		result.WriteString("\n")
	}

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			result.WriteString(line)
		} else {
			result.WriteString("- ")
			result.WriteString(line)
		}
		result.WriteString("\n")
	}
	result.WriteString("- Use newer messages for the exact current step-by-step state.")

	return strings.TrimSpace(result.String())
}

// isCheckpointSummary detects if an assistant message content is a checkpoint summary.
// Checkpoint summaries contain "Compacted earlier conversation state:" or use standard
// checkpoint phrasing. This heuristic is intentionally conservative — false positives
// (treating a regular assistant message as a checkpoint summary) only affect how the
// message is represented in the LLM compaction prompt text (prefixed with
// "[checkpoint summary]" instead of "[assistant]"), not whether the message is kept
// or removed. The LLM gets slightly different context but no data is lost.
func (co *ConversationOptimizer) isCheckpointSummary(content string) bool {
	if content == "" {
		return false
	}

	// Direct check for checkpoint header
	if strings.Contains(content, "Compacted earlier conversation state:") {
		return true
	}

	// Check for common checkpoint summary patterns
	contentLower := strings.ToLower(content)
	checkpointIndicators := []string{
		"summarized", "compacted", "earlier conversation",
		"latest compacted user request", "status at compaction time",
	}

	for _, indicator := range checkpointIndicators {
		if strings.Contains(contentLower, indicator) {
			return true
		}
	}

	return false
}

func looksLikeDurableAssistantState(content string) bool {
	contentLower := strings.ToLower(strings.TrimSpace(content))
	if contentLower == "" {
		return false
	}
	keywords := []string{
		"fixed", "updated", "changed", "implemented", "added", "removed",
		"found", "verified", "build", "test", "lint", "error", "failed",
		"pass", "resolved", "refactored",
	}
	for _, keyword := range keywords {
		if strings.Contains(contentLower, keyword) {
			return true
		}
	}
	return len(content) < 220
}

// mergeLayeredSummaries combines up to three graduated summaries into one
// assistant message body with clear section headers. Returns "" only when
// all three inputs are empty.
func (co *ConversationOptimizer) mergeLayeredSummaries(brief, summary, detailed string, totalMiddle int) string {
	var b strings.Builder
	b.WriteString("[Context compaction — layered summary]\n\n")

	wrote := false

	if brief != "" {
		b.WriteString("### Earlier activities (brief)\n")
		b.WriteString(brief)
		b.WriteString("\n\n")
		wrote = true
	}
	if summary != "" {
		b.WriteString("### Mid-session activities (summary)\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
		wrote = true
	}
	if detailed != "" {
		b.WriteString("### Recent activities (detailed)\n")
		b.WriteString(detailed)
		b.WriteString("\n\n")
		wrote = true
	}

	if !wrote {
		return ""
	}

	b.WriteString(fmt.Sprintf("- Summarized %d earlier messages across 3 graduated detail layers.", totalMiddle))
	return strings.TrimSpace(b.String())
}

// maskConsumedToolResults replaces large consumed tool results with compact
// placeholders. A tool result is "consumed" when the model has produced a
// subsequent assistant message after seeing it. We keep the last N tool
// results unmasked so the model still has recent context.
func (co *ConversationOptimizer) maskConsumedToolResults(messages []api.Message) []api.Message {
	if len(messages) < 3 {
		return messages
	}

	// Find the index of the last assistant message (the model's most recent response).
	// Everything before that point is "consumed".
	var lastAssistantIndex int
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantIndex = i
			break
		}
	}

	if lastAssistantIndex == 0 {
		return messages
	}

	// Count tool messages before the last assistant message.
	// We'll keep the last `observationMaskKeepLast` unmasked.
	var consumedToolIndices []int
	for i := 0; i < lastAssistantIndex; i++ {
		if messages[i].Role == "tool" {
			consumedToolIndices = append(consumedToolIndices, i)
		}
	}

	// How many to mask (all except the last N).
	maskCount := len(consumedToolIndices) - observationMaskKeepLast
	if maskCount <= 0 {
		return messages
	}

	// Build result — replace content for the first maskCount consumed tools.
	result := make([]api.Message, len(messages))
	copy(result, messages)

	for _, idx := range consumedToolIndices[:maskCount] {
		msg := messages[idx]
		content := msg.Content

		// Only mask if content is large enough.
		if len(content) <= observationMaskMaxChars {
			continue
		}

		// Extract tool name.
		toolName := co.extractToolName(msg)
		lineCount := strings.Count(content, "\n") + 1

		placeholder := fmt.Sprintf("[PREVIOUS RESULT: %s, %d chars, %d lines]",
			toolName, len(content), lineCount)

		rewritten := msg
		rewritten.Content = placeholder
		result[idx] = rewritten

		if co.debug && co.printLine != nil {
			co.printLine(fmt.Sprintf("[~] Masked consumed tool result: %s (%d chars → %s)",
				toolName, len(content), placeholder))
		}
	}

	return result
}

// extractToolName extracts the tool name from a tool result message.
func (co *ConversationOptimizer) extractToolName(msg api.Message) string {
	content := msg.Content
	// Check for "Tool call result for <name>:" prefix (used by optimizer dedup)
	if idx := strings.Index(content, "Tool call result for "); idx >= 0 {
		rest := content[idx+len("Tool call result for "):]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			return strings.TrimSpace(rest[:colon])
		}
	}
	// Fallback: use ToolCallID
	if msg.ToolCallID != "" {
		return msg.ToolCallID
	}
	return "unknown"
}
