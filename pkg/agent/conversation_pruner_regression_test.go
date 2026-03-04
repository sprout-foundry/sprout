package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestShouldPruneHonorsConfiguredThreshold(t *testing.T) {
	pruner := NewConversationPruner(false)
	pruner.SetThreshold(0.5)

	if !pruner.ShouldPrune(16000, 32000, "anthropic", false) {
		t.Fatalf("expected threshold override to trigger prune at 50%% usage")
	}
}

func TestTargetTokensRespectMaxContext(t *testing.T) {
	pruner := NewConversationPruner(false)

	target := pruner.getTargetTokensForProvider(10, "anthropic", 32000)
	if target <= 0 || target > 32000 {
		t.Fatalf("invalid target token budget: %d", target)
	}
	// 85% target for default providers on small histories.
	if target != 27200 {
		t.Fatalf("unexpected target for 32k context: got %d want 27200", target)
	}
}

func TestShouldPruneUsesPercentageNotAbsoluteFloor(t *testing.T) {
	pruner := NewConversationPruner(false)

	// 110k used to trigger pruning via absolute threshold.
	// With percentage-based triggering, this should not prune for a 200k model.
	if pruner.ShouldPrune(110000, 200000, "openai", false) {
		t.Fatalf("expected no prune at 55%% usage for 200k context")
	}
}

func TestShouldPruneOnMinAvailableTokens(t *testing.T) {
	pruner := NewConversationPruner(false)

	// 50k/56k is below 92% usage, but remaining is only 6k, which should trigger.
	if !pruner.ShouldPrune(50000, 56000, "anthropic", false) {
		t.Fatalf("expected prune when remaining tokens are below minimum available threshold")
	}
}

func TestShouldPruneAgenticHeadroom(t *testing.T) {
	pruner := NewConversationPruner(false)

	// Remaining 11k > 8k minimum-available but <12k agentic-required headroom.
	if pruner.ShouldPrune(89000, 100000, "anthropic", false) {
		t.Fatalf("did not expect non-agentic prune at 89%% with remaining 11k")
	}
	if !pruner.ShouldPrune(89000, 100000, "anthropic", true) {
		t.Fatalf("expected agentic prune due to required headroom")
	}
}

func TestToolCallAwarePruningDoesNotDuplicateFirstMessage(t *testing.T) {
	pruner := NewConversationPruner(false)
	pruner.SetStrategy(PruneStrategyImportance)

	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "question"},
		{
			Role:    "assistant",
			Content: "calling tools",
			ToolCalls: []api.ToolCall{
				{ID: "call-1"},
			},
		},
		{Role: "tool", Content: "Tool call result for read_file: a.go\nx", ToolCallId: "call-1"},
		{Role: "assistant", Content: "answer"},
	}

	// Force pruning path and tool-aware logic.
	out := pruner.PruneConversation(messages, 120000, 128000, NewConversationOptimizer(true, false), "deepseek", false)
	if len(out) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if out[0].Role != "system" || out[0].Content != "system" {
		t.Fatalf("unexpected first message: %+v", out[0])
	}
	systemCount := 0
	for _, m := range out {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Fatalf("expected exactly one system message, got %d", systemCount)
	}
}

func TestPruningFallbackDoesNotDuplicateMessages(t *testing.T) {
	pruner := NewConversationPruner(false)
	pruner.SetStrategy(PruneStrategySlidingWindow)
	pruner.SetSlidingWindowSize(1)
	pruner.SetRecentMessagesToKeep(5)

	messages := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "1"},
		{Role: "assistant", Content: "2"},
		{Role: "user", Content: "3"},
		{Role: "assistant", Content: "4"},
		{Role: "user", Content: "5"},
		{Role: "assistant", Content: "6"},
	}

	out := pruner.PruneConversation(messages, 90000, 100000, NewConversationOptimizer(true, false), "anthropic", false)
	seen := make(map[string]struct{}, len(out))
	for _, m := range out {
		key := m.Role + "|" + m.Content
		if _, ok := seen[key]; ok {
			t.Fatalf("found duplicate message in fallback output: %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestToolRoleDetectors(t *testing.T) {
	pruner := NewConversationPruner(false)

	messages := []api.Message{
		{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{ID: "call-1"},
			},
		},
		{Role: "tool", Content: "Tool call result for read_file: x.go\n" + strings.Repeat("a", 6001), ToolCallId: "call-1"},
	}

	if pruner.countToolCalls(messages) < 2 {
		t.Fatalf("expected tool call counter to include assistant tool_calls and tool results")
	}
	if !pruner.hasLargeFileReads(messages) {
		t.Fatalf("expected large tool read detection for role=tool")
	}
}

func TestPruneAdaptiveNilOptimizerDoesNotPanic(t *testing.T) {
	pruner := NewConversationPruner(false)
	pruner.SetStrategy(PruneStrategyAdaptive)

	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "response"},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	out := pruner.PruneConversation(messages, 95000, 100000, nil, "anthropic", false)
	if len(out) == 0 {
		t.Fatalf("expected non-empty output")
	}
}

func TestPruneConversationEnforcesAgenticRequiredHeadroom(t *testing.T) {
	pruner := NewConversationPruner(false)
	pruner.SetStrategy(PruneStrategySlidingWindow)
	pruner.SetSlidingWindowSize(10)

	// Build sizable history to allow trimming while keeping minimum messages.
	messages := []api.Message{{Role: "system", Content: "system"}}
	for i := 0; i < 40; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: strings.Repeat("token ", 400),
		})
	}

	current := pruner.estimateTokens(messages)
	nonAgentic := pruner.PruneConversation(messages, current, 100000, NewConversationOptimizer(true, false), "anthropic", false)
	agentic := pruner.PruneConversation(messages, current, 100000, NewConversationOptimizer(true, false), "anthropic", true)

	nonAgenticRemaining := 100000 - pruner.estimateTokens(nonAgentic)
	agenticRemaining := 100000 - pruner.estimateTokens(agentic)

	if agenticRemaining < PruningConfig.AgenticRequiredAvailableTokens {
		t.Fatalf("expected agentic pruning to enforce required headroom, got remaining=%d", agenticRemaining)
	}
	if agenticRemaining < nonAgenticRemaining {
		t.Fatalf("expected agentic pruning to keep at least as much free headroom as non-agentic")
	}
}
