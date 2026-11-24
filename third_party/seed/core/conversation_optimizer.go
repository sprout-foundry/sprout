package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCategory classifies a tool for optimization purposes.
type ToolCategory int

const (
	// ToolCategoryUnknown means the optimizer should skip this tool.
	ToolCategoryUnknown ToolCategory = iota
	// ToolCategoryFileRead indicates a tool that reads file contents.
	ToolCategoryFileRead
	// ToolCategoryShellCommand indicates a tool that runs shell commands.
	ToolCategoryShellCommand
)

// maxFileReadRecords bounds the number of historical reads tracked per file.
// This keeps the dedup comparison window O(n) amortized.
const maxFileReadRecords = 5

// maxShellCmdRecords bounds the number of unique outputs tracked per command.
// Transient commands that produce ever-varying output (e.g., `ls` in a busy dir)
// could otherwise grow the dedup map without bound.
const maxShellCmdRecords = 10

// observationMaskMaxChars is the smallest tool-result body that observation
// masking will replace with a placeholder. Smaller results are kept verbatim;
// the masking overhead isn't worth their savings.
const observationMaskMaxChars = 3000

// observationMaskKeepLast is the number of most-recent tool results that stay
// unmasked regardless of size. The model needs fresh context to reason about
// the current step.
const observationMaskKeepLast = 5

// ConversationOptimizerOptions configures the optimizer.
type ConversationOptimizerOptions struct {
	// Enabled enables optimization. When false, OptimizeConversation is a no-op.
	Enabled bool
	// KnownToolFn classifies tool names. Return ToolCategoryUnknown to skip a tool.
	// If nil, the optimizer treats all tools as unknown (no optimization).
	// Must be deterministic: the same tool name should always return the same category.
	KnownToolFn func(name string) ToolCategory
}

// ConversationOptimizer reduces redundant conversation history by deduplicating
// repeated file reads and transient shell command outputs. It mutates the
// provided message slice in place (replacing tool-result Content with placeholders)
// but is safe to use because prepareMessages() only ever passes it ephemeral copies
// of state — the stored conversation is never modified.
type ConversationOptimizer struct {
	enabled     bool
	knownToolFn func(name string) ToolCategory
}

// NewConversationOptimizer creates a new optimizer from the given options.
func NewConversationOptimizer(opts ConversationOptimizerOptions) *ConversationOptimizer {
	return &ConversationOptimizer{
		enabled:     opts.Enabled,
		knownToolFn: opts.KnownToolFn,
	}
}

// OptimizeOptions controls which optimization passes
// OptimizeConversationWithOptions performs. The zero value disables the
// "mask consumed tool results" pass — callers that need the legacy behavior
// (mask always on) should use OptimizeConversation instead.
type OptimizeOptions struct {
	// MaskConsumedToolResults, when true, replaces large already-consumed
	// tool results with compact placeholders. When false, large tool
	// results pass through verbatim — useful below context-pressure
	// thresholds where the model benefits from seeing the full output it
	// previously read instead of having to re-read.
	MaskConsumedToolResults bool
}

// OptimizeConversation processes the message list, replacing redundant file reads
// and shell command outputs with compact placeholders, then masking large
// consumed tool results to bound context bloat from chatty tools. Returns the
// (possibly modified) message slice.
//
// Equivalent to OptimizeConversationWithOptions(messages,
// OptimizeOptions{MaskConsumedToolResults: true}); kept for backward
// compatibility with callers that haven't migrated.
func (opt *ConversationOptimizer) OptimizeConversation(messages []Message) []Message {
	return opt.OptimizeConversationWithOptions(messages, OptimizeOptions{MaskConsumedToolResults: true})
}

// OptimizeConversationWithReport is OptimizeConversationWithOptions that
// additionally reports whether the dedup pass replaced any tool-result
// content with a placeholder. Callers that persist the result (see
// prepareMessages) use the flag to skip a state write when nothing changed.
func (opt *ConversationOptimizer) OptimizeConversationWithReport(messages []Message, opts OptimizeOptions) ([]Message, bool) {
	return opt.optimize(messages, opts)
}

// OptimizeConversationWithOptions is OptimizeConversation with explicit per-pass
// gates. The deduplication pass (redundant file reads / shell command outputs)
// always runs; only the observation-masking pass is gated, because dedup is
// always strictly beneficial while masking trades information density for
// token savings.
func (opt *ConversationOptimizer) OptimizeConversationWithOptions(messages []Message, opts OptimizeOptions) []Message {
	msgs, _ := opt.optimize(messages, opts)
	return msgs
}

func (opt *ConversationOptimizer) optimize(messages []Message, opts OptimizeOptions) ([]Message, bool) {
	if !opt.enabled || opt.knownToolFn == nil {
		return messages, false
	}
	dedupChanged := false

	// Build a map of tool-call ID → metadata extracted from the assistant message.
	// Used by both the dedup pass below and the observation-masking pass at the
	// end, so the tool name shown in the placeholder is the real function name
	// rather than a generic "unknown".
	type meta struct {
		category ToolCategory
		filePath string
		command  string
		toolName string
	}
	callMeta := make(map[string]meta)
	for _, msg := range messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			cat := opt.knownToolFn(tc.Function.Name)
			m := meta{category: cat, toolName: tc.Function.Name}
			switch cat {
			case ToolCategoryFileRead:
				m.filePath = extractStringArg(tc.Function.Arguments, "path")
			case ToolCategoryShellCommand:
				m.command = extractStringArg(tc.Function.Arguments, "cmd")
			}
			callMeta[tc.ID] = m
		}
	}

	// --- File-read deduplication ---
	// Track: filepath → map[contentHash] → list of message indices.
	// Only replaces earlier reads that have identical content to a later read.
	// Hashing avoids storing large content strings as map keys.
	type readRecord struct {
		index int
	}
	fileReads := make(map[string]map[string][]readRecord)

	// --- Shell-command deduplication ---
	// Track: full command string → map[contentHash] → latest messageIndex.
	// Only replaces earlier output that matches the latest output for transient commands.
	shellCmdLatest := make(map[string]map[string]int)

	for i, msg := range messages {
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}

		m, ok := callMeta[msg.ToolCallID]
		if !ok {
			continue
		}

		switch m.category {
		case ToolCategoryFileRead:
			if m.filePath == "" {
				continue
			}

			byFile, ok := fileReads[m.filePath]
			if !ok {
				byFile = make(map[string][]readRecord)
				fileReads[m.filePath] = byFile
			}

			// Hash the content for efficient comparison and storage.
			contentHash := hashContent(msg.Content)

			// Replace any earlier read of the same file that has identical content.
			records, ok := byFile[contentHash]
			if ok {
				for _, rec := range records {
					if messages[rec.index].Content != fmt.Sprintf("[Earlier file read: %s]", m.filePath) {
						messages[rec.index].Content = fmt.Sprintf("[Earlier file read: %s]", m.filePath)
						dedupChanged = true
					}
				}
			}

			byFile[contentHash] = append(records, readRecord{index: i})

			// Bound the records per file to keep comparisons O(n) amortized.
			for contentHash, recs := range byFile {
				if len(recs) > maxFileReadRecords {
					byFile[contentHash] = recs[len(recs)-maxFileReadRecords:]
				}
			}

		case ToolCategoryShellCommand:
			if m.command == "" {
				continue
			}
			base := baseCommand(m.command)
			if !isTransientCommand(base) {
				continue
			}

			byCmd, ok := shellCmdLatest[m.command]
			if !ok {
				byCmd = make(map[string]int)
				shellCmdLatest[m.command] = byCmd
			}

			// Hash the content for efficient comparison and storage.
			contentHash := hashContent(msg.Content)

			// If there's a previous output with the same content, replace it.
			if prevIdx, exists := byCmd[contentHash]; exists {
				placeholder := fmt.Sprintf("[Earlier command output: %s]", m.command)
				if messages[prevIdx].Content != placeholder {
					messages[prevIdx].Content = placeholder
					dedupChanged = true
				}
			}
			// Record this output as the latest for its content.
			byCmd[contentHash] = i

			// Evict the oldest entry if the map exceeds the cap.
			if len(byCmd) > maxShellCmdRecords {
				oldestHash := ""
				oldestIdx := len(messages)
				for h, idx := range byCmd {
					if idx < oldestIdx {
						oldestIdx = idx
						oldestHash = h
					}
				}
				if oldestHash != "" {
					delete(byCmd, oldestHash)
				}
			}
		}
	}

	// Observation masking: replace large consumed tool results with compact
	// placeholders so chatty tools (web fetch, large file reads, verbose MCP
	// responses) don't accumulate in the prompt forever. A tool result is
	// "consumed" once the model has produced a subsequent assistant message
	// after seeing it. The last observationMaskKeepLast results stay unmasked
	// so recent context survives.
	//
	// Gated on opts.MaskConsumedToolResults: below the caller's context
	// pressure threshold, raw tool outputs flow through so the model can
	// refer back to them instead of re-reading.
	if opts.MaskConsumedToolResults {
		messages = maskConsumedToolResults(messages, func(callID string) string {
			if m, ok := callMeta[callID]; ok && m.toolName != "" {
				return m.toolName
			}
			if callID != "" {
				return callID
			}
			return "tool"
		})
	}

	return messages, dedupChanged
}

// maskConsumedToolResults walks the message list and replaces the content of
// large tool-result messages with compact placeholders. A result is eligible
// when (a) its content exceeds observationMaskMaxChars and (b) it sits before
// the last assistant message AND outside the last observationMaskKeepLast
// tool results. nameFn maps a tool-call ID to a human-readable tool name for
// the placeholder text.
func maskConsumedToolResults(messages []Message, nameFn func(callID string) string) []Message {
	if len(messages) < 3 {
		return messages
	}

	// Find the last assistant message; tool results past this point are still
	// being reasoned about and must not be masked.
	lastAssistantIndex := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantIndex = i
			break
		}
	}
	if lastAssistantIndex == 0 {
		return messages
	}

	// Collect tool-result indices before the last assistant message.
	var consumed []int
	for i := 0; i < lastAssistantIndex; i++ {
		if messages[i].Role == "tool" {
			consumed = append(consumed, i)
		}
	}

	maskCount := len(consumed) - observationMaskKeepLast
	if maskCount <= 0 {
		return messages
	}

	// Copy-on-write: only allocate a new slice if we'll actually mask.
	result := make([]Message, len(messages))
	copy(result, messages)

	for _, idx := range consumed[:maskCount] {
		content := messages[idx].Content
		if len(content) <= observationMaskMaxChars {
			continue
		}
		toolName := "tool"
		if nameFn != nil {
			toolName = nameFn(messages[idx].ToolCallID)
		}
		lineCount := countLines(content)
		rewritten := messages[idx]
		rewritten.Content = formatObservationPlaceholder(toolName, len(content), lineCount)
		result[idx] = rewritten
	}

	return result
}

// IterativelyMaskOldestConsumedToolResults masks already-consumed big tool
// results one at a time, oldest first, until the estimateFn reports we're at
// or below target. Returns the new message list, the number of results
// actually masked, and whether we reached the target.
//
// This is the Phase 0b primitive — used after checkpoint
// substitution (Phase 0a) couldn't relieve enough pressure on its own.
// Like Phase 0a, the property is *minimum information loss*: we never blank
// a result we don't have to. The "keep last N" window
// (observationMaskKeepLast) is honored — the model's most-recent tool
// outputs are never touched, even under pressure.
//
// callMeta should map tool-call IDs to (toolName, ...) so the placeholder
// includes the real tool name (read_file, web_search, etc.); pass nil to
// fall back to a generic "tool" label.
//
// Returns:
//   - newMessages: copy of input with eligible results masked (returned even
//     when applied == 0 to keep the call sites uniform)
//   - applied: number of results actually masked
//   - under: true if applied masks brought estimate to or below target
func IterativelyMaskOldestConsumedToolResults(
	messages []Message,
	nameFn func(callID string) string,
	target int,
	estimateFn func([]Message) int,
) ([]Message, int, bool) {
	if len(messages) < 3 || target <= 0 {
		return messages, 0, estimateFn(messages) <= target
	}
	if nameFn == nil {
		nameFn = func(_ string) string { return "tool" }
	}

	// Identify the eligible-for-masking indices once, oldest first.
	// Same rule as maskConsumedToolResults but doesn't mask — just lists.
	eligible := eligibleConsumedToolResultIndices(messages)
	if len(eligible) == 0 {
		return messages, 0, estimateFn(messages) <= target
	}

	// Early exit if already under target.
	if estimateFn(messages) <= target {
		return messages, 0, true
	}

	// Copy-on-write: allocate once, mutate in place per step.
	result := make([]Message, len(messages))
	copy(result, messages)

	for i, idx := range eligible {
		content := result[idx].Content
		if len(content) <= observationMaskMaxChars {
			// Eligible by position but too small to bother — skip.
			continue
		}
		lineCount := countLines(content)
		rewritten := result[idx]
		rewritten.Content = formatObservationPlaceholder(nameFn(rewritten.ToolCallID), len(content), lineCount)
		result[idx] = rewritten

		if estimateFn(result) <= target {
			return result, i + 1, true
		}
	}

	return result, len(eligible), false
}

// eligibleConsumedToolResultIndices returns the indices of tool-result
// messages that observation masking is *allowed* to touch — those that sit
// before the last assistant message AND outside the most-recent
// observationMaskKeepLast tool-result window. Returned oldest-first so
// callers can mask in stable progression.
//
// This is the shared eligibility computation used by both the bulk
// "mask everything eligible" path (maskConsumedToolResults) and the
// iterative path. Keeping it in one place ensures both paths agree on
// which results are sacrosanct.
func eligibleConsumedToolResultIndices(messages []Message) []int {
	if len(messages) < 3 {
		return nil
	}

	lastAssistantIndex := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantIndex = i
			break
		}
	}
	if lastAssistantIndex == 0 {
		return nil
	}

	var consumed []int
	for i := 0; i < lastAssistantIndex; i++ {
		if messages[i].Role == "tool" {
			consumed = append(consumed, i)
		}
	}

	maskCount := len(consumed) - observationMaskKeepLast
	if maskCount <= 0 {
		return nil
	}
	return consumed[:maskCount]
}

// formatObservationPlaceholder builds the standard "previous result" marker.
// Kept short so it costs nearly nothing in tokens while still telling the
// model what was there.
func formatObservationPlaceholder(toolName string, chars, lines int) string {
	return "[PREVIOUS RESULT: " + toolName + ", " + itoa(chars) + " chars, " + itoa(lines) + " lines]"
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}

// itoa is a tiny strconv-free int-to-string for the placeholder builder. Used
// to avoid pulling strconv into this file just for two numbers per placeholder.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// extractStringArg parses the JSON arguments and returns the string value for
// the given key, or "" if not found / not a string.
func extractStringArg(argsJSON, key string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// baseCommand extracts the first word of a shell command string.
func baseCommand(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// isTransientCommand returns true for commands whose output is inherently
// volatile and can be safely deduplicated (keep only the latest). These are
// commands that inspect the current state of the filesystem or print values
// without producing meaningful side effects. When the same command produces
// identical output across runs, earlier output is replaced with a placeholder
// to save context tokens.
func isTransientCommand(base string) bool {
	switch base {
	case "ls", "find", "pwd", "cat", "echo", "head", "tail", "wc":
		return true
	}
	return false
}

// hashContent returns a short hex hash of the content for efficient
// comparison and map-key storage. Uses SHA-256 truncated to 16 hex chars
// (64 bits) which is more than sufficient for collision resistance in
// this context — we only need to distinguish different file/command outputs.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}
