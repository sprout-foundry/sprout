package agent

import (
	"fmt"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ConversationOptimizer manages conversation history optimization
type ConversationOptimizer struct {
	mu            sync.RWMutex
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

	// Phase 1: Track reads and commands (modifies maps, needs write lock)
	co.mu.Lock()
	for i, msg := range messages {
		co.trackFileRead(msg, i)
		co.trackShellCommand(msg, i)
	}
	co.mu.Unlock()

	// Phase 2: Optimize based on tracked data (read-only access to maps).
	// Use RLock so concurrent readers aren't blocked during the optimization pass.
	co.mu.RLock()
	defer co.mu.RUnlock()

	optimized := make([]api.Message, 0, len(messages))

	for i, msg := range messages {
		if co.isRedundantFileRead(msg, i) {
			// Replace with summary
			summary := co.createFileReadSummary(msg)
			rewritten := msg
			rewritten.Content = summary
			optimized = append(optimized, rewritten)
			if co.debug && co.printLine != nil {
				co.printLine(fmt.Sprintf("\n[~] Optimized redundant file read: %s\n", co.extractFilePath(msg.Content)))
			}
		} else if co.isRedundantShellCommand(msg, i) {
			// Replace with summary
			summary := co.createShellCommandSummary(msg)
			rewritten := msg
			rewritten.Content = summary
			optimized = append(optimized, rewritten)
			if co.debug && co.printLine != nil {
				co.printLine(fmt.Sprintf("\n[~] Optimized redundant shell command: %s\n", co.extractShellCommand(msg.Content)))
			}
		} else {
			optimized = append(optimized, msg)
		}
	}

	// Phase 3: Mask consumed tool results to prevent context bloat.
	optimized = co.maskConsumedToolResults(optimized)

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
				if co.debug && co.printLine != nil {
					co.printLine("[clean] Removed consecutive assistant at compaction boundary\n")
				}
				compacted = append(compacted[:summaryIdx+1], compacted[summaryIdx+2:]...)
			}
		}
	}

	return compacted
}

// compactConversationLayered compacts a large middle segment into a single
// assistant message containing graduated detail levels (brief, summary,
// detailed). Merging into one message avoids consecutive assistant-role
// messages that downstream sanitisation (stripConsecutiveAssistantMessages)
// would otherwise strip, wasting the earlier LLM compaction calls.
func (co *ConversationOptimizer) compactConversationLayered(messages []api.Message, anchorEnd, recentStart int, middle []api.Message) []api.Message {
	// Split middle into 3 layers: old-middle, mid-middle, recent-middle
	layerSize := len(middle) / 3
	if layerSize < MinLayerSize {
		layerSize = MinLayerSize
	}

	oldMiddleEnd := anchorEnd + layerSize
	midMiddleEnd := oldMiddleEnd + layerSize

	// Compute each layer's summary text (already wrapped via wrapCompactionSummaryWithLevel).
	oldMiddle := messages[anchorEnd:oldMiddleEnd]
	briefText := co.buildLLMCompactionSummaryWithLimit(oldMiddle, BriefWordLimit, "brief")

	midMiddle := messages[oldMiddleEnd:midMiddleEnd]
	summaryText := co.buildLLMCompactionSummaryWithLimit(midMiddle, SummaryWordLimit, "summary")

	recentMiddle := messages[midMiddleEnd:recentStart]
	detailedText := co.buildLLMCompactionSummaryWithLimit(recentMiddle, DetailedWordLimit, "detailed")

	// Build a single combined summary message. If no layer succeeded, fall back
	// to a single whole-segment summary.
	combined := co.mergeLayeredSummaries(briefText, summaryText, detailedText, len(middle))

	if combined == "" {
		// Fallback: single summary of the whole middle segment
		summary := co.buildLLMCompactionSummary(middle)
		if summary == "" {
			return messages
		}
		combined = summary
	}

	// Build compacted message list — only one assistant summary message.
	compacted := make([]api.Message, 0, anchorEnd+1+len(messages)-recentStart)
	compacted = append(compacted, messages[:anchorEnd]...)
	compacted = append(compacted, api.Message{
		Role:    "assistant",
		Content: combined,
	})
	compacted = append(compacted, messages[recentStart:]...)

	// Safety net: ensure we don't have consecutive assistant messages at the boundary.
	summaryIdx := anchorEnd
	if summaryIdx+1 < len(compacted) {
		if compacted[summaryIdx].Role == "assistant" && len(compacted[summaryIdx].ToolCalls) == 0 &&
			compacted[summaryIdx+1].Role == "assistant" && len(compacted[summaryIdx+1].ToolCalls) == 0 {
			if co.debug && co.printLine != nil {
				co.printLine("[clean] Removed consecutive assistant at layered compaction boundary\n")
			}
			compacted = append(compacted[:summaryIdx+1], compacted[summaryIdx+2:]...)
		}
	}

	if co.debug && co.printLine != nil {
		co.printLine(fmt.Sprintf("[layered] Layered compaction: %d messages → 1 merged summary\n", len(middle)))
	}

	return compacted
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

// GetOptimizationStats returns statistics about optimization
func (co *ConversationOptimizer) GetOptimizationStats() map[string]interface{} {
	co.mu.RLock()
	defer co.mu.RUnlock()

	return map[string]interface{}{
		"enabled":          co.enabled,
		"tracked_files":    len(co.fileReads),
		"tracked_commands": len(co.shellCommands),
		"file_paths":       co.getTrackedFilePaths(),
		"shell_commands":   co.getTrackedCommands(),
	}
}

// Reset clears all optimization state
func (co *ConversationOptimizer) Reset() {
	co.mu.Lock()
	defer co.mu.Unlock()

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
