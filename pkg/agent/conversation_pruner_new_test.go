package agent

import (
	"fmt"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestConversationPruner_ShouldPrune_NoneStrategy(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyNone)

	if cp.ShouldPrune(900, 1000, "anthropic", false) {
		t.Error("ShouldPrune should return false when strategy is none")
	}
}

func TestConversationPruner_ShouldPrune_MaxTokensZero(t *testing.T) {
	cp := NewConversationPruner(false)

	if cp.ShouldPrune(900, 0, "anthropic", false) {
		t.Error("ShouldPrune should return false when maxTokens is 0")
	}
}

func TestConversationPruner_ShouldPrune_BelowThreshold(t *testing.T) {
	cp := NewConversationPruner(false)
	// Default threshold is 0.87, so 50% usage should not trigger
	if cp.ShouldPrune(500, 1000, "anthropic", false) {
		t.Error("ShouldPrune should return false when usage is below threshold")
	}
}

func TestConversationPruner_ShouldPrune_AboveThreshold(t *testing.T) {
	cp := NewConversationPruner(false)
	// 90% usage > 87% threshold
	if !cp.ShouldPrune(900, 1000, "anthropic", false) {
		t.Error("ShouldPrune should return true when usage exceeds threshold")
	}
}

func TestConversationPruner_ShouldPrune_CustomThreshold(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetThreshold(0.5) // 50%

	if !cp.ShouldPrune(501, 1000, "anthropic", false) {
		t.Error("ShouldPrune should return true with custom threshold exceeded")
	}

	if cp.ShouldPrune(499, 1000, "anthropic", false) {
		t.Error("ShouldPrune should return false below custom threshold")
	}
}

func TestConversationPruner_ShouldPrune_InvalidThreshold(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetThreshold(-0.5) // negative - should be ignored
	cp.SetThreshold(1.5)  // >1 - should be ignored
	// Falls back to default 0.87 threshold
	if cp.ShouldPrune(500, 1000, "anthropic", false) {
		t.Error("ShouldPrune should use default threshold when custom is invalid")
	}
	if !cp.ShouldPrune(900, 1000, "anthropic", false) {
		t.Error("ShouldPrune should use default threshold when custom is invalid")
	}
}

func TestConversationPruner_SetStrategy(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	if cp.strategy != PruneStrategySlidingWindow {
		t.Errorf("strategy = %s, want %s", cp.strategy, PruneStrategySlidingWindow)
	}

	cp.SetStrategy(PruneStrategyImportance)
	if cp.strategy != PruneStrategyImportance {
		t.Errorf("strategy = %s, want %s", cp.strategy, PruneStrategyImportance)
	}
}

func TestConversationPruner_SetThreshold(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetThreshold(0.75)
	if cp.contextThreshold != 0.75 {
		t.Errorf("contextThreshold = %f, want 0.75", cp.contextThreshold)
	}

	// Invalid thresholds are ignored
	before := cp.contextThreshold
	cp.SetThreshold(-1)
	if cp.contextThreshold != before {
		t.Error("negative threshold should be ignored")
	}
	cp.SetThreshold(0)
	if cp.contextThreshold != before {
		t.Error("zero threshold should be ignored")
	}
	cp.SetThreshold(2.0)
	if cp.contextThreshold != before {
		t.Error("threshold > 1 should be ignored")
	}
}

func TestConversationPruner_SetRecentMessagesToKeep(t *testing.T) {
	cp := NewConversationPruner(false)

	cp.SetRecentMessagesToKeep(10)
	if cp.recentMessagesToKeep != 10 {
		t.Errorf("recentMessagesToKeep = %d, want 10", cp.recentMessagesToKeep)
	}

	// Invalid values are ignored
	cp.SetRecentMessagesToKeep(0)
	if cp.recentMessagesToKeep != 10 {
		t.Error("zero value should be ignored")
	}
	cp.SetRecentMessagesToKeep(-5)
	if cp.recentMessagesToKeep != 10 {
		t.Error("negative value should be ignored")
	}
}

func TestConversationPruner_SetSlidingWindowSize(t *testing.T) {
	cp := NewConversationPruner(false)

	cp.SetSlidingWindowSize(20)
	if cp.slidingWindowSize != 20 {
		t.Errorf("slidingWindowSize = %d, want 20", cp.slidingWindowSize)
	}

	cp.SetSlidingWindowSize(0)
	if cp.slidingWindowSize != 20 {
		t.Error("zero value should be ignored")
	}
}

func TestConversationPruner_pruneSlidingWindow_BelowWindow(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	cp.SetSlidingWindowSize(10)

	messages := makeMessages(8) // fewer than window
	result := cp.pruneSlidingWindow(messages)
	if len(result) != 8 {
		t.Errorf("expected 8 messages when below window, got %d", len(result))
	}
}

func TestConversationPruner_pruneSlidingWindow_AboveWindow(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	cp.SetSlidingWindowSize(5)

	messages := makeMessages(20)
	result := cp.pruneSlidingWindow(messages)
	// startIdx = len(messages) - slidingWindowSize + 1 = 20 - 5 + 1 = 16
	// result = [system] + messages[16:] = 1 + 4 = 5 total
	if len(result) != 5 {
		t.Errorf("expected 5 messages (system + 4 from window), got %d", len(result))
	}
	// First message should be system (index 0)
	if result[0].Role != "system" {
		t.Errorf("first message should be system, got role %s", result[0].Role)
	}
	// Second message should be msg-16 (startIdx=16)
	if result[1].Content != "msg-16" {
		t.Errorf("second message should be msg-16 (start of window), got %s", result[1].Content)
	}
}

func TestConversationPruner_pruneByImportance_KeepsSystemAndRecent(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyImportance)
	cp.SetRecentMessagesToKeep(5)

	messages := makeMessages(20)
	result := cp.pruneByImportance(messages, "anthropic", 100000)

	// Should keep system message
	if result[0].Role != "system" {
		t.Errorf("first message should be system, got %s", result[0].Role)
	}

	// Should keep recent messages (last 5)
	foundRecent := false
	for i := len(result) - 1; i >= 0 && i > 0; i-- {
		if result[i].Content == "msg-19" {
			foundRecent = true
			break
		}
	}
	if !foundRecent {
		t.Error("should keep most recent messages")
	}
}

func TestConversationPruner_pruneByImportanceToolCallAware(t *testing.T) {
	cp := NewConversationPruner(true) // debug to see output
	cp.SetStrategy(PruneStrategyImportance)
	cp.SetRecentMessagesToKeep(5)

	// Build messages with tool calls
	messages := []api.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "calling tool", ToolCalls: []api.ToolCall{{ID: "call-1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read_file", Arguments: ""}}}},
		{Role: "tool", Content: "file content", ToolCallId: "call-1"},
		{Role: "assistant", Content: "calling another tool", ToolCalls: []api.ToolCall{{ID: "call-2", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "shell_command", Arguments: ""}}}},
		{Role: "tool", Content: "command output", ToolCallId: "call-2"},
		{Role: "assistant", Content: "here is the result"},
	}

	result := cp.pruneByImportanceToolCallAware(messages, "minimax", 100000)

	// Should keep all messages since they're small and tool calls are paired
	if len(result) == 0 {
		t.Error("should return some messages")
	}

	// System message should be first
	if result[0].Role != "system" {
		t.Errorf("first message should be system, got %s", result[0].Role)
	}
}

func TestConversationPruner_pruneHybrid(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyHybrid)
	cp.SetRecentMessagesToKeep(5)

	messages := makeMessages(20)

	// With nil optimizer, falls back to importance
	result := cp.pruneHybrid(messages, nil, "anthropic", 100000)
	if len(result) == 0 {
		t.Error("should return some messages")
	}

	// With optimizer
	optimizer := NewConversationOptimizer(true, false)
	result = cp.pruneHybrid(messages, optimizer, "anthropic", 100000)
	if len(result) == 0 {
		t.Error("should return some messages with optimizer")
	}
}

func TestConversationPruner_pruneAdaptive_CriticalUsage(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyAdaptive)

	messages := makeMessages(20)
	// 96% usage > 95% aggressive threshold
	result := cp.pruneAdaptive(messages, 96000, 100000, nil, "anthropic")
	if len(result) == 0 {
		t.Error("should return some messages in critical mode")
	}
}

func TestConversationPruner_pruneAdaptive_DefaultPath(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyAdaptive)

	messages := makeMessages(10)
	// 50% usage, no long history, no large files -> importance-based
	result := cp.pruneAdaptive(messages, 5000, 10000, nil, "anthropic")
	// Importance-based on small messages returns them all
	if len(result) == 0 {
		t.Error("should return some messages in default mode")
	}
}

func TestConversationPruner_pruneAdaptive_HasLargeFileReads(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyAdaptive)

	// Build messages with a large file read
	bigContent := make([]byte, 6000)
	for i := range bigContent {
		bigContent[i] = 'a'
	}

	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "read file"},
		{Role: "tool", Content: "Tool call result for read_file\n" + string(bigContent)},
	}

	result := cp.pruneAdaptive(messages, 8100, 10000, nil, "anthropic")
	// 81% > 80%, has large file reads -> deduplication path
	if len(result) == 0 {
		t.Error("should return some messages")
	}
}

func TestConversationPruner_PruneConversation_NoPruneNeeded(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)

	messages := makeMessages(5)
	result := cp.PruneConversation(messages, 500, 10000, nil, "anthropic", false)
	if len(result) != len(messages) {
		t.Errorf("expected all messages when below threshold, got %d", len(result))
	}
}

func TestConversationPruner_PruneConversation_SlidingWindow(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	cp.SetSlidingWindowSize(5)
	cp.SetThreshold(0.5)

	messages := makeMessages(20)
	result := cp.PruneConversation(messages, 900, 1000, nil, "anthropic", false)
	if len(result) > len(messages) {
		t.Error("pruned result should not exceed original size")
	}
}

func TestConversationPruner_PruneConversation_MinMessagesEnforced(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategySlidingWindow)
	cp.SetSlidingWindowSize(2)
	cp.minMessagesToKeep = 5
	cp.recentMessagesToKeep = 3

	messages := makeMessages(20)
	result := cp.PruneConversation(messages, 900, 1000, nil, "anthropic", false)
	// Should enforce minMessagesToKeep
	if len(result) < 5 {
		t.Errorf("should keep at least %d messages, got %d", 5, len(result))
	}
}

func TestConversationPruner_pruneByImportance_ProviderCases(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.SetStrategy(PruneStrategyImportance)
	cp.SetRecentMessagesToKeep(5)

	messages := makeMessages(20)

	// Test different providers (all should work)
	for _, provider := range []string{"anthropic", "openai", "minimax", "deepseek"} {
		result := cp.pruneByImportance(messages, provider, 100000)
		if len(result) == 0 {
			t.Errorf("provider %s: should return some messages", provider)
		}
	}
}

func TestConversationPruner_countToolCalls(t *testing.T) {
	cp := NewConversationPruner(false)

	// No tool calls
	messages := makeMessages(5)
	if cp.countToolCalls(messages) != 0 {
		t.Error("should count 0 tool calls for non-tool messages")
	}

	// With tool messages
	messages = []api.Message{
		{Role: "tool", Content: "result"},
		{Role: "assistant", Content: "response", ToolCalls: []api.ToolCall{{ID: "call-1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "tool_name", Arguments: ""}}}},
		{Role: "tool", Content: "result 2"},
	}
	if got := cp.countToolCalls(messages); got != 3 {
		t.Errorf("countToolCalls = %d, want 3", got)
	}
}

func TestConversationPruner_hasLargeFileReads(t *testing.T) {
	cp := NewConversationPruner(false)

	// No large reads
	messages := makeMessages(5)
	if cp.hasLargeFileReads(messages) {
		t.Error("should return false for small messages")
	}

	// Large file read
	bigContent := make([]byte, 6000)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	messages = []api.Message{
		{Role: "tool", Content: "Tool call result for read_file\n" + string(bigContent)},
	}
	if !cp.hasLargeFileReads(messages) {
		t.Error("should return true for large file reads")
	}

	// Large content but not read_file
	messages = []api.Message{
		{Role: "tool", Content: "Tool call result for shell_command\n" + string(bigContent)},
	}
	if cp.hasLargeFileReads(messages) {
		t.Error("should return false for non-read_file large content")
	}
}

func TestConversationPruner_ensureRequiredHeadroom(t *testing.T) {
	cp := NewConversationPruner(false)
	cp.minMessagesToKeep = 3

	// Not enough messages to trim
	messages := makeMessages(2)
	result := cp.ensureRequiredHeadroom(messages, 10000, 5000)
	if len(result) != 2 {
		t.Error("should not trim when below minMessagesToKeep")
	}

	// maxTokens = 0
	result = cp.ensureRequiredHeadroom(messages, 0, 5000)
	if len(result) != 2 {
		t.Error("should not trim when maxTokens is 0")
	}

	// requiredAvailable = 0
	result = cp.ensureRequiredHeadroom(messages, 10000, 0)
	if len(result) != 2 {
		t.Error("should not trim when requiredAvailable is 0")
	}
}

func TestConversationPruner_scoreSingleMessage(t *testing.T) {
	cp := NewConversationPruner(false)

	tests := []struct {
		msg    api.Message
		expect float64
	}{
		{api.Message{Role: "system", Content: "sys"}, 1.0},
		{api.Message{Role: "user", Content: "hello"}, 0.6},
		{api.Message{Role: "user", Content: "error occurred"}, 0.8},
		{api.Message{Role: "tool", Content: "result"}, 0.5},
		{api.Message{Role: "tool", Content: "error in output"}, 0.8},
		{api.Message{Role: "assistant", Content: "response"}, 0.5},
		{api.Message{Role: "assistant", Content: "response", ToolCalls: []api.ToolCall{{ID: "1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "tool", Arguments: ""}}}}, 0.6},
	}

	for _, tt := range tests {
		got := cp.scoreSingleMessage(tt.msg)
		if got != tt.expect {
			t.Errorf("scoreSingleMessage(%q) = %f, want %f", tt.msg.Role, got, tt.expect)
		}
	}
}

func TestNewConversationPruner(t *testing.T) {
	cp := NewConversationPruner(true)

	if cp.strategy != PruneStrategyAdaptive {
		t.Errorf("default strategy = %s, want %s", cp.strategy, PruneStrategyAdaptive)
	}
	if cp.debug != true {
		t.Error("debug should be true")
	}
	if cp.contextThreshold != PruningConfig.Default.StandardPercent {
		t.Errorf("threshold = %f, want %f", cp.contextThreshold, PruningConfig.Default.StandardPercent)
	}
	if cp.minMessagesToKeep != PruningConfig.Default.MinMessages {
		t.Errorf("minMessagesToKeep = %d, want %d", cp.minMessagesToKeep, PruningConfig.Default.MinMessages)
	}
	if cp.recentMessagesToKeep != PruningConfig.Default.RecentMessages {
		t.Errorf("recentMessagesToKeep = %d, want %d", cp.recentMessagesToKeep, PruningConfig.Default.RecentMessages)
	}
	if cp.slidingWindowSize != PruningConfig.Default.SlidingWindow {
		t.Errorf("slidingWindowSize = %d, want %d", cp.slidingWindowSize, PruningConfig.Default.SlidingWindow)
	}
}

// makeMessages creates a slice of test messages with a system message first
func makeMessages(count int) []api.Message {
	msgs := make([]api.Message, count)
	msgs[0] = api.Message{Role: "system", Content: "system prompt"}
	for i := 1; i < count; i++ {
		msgs[i] = api.Message{
			Role:    "user",
			Content: fmt.Sprintf("msg-%d", i),
		}
	}
	return msgs
}
