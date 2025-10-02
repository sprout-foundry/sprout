package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// PruningStrategy defines different pruning approaches
type PruningStrategy string

const (
	PruneStrategyNone          PruningStrategy = "none"
	PruneStrategySlidingWindow PruningStrategy = "sliding_window"
	PruneStrategyImportance    PruningStrategy = "importance"
	PruneStrategyHybrid        PruningStrategy = "hybrid"
	PruneStrategyAdaptive      PruningStrategy = "adaptive"
)

// MessageImportance tracks the importance of a message
type MessageImportance struct {
	Index           int
	Role            string
	IsUserQuery     bool
	HasToolCalls    bool
	IsToolResult    bool
	IsError         bool
	ContentLength   int
	TokenEstimate   int
	Age             int // How many messages ago
	ImportanceScore float64
}

// ConversationPruner handles automatic conversation pruning
type ConversationPruner struct {
	strategy             PruningStrategy
	contextThreshold     float64 // Threshold to trigger pruning (e.g., 0.7 = 70%)
	minMessagesToKeep    int     // Minimum messages to always keep
	recentMessagesToKeep int     // Number of recent messages to always keep
	slidingWindowSize    int     // For sliding window strategy
	debug                bool
}

// NewConversationPruner creates a new conversation pruner with default settings
func NewConversationPruner(debug bool) *ConversationPruner {
	return &ConversationPruner{
		strategy:             PruneStrategyAdaptive, // Default to adaptive
		contextThreshold:     0.7,                   // 70% threshold (used in hybrid approach)
		minMessagesToKeep:    3,                     // Always keep at least system + first query + first response
		recentMessagesToKeep: 10,                    // Keep last 10 messages
		slidingWindowSize:    20,                    // For sliding window strategy
		debug:                debug,
	}
}

// ShouldPrune checks if pruning should occur based on context usage
// Default behavior: Hybrid approach (70K tokens OR 70% of context limit)
// For providers with significant cached-token discounts (e.g., OpenAI) use an
// alternative policy: allow the context to grow until the remaining tokens are
// within min(20k, 20% of max) then prune.
func (cp *ConversationPruner) ShouldPrune(currentTokens, maxTokens int, provider string) bool {
	if cp.strategy == PruneStrategyNone {
		return false
	}

	// Providers that should use the cached-token friendly threshold
	cachedDiscountProviders := map[string]bool{
		"openai": true,
	}

	if cachedDiscountProviders[provider] {
		// Remaining tokens before hitting the model limit
		remaining := maxTokens - currentTokens

		// Compute threshold: whichever is lower of 20k or 20% of maxTokens
		percentThreshold := int(0.2 * float64(maxTokens))
		threshold := 20000
		if percentThreshold < threshold {
			threshold = percentThreshold
		}

		if cp.debug {
			fmt.Printf("🔁 Cached-discount provider (%s): remaining=%d, threshold=%d (min of 20k and 20%% of %d)\n",
				provider, remaining, threshold, maxTokens)
		}

		if remaining <= threshold {
			if cp.debug {
				fmt.Printf("🔄 Pruning triggered for cached-discount provider: remaining %d <= %d\n", remaining, threshold)
			}
			return true
		}

		return false
	}

	// Fallback/default behavior: Hybrid threshold: 70K tokens OR 70% of context limit
	const tokenCeiling = 70000      // 70K token absolute ceiling
	const percentageThreshold = 0.7 // 70% threshold

	// Check if we hit the absolute token ceiling
	if currentTokens >= tokenCeiling {
		if cp.debug {
			fmt.Printf("🔄 Token ceiling hit: %d >= %d tokens\n", currentTokens, tokenCeiling)
		}
		return true
	}

	// Check if we hit the percentage threshold
	contextUsage := float64(currentTokens) / float64(maxTokens)
	if contextUsage >= percentageThreshold {
		if cp.debug {
			fmt.Printf("🔄 Percentage threshold hit: %.1f%% >= %.1f%%\n", contextUsage*100, percentageThreshold*100)
		}
		return true
	}

	return false
}

// PruneConversation automatically prunes conversation based on strategy
func (cp *ConversationPruner) PruneConversation(messages []api.Message, currentTokens, maxTokens int, optimizer *ConversationOptimizer, provider string) []api.Message {
	if !cp.ShouldPrune(currentTokens, maxTokens, provider) {
		return messages
	}

	if cp.debug {
		contextUsage := float64(currentTokens) / float64(maxTokens)
		fmt.Printf("🔄 Auto-pruning triggered (%.1f%% context used, strategy: %s, provider: %s)\n", contextUsage*100, cp.strategy, provider)
	}

	var pruned []api.Message

	switch cp.strategy {
	case PruneStrategySlidingWindow:
		pruned = cp.pruneSlidingWindow(messages)
	case PruneStrategyImportance:
		pruned = cp.pruneByImportance(messages)
	case PruneStrategyHybrid:
		pruned = cp.pruneHybrid(messages, optimizer)
	case PruneStrategyAdaptive:
		pruned = cp.pruneAdaptive(messages, currentTokens, maxTokens, optimizer)
	default:
		pruned = messages // No pruning
	}

	// Ensure we never prune too aggressively
	if len(pruned) < cp.minMessagesToKeep && len(messages) >= cp.minMessagesToKeep {
		// Keep at least the minimum required messages
		pruned = messages[:cp.minMessagesToKeep]
		if len(messages) > cp.recentMessagesToKeep {
			// Add recent messages
			pruned = append(pruned, messages[len(messages)-cp.recentMessagesToKeep:]...)
		}
	}

	if cp.debug {
		oldTokens := cp.estimateTokens(messages)
		newTokens := cp.estimateTokens(pruned)
		fmt.Printf("✅ Pruning complete: %d → %d messages, ~%dK → ~%dK tokens\n",
			len(messages), len(pruned), oldTokens/1000, newTokens/1000)
	}

	return pruned
}

// pruneSlidingWindow keeps only the most recent messages within window
func (cp *ConversationPruner) pruneSlidingWindow(messages []api.Message) []api.Message {
	if len(messages) <= cp.slidingWindowSize {
		return messages
	}

	// Always keep system message
	pruned := []api.Message{messages[0]}

	// Keep the sliding window of recent messages
	startIdx := len(messages) - cp.slidingWindowSize + 1 // +1 because we already have system message
	pruned = append(pruned, messages[startIdx:]...)

	return pruned
}

// pruneByImportance keeps messages based on importance scoring
func (cp *ConversationPruner) pruneByImportance(messages []api.Message) []api.Message {
	// Score all messages
	scores := cp.scoreMessages(messages)

	// Always keep system message and recent messages
	pruned := []api.Message{messages[0]}

	// Identify messages to keep based on importance
	keepIndices := make(map[int]bool)
	keepIndices[0] = true // System message

	// Keep recent messages
	recentStart := len(messages) - cp.recentMessagesToKeep
	if recentStart < 1 {
		recentStart = 1
	}
	for i := recentStart; i < len(messages); i++ {
		keepIndices[i] = true
	}

	// Keep the first user query and response
	if len(messages) > 2 {
		keepIndices[1] = true // First user query
		keepIndices[2] = true // First response
	}

	// Keep high-importance messages from the middle
	targetTokens := cp.getTargetTokens(len(messages))
	currentTokens := cp.estimateTokensForIndices(messages, keepIndices)

	// Sort messages by importance (excluding already kept ones)
	for _, score := range scores {
		if !keepIndices[score.Index] && score.ImportanceScore > 0.5 {
			testTokens := currentTokens + score.TokenEstimate
			if testTokens < targetTokens {
				keepIndices[score.Index] = true
				currentTokens = testTokens
			}
		}
	}

	// Build pruned message list
	for i := 1; i < len(messages); i++ {
		if keepIndices[i] {
			pruned = append(pruned, messages[i])
		}
	}

	return pruned
}

// pruneHybrid combines sliding window with importance scoring
func (cp *ConversationPruner) pruneHybrid(messages []api.Message, optimizer *ConversationOptimizer) []api.Message {
	// First apply optimizer's deduplication
	optimized := optimizer.OptimizeConversation(messages)

	// Then apply importance-based pruning
	return cp.pruneByImportance(optimized)
}

// pruneAdaptive uses different strategies based on conversation characteristics
func (cp *ConversationPruner) pruneAdaptive(messages []api.Message, currentTokens, maxTokens int, optimizer *ConversationOptimizer) []api.Message {
	contextUsage := float64(currentTokens) / float64(maxTokens)

	// Analyze conversation characteristics
	hasLongHistory := len(messages) > 50
	hasManyToolCalls := cp.countToolCalls(messages) > 20
	hasLargeFiles := cp.hasLargeFileReads(messages)

	// Apply different strategies based on context
	if contextUsage > 0.9 {
		// Critical - use aggressive optimization
		if cp.debug {
			fmt.Printf("🚨 Critical context usage (%.1f%%), using aggressive optimization\n", contextUsage*100)
		}
		return optimizer.AggressiveOptimization(messages)
	} else if hasLongHistory && hasManyToolCalls {
		// Long technical conversation - use hybrid approach
		if cp.debug {
			fmt.Printf("📊 Long technical conversation detected, using hybrid pruning\n")
		}
		return cp.pruneHybrid(messages, optimizer)
	} else if hasLargeFiles {
		// File-heavy conversation - focus on deduplication
		if cp.debug {
			fmt.Printf("📄 File-heavy conversation detected, focusing on deduplication\n")
		}
		optimized := optimizer.OptimizeConversation(messages)
		if cp.estimateTokens(optimized) < int(float64(maxTokens)*0.8) {
			return optimized
		}
		// Still too large, apply sliding window
		return cp.pruneSlidingWindow(optimized)
	} else {
		// Default - importance-based pruning
		if cp.debug {
			fmt.Printf("⚖️ Using importance-based pruning\n")
		}
		return cp.pruneByImportance(messages)
	}
}

// scoreMessages calculates importance scores for all messages
func (cp *ConversationPruner) scoreMessages(messages []api.Message) []MessageImportance {
	scores := make([]MessageImportance, 0, len(messages))

	for i, msg := range messages {
		score := MessageImportance{
			Index:         i,
			Role:          msg.Role,
			ContentLength: len(msg.Content),
			TokenEstimate: EstimateTokens(msg.Content),
			Age:           len(messages) - i,
		}

		// Calculate importance based on various factors
		importance := 0.0

		// System messages are always important
		if msg.Role == "system" {
			importance = 1.0
		} else if msg.Role == "user" {
			// User messages have base importance
			importance = 0.6

			// First user query is very important
			if i == 1 {
				importance = 0.9
				score.IsUserQuery = true
			}

			// Tool results vary in importance
			if strings.Contains(msg.Content, "Tool call result") {
				score.IsToolResult = true
				// Recent tool results are more important
				if score.Age < 5 {
					importance = 0.7
				} else {
					importance = 0.3
				}
			}

			// Errors are important
			if strings.Contains(strings.ToLower(msg.Content), "error") {
				score.IsError = true
				importance = 0.8
			}
		} else if msg.Role == "assistant" {
			// Assistant messages are moderately important
			importance = 0.5

			// Assistant messages that mention tool usage are more important
			if strings.Contains(msg.Content, "I'll") || strings.Contains(msg.Content, "Let me") {
				importance = 0.6
			}

			// Recent assistant messages are more important
			if score.Age < 3 {
				importance += 0.2
			}
		}

		// Adjust for recency
		recencyBonus := 0.0
		if score.Age < 5 {
			recencyBonus = 0.3 * (5.0 - float64(score.Age)) / 5.0
		}
		importance += recencyBonus

		// Cap importance at 1.0
		if importance > 1.0 {
			importance = 1.0
		}

		score.ImportanceScore = importance
		scores = append(scores, score)
	}

	return scores
}

// Helper methods

func (cp *ConversationPruner) estimateTokens(messages []api.Message) int {
	tokens := 0
	for _, msg := range messages {
		tokens += EstimateTokens(msg.Content)
		if msg.ReasoningContent != "" {
			tokens += EstimateTokens(msg.ReasoningContent)
		}
	}
	return tokens
}

func (cp *ConversationPruner) estimateTokensForIndices(messages []api.Message, indices map[int]bool) int {
	tokens := 0
	for i, msg := range messages {
		if indices[i] {
			tokens += EstimateTokens(msg.Content)
			if msg.ReasoningContent != "" {
				tokens += EstimateTokens(msg.ReasoningContent)
			}
		}
	}
	return tokens
}

func (cp *ConversationPruner) getTargetTokens(messageCount int) int {
	// Aim for about 60% of typical context window when pruning
	baseTarget := 60000 // ~60K tokens

	// Adjust based on message count
	if messageCount < 20 {
		return baseTarget
	} else if messageCount < 50 {
		return baseTarget - 10000
	} else {
		return baseTarget - 20000
	}
}

func (cp *ConversationPruner) countToolCalls(messages []api.Message) int {
	count := 0
	for _, msg := range messages {
		// Count tool results in user messages
		if msg.Role == "user" && strings.Contains(msg.Content, "Tool call result") {
			count++
		}
		// Count assistant messages that appear to be initiating tool calls
		if msg.Role == "assistant" && (strings.Contains(msg.Content, "I'll use") ||
			strings.Contains(msg.Content, "Let me") ||
			strings.Contains(msg.Content, "I'll execute")) {
			count++
		}
	}
	return count
}

func (cp *ConversationPruner) hasLargeFileReads(messages []api.Message) bool {
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Tool call result for read_file") {
			if len(msg.Content) > 5000 { // Large file read
				return true
			}
		}
	}
	return false
}

// SetStrategy sets the pruning strategy
func (cp *ConversationPruner) SetStrategy(strategy PruningStrategy) {
	cp.strategy = strategy
}

// SetThreshold sets the context usage threshold for triggering pruning
func (cp *ConversationPruner) SetThreshold(threshold float64) {
	if threshold > 0 && threshold < 1 {
		cp.contextThreshold = threshold
	}
}

// SetRecentMessagesToKeep sets how many recent messages to always preserve
func (cp *ConversationPruner) SetRecentMessagesToKeep(count int) {
	if count > 0 {
		cp.recentMessagesToKeep = count
	}
}

// SetSlidingWindowSize sets the window size for sliding window strategy
func (cp *ConversationPruner) SetSlidingWindowSize(size int) {
	if size > 0 {
		cp.slidingWindowSize = size
	}
}
