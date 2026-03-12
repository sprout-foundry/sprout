package agent

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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

// PruningThresholds defines when and how aggressively to prune for each provider type
type PruningThresholds struct {
	ProviderName       string
	StandardPercent    float64 // Percentage of max context to start pruning (e.g., 0.85 = 85%)
	MinAvailableTokens int     // Start pruning when remaining context is below this value
	AggressivePercent  float64 // Percentage to trigger aggressive mode (e.g., 0.90 = 90%)
	MinMessages        int     // Minimum messages to always keep
	RecentMessages     int     // Recent messages to always preserve
	SlidingWindow      int     // Window size for sliding window strategy
}

// PruningConfig is the single source of truth for all pruning thresholds
var PruningConfig = struct {
	// Threshold definitions
	Default PruningThresholds

	// Aggressive optimization settings
	Aggressive struct {
		RecentMessagesToKeep int // Messages from end to preserve during aggressive pruning
		TruncateAt           int // Character limit for truncation during aggressive pruning
		FileReadAgeThreshold int // Message age below which old file reads are summarized
	}

	// Target token percentages when pruning
	TargetPercentDefault float64 // % of max tokens to target after pruning

	// Agentic-flow safeguard: required headroom after pruning when possible.
	AgenticRequiredAvailableTokens int
}{
	// Default thresholds (applies to all providers - based on total context)
	Default: PruningThresholds{
		ProviderName:       "default",
		StandardPercent:    0.90, // Start pruning at 90% of context (don't prune until 90%+)
		MinAvailableTokens: 0,   // Disabled - use percentage-based threshold only
		AggressivePercent:  0.95, // Aggressive mode at 95%
		MinMessages:        5,
		RecentMessages:     15,
		SlidingWindow:      30,
	},

	// Aggressive optimization settings
	Aggressive: struct {
		RecentMessagesToKeep int
		TruncateAt           int
		FileReadAgeThreshold int
	}{
		RecentMessagesToKeep: 8,    // Keep last 8 messages during aggressive mode
		TruncateAt:           1200, // Truncate at 1200 characters
		FileReadAgeThreshold: 12,   // Summarize file reads older than 12 messages
	},

	// Target percentages for pruning
	TargetPercentDefault:           0.85,  // Target ~85% of context when pruning
	AgenticRequiredAvailableTokens: 12000, // Keep at least 12K headroom for agentic/tool-heavy loops
}

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
	// Use PruningConfig.Default as single source of truth
	return &ConversationPruner{
		strategy:             PruneStrategyAdaptive,
		contextThreshold:     PruningConfig.Default.StandardPercent,
		minMessagesToKeep:    PruningConfig.Default.MinMessages,
		recentMessagesToKeep: PruningConfig.Default.RecentMessages,
		slidingWindowSize:    PruningConfig.Default.SlidingWindow,
		debug:                debug,
	}
}

// ShouldPrune checks if pruning should occur.
// It triggers when usage exceeds the threshold percentage (based on model's max context).
// All thresholds are percentage-based to work with any context size.
func (cp *ConversationPruner) ShouldPrune(currentTokens, maxTokens int, provider string, isAgenticFlow bool) bool {
	if cp.strategy == PruneStrategyNone {
		return false
	}
	if maxTokens <= 0 {
		return false
	}

	// Use default thresholds for all providers (based on model's max context)
	// The threshold is calculated as a percentage of maxTokens, not an absolute value
	standardThreshold := PruningConfig.Default.StandardPercent
	if cp.contextThreshold > 0 && cp.contextThreshold < 1 {
		standardThreshold = cp.contextThreshold
	}

	// Check if usage exceeds the main percentage threshold
	contextUsage := float64(currentTokens) / float64(maxTokens)
	if contextUsage > standardThreshold {
		if cp.debug {
			fmt.Printf("🔄 Context usage exceeds threshold: %.1f%% > %.1f%%\n",
				contextUsage*100, standardThreshold*100)
		}
		return true
	}

	return false
}

// PruneConversation automatically prunes conversation based on strategy
func (cp *ConversationPruner) PruneConversation(messages []api.Message, currentTokens, maxTokens int, optimizer *ConversationOptimizer, provider string, isAgenticFlow bool) []api.Message {
	if !cp.ShouldPrune(currentTokens, maxTokens, provider, isAgenticFlow) {
		return messages
	}

	contextUsage := float64(currentTokens) / float64(maxTokens)
	if cp.debug {
		fmt.Printf("🔄 Auto-pruning triggered (%.1f%% context used, strategy: %s, provider: %s)\n", contextUsage*100, cp.strategy, provider)
	}

	var pruned []api.Message

	switch cp.strategy {
	case PruneStrategySlidingWindow:
		pruned = cp.pruneSlidingWindow(messages)
	case PruneStrategyImportance:
		pruned = cp.pruneByImportance(messages, provider, maxTokens)
	case PruneStrategyHybrid:
		pruned = cp.pruneHybrid(messages, optimizer, provider, maxTokens)
	case PruneStrategyAdaptive:
		pruned = cp.pruneAdaptive(messages, currentTokens, maxTokens, optimizer, provider)
	default:
		pruned = messages // No pruning
	}

	// Ensure we never prune too aggressively
	if len(pruned) < cp.minMessagesToKeep && len(messages) >= cp.minMessagesToKeep {
		keepIndices := make(map[int]struct{}, cp.minMessagesToKeep+cp.recentMessagesToKeep)
		for i := 0; i < cp.minMessagesToKeep && i < len(messages); i++ {
			keepIndices[i] = struct{}{}
		}
		recentStart := len(messages) - cp.recentMessagesToKeep
		if recentStart < 0 {
			recentStart = 0
		}
		for i := recentStart; i < len(messages); i++ {
			keepIndices[i] = struct{}{}
		}

		pruned = make([]api.Message, 0, len(keepIndices))
		for i := range messages {
			if _, ok := keepIndices[i]; ok {
				pruned = append(pruned, messages[i])
			}
		}
	}

	if cp.debug {
		oldTokens := cp.estimateTokens(messages)
		newTokens := cp.estimateTokens(pruned)
		fmt.Printf("✅ Pruning complete: %d → %d messages, ~%dK → ~%dK tokens\n",
			len(messages), len(pruned), oldTokens/1000, newTokens/1000)
	}

	if isAgenticFlow {
		pruned = cp.ensureRequiredHeadroom(pruned, maxTokens, PruningConfig.AgenticRequiredAvailableTokens)
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
func (cp *ConversationPruner) pruneByImportance(messages []api.Message, provider string, maxTokens int) []api.Message {
	// For providers with strict tool call requirements (Minimax, DeepSeek),
	// ensure tool calls and results are linked
	if strings.EqualFold(provider, "minimax") || strings.EqualFold(provider, "deepseek") {
		return cp.pruneByImportanceToolCallAware(messages, provider, maxTokens)
	}

	// Original scoring for other providers
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
	targetTokens := cp.getTargetTokensForProvider(len(messages), provider, maxTokens)
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

// pruneByImportanceToolCallAware keeps messages based on importance scoring while preserving tool call/result pairing
// This is critical for providers like Minimax and DeepSeek that require strict tool call format
func (cp *ConversationPruner) pruneByImportanceToolCallAware(messages []api.Message, provider string, maxTokens int) []api.Message {
	if cp.debug {
		fmt.Printf("🔧 Using tool-call aware pruning for %s\n", provider)
	}

	// Step 1: Group messages into tool call groups
	type MessageGroup struct {
		AssistantIndex int      // Index of assistant message with tool calls
		ToolCallIDs    []string // Tool call IDs
		ToolIndices    []int    // Indices of corresponding tool results
		IsToolGroup    bool
		Importance     float64
		TokenEstimate  int
	}

	groups := make([]*MessageGroup, 0, len(messages))
	var currentGroup *MessageGroup

	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Start a new tool group
			if currentGroup != nil {
				groups = append(groups, currentGroup)
			}

			toolCallIDs := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					toolCallIDs = append(toolCallIDs, tc.ID)
				}
			}

			currentGroup = &MessageGroup{
				AssistantIndex: i,
				ToolCallIDs:    toolCallIDs,
				ToolIndices:    make([]int, 0),
				IsToolGroup:    true,
			}
		} else if msg.Role == "tool" {
			// Check if this tool result belongs to current group
			if currentGroup != nil && msg.ToolCallId != "" && slices.Contains(currentGroup.ToolCallIDs, msg.ToolCallId) {
				currentGroup.ToolIndices = append(currentGroup.ToolIndices, i)
			} else {
				// Orphaned tool result - shouldn't happen but handle gracefully
				if cp.debug {
					fmt.Printf("⚠️ Found orphaned tool result at index %d with tool_call_id=%s\n", i, msg.ToolCallId)
				}
				// Add current group first if it's a tool group
				if currentGroup != nil {
					groups = append(groups, currentGroup)
				}
				// Create a single-message group for this orphan
				groups = append(groups, &MessageGroup{
					AssistantIndex: i,
					ToolIndices:    []int{},
					IsToolGroup:    false,
				})
				currentGroup = nil
			}
		} else {
			// Non-tool-related message
			if currentGroup != nil {
				groups = append(groups, currentGroup)
				currentGroup = nil
			}

			// Add as a single-message group
			groups = append(groups, &MessageGroup{
				AssistantIndex: i,
				ToolIndices:    []int{},
				IsToolGroup:    false,
			})
		}
	}

	// Don't forget the last group
	if currentGroup != nil {
		groups = append(groups, currentGroup)
	}

	// Step 2: Score each group
	for _, group := range groups {
		if group.IsToolGroup {
			// For tool groups, score is max of all messages in the group
			maxScore := 0.0
			totalTokens := 0

			// Score assistant message
			asstScore := cp.scoreSingleMessage(messages[group.AssistantIndex])
			maxScore = asstScore
			totalTokens += EstimateTokens(messages[group.AssistantIndex].Content)

			// Score tool results
			for _, toolIdx := range group.ToolIndices {
				toolScore := cp.scoreSingleMessage(messages[toolIdx])
				if toolScore > maxScore {
					maxScore = toolScore
				}
				totalTokens += EstimateTokens(messages[toolIdx].Content)
			}

			group.Importance = maxScore
			group.TokenEstimate = totalTokens
		} else {
			// Single message
			group.Importance = cp.scoreSingleMessage(messages[group.AssistantIndex])
			group.TokenEstimate = EstimateTokens(messages[group.AssistantIndex].Content)
		}
	}

	// Step 3: Select groups to keep (always keep first and last groups)
	keepGroups := make(map[int]bool)
	keepGroups[0] = true // First group
	if len(groups) > 1 {
		keepGroups[len(groups)-1] = true // Last group
	}

	// Keep recent groups (last few groups)
	recentGroups := 5
	if len(groups) <= recentGroups {
		recentGroups = len(groups) - 1
	}
	for i := len(groups) - recentGroups; i < len(groups); i++ {
		if i >= 0 {
			keepGroups[i] = true
		}
	}

	// Keep high-importance groups
	targetTokens := cp.getTargetTokensForProvider(len(messages), provider, maxTokens)
	currentTokens := 0
	for i := range groups {
		if keepGroups[i] {
			currentTokens += groups[i].TokenEstimate
		}
	}

	// Sort groups by importance and add high-scoring ones
	sortedGroups := make([]int, 0, len(groups))
	for i := range groups {
		if !keepGroups[i] {
			sortedGroups = append(sortedGroups, i)
		}
	}
	// Simple sort by importance
	for i := 0; i < len(sortedGroups); i++ {
		for j := i + 1; j < len(sortedGroups); j++ {
			if groups[sortedGroups[i]].Importance < groups[sortedGroups[j]].Importance {
				sortedGroups[i], sortedGroups[j] = sortedGroups[j], sortedGroups[i]
			}
		}
	}

	// Add high-importance groups until we reach target
	for _, groupIdx := range sortedGroups {
		if groups[groupIdx].Importance > 0.5 {
			testTokens := currentTokens + groups[groupIdx].TokenEstimate
			if testTokens < targetTokens {
				keepGroups[groupIdx] = true
				currentTokens += groups[groupIdx].TokenEstimate
			}
		}
	}

	// Step 4: Build pruned message list from kept groups
	pruned := make([]api.Message, 0, len(messages))
	for i := range groups {
		if keepGroups[i] {
			group := groups[i]
			pruned = append(pruned, messages[group.AssistantIndex])
			for _, toolIdx := range group.ToolIndices {
				pruned = append(pruned, messages[toolIdx])
			}
		}
	}

	if cp.debug {
		oldTokens := cp.estimateTokens(messages)
		newTokens := cp.estimateTokens(pruned)
		fmt.Printf("✅ Tool-call aware pruning: %d → %d messages, ~%dK → ~%dK tokens\n",
			len(messages), len(pruned), oldTokens/1000, newTokens/1000)
	}

	return pruned
}

// scoreSingleMessage scores a single message (simplified version of scoreMessages)
func (cp *ConversationPruner) scoreSingleMessage(msg api.Message) float64 {
	importance := 0.0

	if msg.Role == "system" {
		return 1.0
	} else if msg.Role == "user" {
		importance = 0.6

		// Errors are important
		if strings.Contains(strings.ToLower(msg.Content), "error") {
			importance = 0.8
		}
	} else if msg.Role == "tool" {
		importance = 0.5
		if strings.Contains(strings.ToLower(msg.Content), "error") {
			importance = 0.8
		}
	} else if msg.Role == "assistant" {
		// Assistant messages are moderately important
		importance = 0.5

		// Assistant messages with tool calls are more important
		if len(msg.ToolCalls) > 0 {
			importance = 0.6
		}
	}

	if importance > 1.0 {
		importance = 1.0
	}

	return importance
}

// pruneHybrid combines sliding window with importance scoring
func (cp *ConversationPruner) pruneHybrid(messages []api.Message, optimizer *ConversationOptimizer, provider string, maxTokens int) []api.Message {
	if optimizer == nil {
		return cp.pruneByImportance(messages, provider, maxTokens)
	}
	// First apply optimizer's deduplication
	optimized := optimizer.OptimizeConversation(messages)

	// Then apply importance-based pruning
	return cp.pruneByImportance(optimized, provider, maxTokens)
}

// pruneAdaptive uses different strategies based on conversation characteristics
func (cp *ConversationPruner) pruneAdaptive(messages []api.Message, currentTokens, maxTokens int, optimizer *ConversationOptimizer, provider string) []api.Message {
	contextUsage := float64(currentTokens) / float64(maxTokens)

	// Analyze conversation characteristics
	hasLongHistory := len(messages) > 50
	hasManyToolCalls := cp.countToolCalls(messages) > 20
	hasLargeFiles := cp.hasLargeFileReads(messages)

	// Apply different strategies based on context usage
	if contextUsage > PruningConfig.Default.AggressivePercent {
		// Critical - use aggressive optimization
		if cp.debug {
			fmt.Printf("🚨 Critical context usage (%.1f%% >= %.1f%%), using aggressive optimization\n",
				contextUsage*100, PruningConfig.Default.AggressivePercent*100)
		}
		if optimizer == nil {
			return cp.pruneByImportance(messages, provider, maxTokens)
		}
		return optimizer.AggressiveOptimization(messages)
	} else if hasLongHistory && hasManyToolCalls && contextUsage > 0.80 {
		// Long technical conversation - use hybrid approach (only above 80% context)
		if cp.debug {
			fmt.Printf("📊 Long technical conversation detected, using hybrid pruning\n")
		}
		return cp.pruneHybrid(messages, optimizer, provider, maxTokens)
	} else if hasLargeFiles && contextUsage > 0.80 {
		// File-heavy conversation - focus on deduplication (only above 80% context)
		if cp.debug {
			fmt.Printf("📄 File-heavy conversation detected, focusing on deduplication\n")
		}
		if optimizer == nil {
			return cp.pruneSlidingWindow(messages)
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
		return cp.pruneByImportance(messages, provider, maxTokens)
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

			// Errors are important
			if strings.Contains(strings.ToLower(msg.Content), "error") {
				score.IsError = true
				importance = 0.8
			}
		} else if msg.Role == "tool" {
			score.IsToolResult = true
			// Recent tool results are more important
			if score.Age < 5 {
				importance = 0.7
			} else {
				importance = 0.3
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

// getTargetTokens returns default target tokens based on conversation size
func (cp *ConversationPruner) getTargetTokens(messageCount, maxTokens int) int {
	if maxTokens <= 0 {
		maxTokens = 100000
	}
	baseTarget := int(PruningConfig.TargetPercentDefault * float64(maxTokens))

	// Adjust based on message count
	if messageCount < 20 {
		return clampTargetTokens(baseTarget, maxTokens)
	} else if messageCount < 50 {
		return clampTargetTokens(baseTarget-int(0.08*float64(maxTokens)), maxTokens)
	} else {
		return clampTargetTokens(baseTarget-int(0.15*float64(maxTokens)), maxTokens)
	}
}

// getTargetTokensForProvider is kept for compatibility and currently uses default targets.
func (cp *ConversationPruner) getTargetTokensForProvider(messageCount int, provider string, maxTokens int) int {
	_ = provider
	return cp.getTargetTokens(messageCount, maxTokens)
}

func (cp *ConversationPruner) countToolCalls(messages []api.Message) int {
	count := 0
	for _, msg := range messages {
		// Count tool result messages directly
		if msg.Role == "tool" {
			count++
		}
		// Count assistant messages that explicitly contain tool calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			count++
		}
	}
	return count
}

func (cp *ConversationPruner) hasLargeFileReads(messages []api.Message) bool {
	for _, msg := range messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Tool call result for read_file") {
			if len(msg.Content) > 5000 { // Large file read
				return true
			}
		}
	}
	return false
}

func clampTargetTokens(target, maxTokens int) int {
	minTarget := int(0.25 * float64(maxTokens))
	if minTarget < 1000 {
		minTarget = 1000
	}
	if target < minTarget {
		return minTarget
	}
	if target > maxTokens {
		return maxTokens
	}
	return target
}

func (cp *ConversationPruner) ensureRequiredHeadroom(messages []api.Message, maxTokens, requiredAvailable int) []api.Message {
	if maxTokens <= 0 || requiredAvailable <= 0 || len(messages) <= cp.minMessagesToKeep {
		return messages
	}

	pruned := messages
	for len(pruned) > cp.minMessagesToKeep {
		remaining := maxTokens - cp.estimateTokens(pruned)
		if remaining >= requiredAvailable {
			return pruned
		}
		// Keep system message and trim oldest non-system message.
		if len(pruned) <= 1 {
			return pruned
		}
		pruned = append(pruned[:1], pruned[2:]...)
	}

	return pruned
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
