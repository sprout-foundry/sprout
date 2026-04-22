package agent

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strings"
	"time"

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

// FileReadRecord tracks file reads to detect redundancy
type FileReadRecord struct {
	FilePath     string
	Content      string
	ContentHash  string
	Timestamp    time.Time
	MessageIndex int
}

// ShellCommandRecord tracks shell commands to detect redundancy
type ShellCommandRecord struct {
	Command      string
	Output       string
	OutputHash   string
	Timestamp    time.Time
	MessageIndex int
	IsTransient  bool // Commands like ls, find that become less relevant over time
}

type compactionContext struct {
	latestUserRequest   string
	latestAssistantNote string
}

// ConversationOptimizer manages conversation history optimization
type ConversationOptimizer struct {
	fileReads     map[string]*FileReadRecord     // filepath -> latest read record
	shellCommands map[string]*ShellCommandRecord // command -> latest execution record
	enabled       bool
	debug         bool
	client       api.ClientInterface // LLM client for generating summaries (nil = use Go fallback)
	providerName string              // Provider name for summary logging
	printLine    func(string)        // Console output callback (nil = silent)
}

// NewConversationOptimizer creates a new conversation optimizer
func NewConversationOptimizer(enabled bool, debug bool) *ConversationOptimizer {
	return &ConversationOptimizer{
		fileReads:     make(map[string]*FileReadRecord),
		shellCommands: make(map[string]*ShellCommandRecord),
		enabled:       enabled,
		debug:         debug,
	}
}

// OptimizeConversation optimizes the conversation history by removing redundant content
func (co *ConversationOptimizer) OptimizeConversation(messages []api.Message) []api.Message {
	if !co.enabled {
		return messages
	}

	// First pass: find the most recent read of each file
	for i, msg := range messages {
		co.trackFileRead(msg, i)
		co.trackShellCommand(msg, i)
	}

	// Second pass: optimize based on tracked data
	optimized := make([]api.Message, 0, len(messages))

	for i, msg := range messages {
		if co.isRedundantFileRead(msg, i) {
			// Replace with summary
			summary := co.createFileReadSummary(msg)
			rewritten := msg
			rewritten.Content = summary
			optimized = append(optimized, rewritten)
			if co.debug {
				fmt.Printf("\n[~] Optimized redundant file read: %s\n", co.extractFilePath(msg.Content))
			}
		} else if co.isRedundantShellCommand(msg, i) {
			// Replace with summary
			summary := co.createShellCommandSummary(msg)
			rewritten := msg
			rewritten.Content = summary
			optimized = append(optimized, rewritten)
			if co.debug {
				fmt.Printf("\n[~] Optimized redundant shell command: %s\n", co.extractShellCommand(msg.Content))
			}
		} else {
			optimized = append(optimized, msg)
		}
	}

	return optimized
}

// CompactConversation rewrites older middle history into durable summaries while
// preserving the opening task anchor and the recent causal chain intact.
// For large middle segments (>= 30 messages), it produces layered summaries at
// graduated detail levels (brief, summary, detailed).
func (co *ConversationOptimizer) CompactConversation(messages []api.Message) []api.Message {
	if !co.enabled || len(messages) < PruningConfig.Structural.MinMessagesToCompact {
		return messages
	}

	anchorEnd := co.compactionAnchorEnd(messages)
	recentStart := len(messages) - PruningConfig.Structural.RecentMessagesToKeep
	if recentStart <= anchorEnd {
		return messages
	}

	recentStart = co.adjustCompactionBoundary(messages, recentStart, anchorEnd)
	if recentStart-anchorEnd < PruningConfig.Structural.MinMiddleMessages {
		return messages
	}

	middle := messages[anchorEnd:recentStart]
	
	// Layered compaction for large middle segments (>= LayeredThreshold messages)
	if len(middle) >= LayeredThreshold {
		return co.compactConversationLayered(messages, anchorEnd, recentStart, middle)
	}
	
	// Single summary for smaller middle segments (original behavior)
	summary := co.buildLLMCompactionSummary(middle)
	if summary == "" {
		return messages
	}

	compacted := make([]api.Message, 0, anchorEnd+1+len(messages)-recentStart)
	compacted = append(compacted, messages[:anchorEnd]...)
	compacted = append(compacted, api.Message{
		Role:    "assistant",
		Content: summary,
	})
	compacted = append(compacted, messages[recentStart:]...)
	
	// FIX: Ensure we don't have consecutive assistant messages at the boundary.
	// If the summary is followed by an assistant message without tool_calls,
	// remove the following assistant message to avoid llama.cpp error:
	// "Cannot have 2 or more assistant messages at the end of the list"
	if len(compacted) >= 2 {
		summaryIdx := anchorEnd
		if summaryIdx < len(compacted) && compacted[summaryIdx].Role == "assistant" && len(compacted[summaryIdx].ToolCalls) == 0 {
			// Check if the next message is also an assistant without tool_calls
			if summaryIdx+1 < len(compacted) && compacted[summaryIdx+1].Role == "assistant" && len(compacted[summaryIdx+1].ToolCalls) == 0 {
				// Remove the duplicate assistant message (keep the summary, remove the original)
				if co.debug {
					fmt.Printf("[clean] Removed consecutive assistant at compaction boundary\n")
				}
				compacted = append(compacted[:summaryIdx+1], compacted[summaryIdx+2:]...)
			}
		}
	}
	
	return compacted
}

// compactConversationLayered creates multiple summary messages at graduated detail levels
// for large middle segments. This prevents over-aggressive single-summary compaction.
func (co *ConversationOptimizer) compactConversationLayered(messages []api.Message, anchorEnd, recentStart int, middle []api.Message) []api.Message {
	// Split middle into 3 layers: old-middle, mid-middle, recent-middle
	layerSize := len(middle) / 3
	if layerSize < MinLayerSize {
		layerSize = MinLayerSize
	}
	
	oldMiddleEnd := anchorEnd + layerSize
	midMiddleEnd := oldMiddleEnd + layerSize
	
	var summaries []api.Message
	
	// Old-middle: most condensed (brief)
	oldMiddle := messages[anchorEnd:oldMiddleEnd]
	briefSummary := co.buildLLMCompactionSummaryWithLimit(oldMiddle, BriefWordLimit, "brief")
	if briefSummary != "" {
		summaries = append(summaries, api.Message{
			Role:    "assistant",
			Content: briefSummary,
		})
	}
	
	// Mid-middle: medium detail (summary)
	midMiddle := messages[oldMiddleEnd:midMiddleEnd]
	summarySummary := co.buildLLMCompactionSummaryWithLimit(midMiddle, SummaryWordLimit, "summary")
	if summarySummary != "" {
		summaries = append(summaries, api.Message{
			Role:    "assistant",
			Content: summarySummary,
		})
	}
	
	// Recent-middle: higher detail (detailed)
	recentMiddle := messages[midMiddleEnd:recentStart]
	detailedSummary := co.buildLLMCompactionSummaryWithLimit(recentMiddle, DetailedWordLimit, "detailed")
	if detailedSummary != "" {
		summaries = append(summaries, api.Message{
			Role:    "assistant",
			Content: detailedSummary,
		})
	}
	
	// If no summaries were created, fall back to single summary
	if len(summaries) == 0 {
		summary := co.buildLLMCompactionSummary(middle)
		if summary == "" {
			return messages
		}
		summaries = append(summaries, api.Message{
			Role:    "assistant",
			Content: summary,
		})
	}
	
	// Build compacted message list
	compacted := make([]api.Message, 0, anchorEnd+len(summaries)+len(messages)-recentStart)
	compacted = append(compacted, messages[:anchorEnd]...)
	compacted = append(compacted, summaries...)
	compacted = append(compacted, messages[recentStart:]...)
	
	// FIX: Ensure we don't have consecutive assistant messages at the boundary.
	// Check if the last summary is followed by an assistant message without tool_calls
	if len(summaries) > 0 {
		lastSummaryIdx := anchorEnd + len(summaries) - 1
		if lastSummaryIdx+1 < len(compacted) {
			if compacted[lastSummaryIdx].Role == "assistant" && len(compacted[lastSummaryIdx].ToolCalls) == 0 &&
				compacted[lastSummaryIdx+1].Role == "assistant" && len(compacted[lastSummaryIdx+1].ToolCalls) == 0 {
				if co.debug {
					fmt.Printf("[clean] Removed consecutive assistant at layered compaction boundary\n")
				}
				compacted = append(compacted[:lastSummaryIdx+1], compacted[lastSummaryIdx+2:]...)
			}
		}
	}
	
	if co.debug {
		fmt.Printf("[layered] Layered compaction: %d messages → %d summary layers\n", len(middle), len(summaries))
	}
	
	return compacted
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
		if co.debug {
			fmt.Printf("\n[WARN] LLM compaction summary failed: %v, falling back to Go summary\n", err)
		}
		if co.printLine != nil {
			co.printLine(fmt.Sprintf("[WARN] LLM compaction failed (%v), using fallback summary", err))
		}
		return co.buildGoCompactionSummary(messages)
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		if co.debug {
			fmt.Printf("\n[WARN] LLM compaction returned empty response, falling back to Go summary\n")
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

// isRedundantFileRead checks if this message is a redundant file read
func (co *ConversationOptimizer) isRedundantFileRead(msg api.Message, index int) bool {
	if msg.Role != "tool" {
		return false
	}

	// Check if this is a file read result
	if !strings.Contains(msg.Content, "Tool call result for read_file:") {
		return false
	}

	filePath := co.extractFilePath(msg.Content)
	if filePath == "" {
		return false
	}

	// Check if we have the most recent read of this file
	if record, exists := co.fileReads[filePath]; exists {
		// Extract current content
		currentContent := co.extractFileContent(msg.Content)
		currentHash := co.hashContent(currentContent)

		// Only consider it redundant if:
		// 1. Content hasn't changed AND
		// 2. This is NOT the most recent read (index < record.MessageIndex) AND
		// 3. The gap to the most recent read is at least 15 messages (preserving more context)
		messageGap := record.MessageIndex - index
		if record.ContentHash == currentHash && index < record.MessageIndex && messageGap >= 15 {
			return true
		}
	}

	return false
}

// trackFileRead records a file read for future optimization
func (co *ConversationOptimizer) trackFileRead(msg api.Message, index int) {
	if msg.Role != "tool" || !strings.Contains(msg.Content, "Tool call result for read_file:") {
		return
	}

	filePath := co.extractFilePath(msg.Content)
	if filePath == "" {
		return
	}

	content := co.extractFileContent(msg.Content)
	hash := co.hashContent(content)

	// Always track the MOST RECENT read of each file
	// This ensures we preserve the latest read and optimize older ones
	co.fileReads[filePath] = &FileReadRecord{
		FilePath:     filePath,
		Content:      content,
		ContentHash:  hash,
		Timestamp:    time.Now(),
		MessageIndex: index,
	}
}

func (co *ConversationOptimizer) compactionAnchorEnd(messages []api.Message) int {
	anchorEnd := 0
	if len(messages) == 0 {
		return anchorEnd
	}

	if messages[0].Role == "system" {
		anchorEnd = 1
	}

	for i := anchorEnd; i < len(messages); i++ {
		if messages[i].Role != "user" {
			continue
		}
		anchorEnd = i + 1
		if i+1 < len(messages) && messages[i+1].Role == "assistant" && len(messages[i+1].ToolCalls) == 0 {
			anchorEnd = i + 2
		}
		break
	}

	if anchorEnd == 0 && len(messages) > 0 {
		anchorEnd = 1
	}
	return anchorEnd
}

func (co *ConversationOptimizer) adjustCompactionBoundary(messages []api.Message, recentStart, anchorEnd int) int {
	for recentStart > anchorEnd {
		if recentStart < len(messages) && messages[recentStart].Role == "tool" {
			recentStart--
			continue
		}
		if recentStart-1 >= anchorEnd && messages[recentStart-1].Role == "assistant" && len(messages[recentStart-1].ToolCalls) > 0 {
			recentStart--
			continue
		}
		break
	}
	return recentStart
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
		if co.debug {
			fmt.Printf("\n[WARN] LLM compaction summary failed: %v, falling back to Go summary\n", err)
		}
		if co.printLine != nil {
			co.printLine(fmt.Sprintf("[WARN] LLM compaction failed (%v), using fallback summary", err))
		}
		return co.buildGoCompactionSummary(messages)
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		if co.debug {
			fmt.Printf("\n[WARN] LLM compaction returned empty response, falling back to Go summary\n")
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

// extractFilePath extracts the file path from a tool call result message
func (co *ConversationOptimizer) extractFilePath(content string) string {
	// Pattern: "Tool call result for read_file: <filepath>"
	re := regexp.MustCompile(`Tool call result for read_file:\s*([^\s\n]+)`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractFileContent extracts the file content from a tool call result message
func (co *ConversationOptimizer) extractFileContent(content string) string {
	// Find the content after the file path
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ""
	}

	// Skip the first line (tool call result header) and join the rest
	return strings.Join(lines[1:], "\n")
}

// hashContent creates a hash of file content for comparison
func (co *ConversationOptimizer) hashContent(content string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(content)))
}

// createFileReadSummary creates a summary for a redundant file read
func (co *ConversationOptimizer) createFileReadSummary(msg api.Message) string {
	filePath := co.extractFilePath(msg.Content)
	content := co.extractFileContent(msg.Content)

	// Count lines and characters
	lines := strings.Split(strings.TrimSpace(content), "\n")
	lineCount := len(lines)
	charCount := len(content)

	// Determine file type
	fileType := "file"
	if strings.HasSuffix(filePath, ".go") {
		fileType = "Go file"
	} else if strings.HasSuffix(filePath, ".md") {
		fileType = "Markdown file"
	} else if strings.HasSuffix(filePath, ".json") {
		fileType = "JSON file"
	}

	return fmt.Sprintf("Tool call result for read_file: %s\n[OPTIMIZED] Previously read %s (%d lines, %d chars) - content unchanged since last read",
		filePath, fileType, lineCount, charCount)
}

// GetOptimizationStats returns statistics about optimization
func (co *ConversationOptimizer) GetOptimizationStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":          co.enabled,
		"tracked_files":    len(co.fileReads),
		"tracked_commands": len(co.shellCommands),
		"file_paths":       co.getTrackedFilePaths(),
		"shell_commands":   co.getTrackedCommands(),
	}
}

// getTrackedCommands returns list of tracked shell commands
func (co *ConversationOptimizer) getTrackedCommands() []string {
	commands := make([]string, 0, len(co.shellCommands))
	for command := range co.shellCommands {
		commands = append(commands, command)
	}
	return commands
}

// getTrackedFilePaths returns list of tracked file paths
func (co *ConversationOptimizer) getTrackedFilePaths() []string {
	paths := make([]string, 0, len(co.fileReads))
	for path := range co.fileReads {
		paths = append(paths, path)
	}
	return paths
}

// isRedundantShellCommand checks if this message is a redundant shell command
func (co *ConversationOptimizer) isRedundantShellCommand(msg api.Message, index int) bool {
	if msg.Role != "tool" {
		return false
	}

	// Check if this is a shell command result
	if !strings.Contains(msg.Content, "Tool call result for shell_command:") {
		return false
	}

	command := co.extractShellCommand(msg.Content)
	if command == "" {
		return false
	}

	// Check if we have a more recent execution of this command
	if record, exists := co.shellCommands[command]; exists {
		// This is an OLDER execution if there's a newer one
		if index < record.MessageIndex {
			return true // Mark as stale since there's a newer execution
		}
	}

	return false
}

// trackShellCommand records a shell command execution for future optimization
func (co *ConversationOptimizer) trackShellCommand(msg api.Message, index int) {
	if msg.Role != "tool" || !strings.Contains(msg.Content, "Tool call result for shell_command:") {
		return
	}

	command := co.extractShellCommand(msg.Content)
	if command == "" {
		return
	}

	output := co.extractShellOutput(msg.Content)
	hash := co.hashContent(output)
	isTransient := co.isTransientCommand(command)

	co.shellCommands[command] = &ShellCommandRecord{
		Command:      command,
		Output:       output,
		OutputHash:   hash,
		Timestamp:    time.Now(),
		MessageIndex: index,
		IsTransient:  isTransient,
	}
}

// extractShellCommand extracts the shell command from a tool call result message
func (co *ConversationOptimizer) extractShellCommand(content string) string {
	// Pattern: "Tool call result for shell_command: <command>"
	re := regexp.MustCompile(`Tool call result for shell_command:\s*([^\n]+)`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractShellOutput extracts the shell command output from a tool call result message
func (co *ConversationOptimizer) extractShellOutput(content string) string {
	// Find the output after the command line
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ""
	}

	// Skip the first line (tool call result header) and join the rest
	return strings.Join(lines[1:], "\n")
}

// isTransientCommand checks if a command is transient (exploration commands that become stale)
func (co *ConversationOptimizer) isTransientCommand(command string) bool {
	transientPatterns := []string{
		"ls", "find", "grep", "tree", "pwd", "whoami", "date", "ps",
		"df", "du", "which", "whereis", "locate", "file", "stat",
	}

	cmdLower := strings.ToLower(command)
	for _, pattern := range transientPatterns {
		if strings.HasPrefix(cmdLower, pattern+" ") || cmdLower == pattern {
			return true
		}
	}
	return false
}

// createShellCommandSummary creates a summary for a redundant shell command
func (co *ConversationOptimizer) createShellCommandSummary(msg api.Message) string {
	command := co.extractShellCommand(msg.Content)
	output := co.extractShellOutput(msg.Content)

	// Count lines and characters in output
	lines := strings.Split(strings.TrimSpace(output), "\n")
	lineCount := len(lines)
	charCount := len(output)

	// Determine command type
	commandType := "command"
	if co.isTransientCommand(command) {
		commandType = "exploration command"
	}

	return fmt.Sprintf("Tool call result for shell_command: %s\n[STALE] Earlier execution of %s (%d lines output, %d chars) - see latest execution for current state",
		command, commandType, lineCount, charCount)
}

// InvalidateFile clears cached data for a specific file when it's modified
// This ensures stale metadata (like line counts) doesn't mislead the model
func (co *ConversationOptimizer) InvalidateFile(filePath string) {
	if filePath == "" {
		return
	}
	if co.debug {
		fmt.Printf("\n[~] Invalidating cached file data: %s\n", filePath)
	}
	delete(co.fileReads, filePath)
}

// Reset clears all optimization state
func (co *ConversationOptimizer) Reset() {
	co.fileReads = make(map[string]*FileReadRecord)
	co.shellCommands = make(map[string]*ShellCommandRecord)
}

// SetLLMClient configures the optimizer to use an LLM for compaction summaries.
// If client is nil, the optimizer falls back to the Go-based summary builder.
func (co *ConversationOptimizer) SetLLMClient(client api.ClientInterface, provider string, printLine func(string)) {
	co.client = client
	co.providerName = provider
	co.printLine = printLine
}

// SetEnabled enables or disables optimization
func (co *ConversationOptimizer) SetEnabled(enabled bool) {
	co.enabled = enabled
}

// IsEnabled returns whether optimization is enabled
func (co *ConversationOptimizer) IsEnabled() bool {
	return co.enabled
}


