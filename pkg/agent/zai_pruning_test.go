package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestZAIPruningThresholds(t *testing.T) {
	debug := false
	pruner := NewConversationPruner(debug)

	// Test thresholds - all providers now use the same percentage-based thresholds
	maxTokens := 128000 // Example context limit

	tests := []struct {
		name          string
		currentTokens int
		provider      string
		expectedPrune bool
		description   string
	}{
		{
			name:          "Low Context",
			currentTokens: 50000,
			provider:      "zai",
			expectedPrune: false,
			description:   "50K tokens should not trigger pruning (below 90%)",
		},
		{
			name:          "At 85% Threshold",
			currentTokens: 108800, // 85% of 128K
			provider:      "zai",
			expectedPrune: false,
			description:   "85% should not trigger pruning (below 90%)",
		},
		{
			name:          "At 89% Threshold",
			currentTokens: 113920, // 89% of 128K
			provider:      "zai",
			expectedPrune: false,
			description:   "89% should not trigger pruning (below 90%)",
		},
		{
			name:          "At 90% Threshold",
			currentTokens: 115200, // 90% of 128K
			provider:      "zai",
			expectedPrune: false,
			description:   "90% should NOT trigger pruning (threshold uses > not >=)",
		},
		{
			name:          "At 91% Threshold",
			currentTokens: 116480, // 91% of 128K
			provider:      "zai",
			expectedPrune: true,
			description:   "91% should trigger pruning (above 90%)",
		},
		{
			name:          "Other Provider Default",
			currentTokens: 110000,
			provider:      "openai",
			expectedPrune: false,
			description:   "110K/128K = 86% should not trigger pruning",
		},
		{
			name:          "All Providers Same",
			currentTokens: 116000,
			provider:      "anthropic",
			expectedPrune: true,
			description:   "All providers use same thresholds now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldPrune := pruner.ShouldPrune(tt.currentTokens, maxTokens, tt.provider, false)
			if shouldPrune != tt.expectedPrune {
				t.Errorf("ShouldPrune() = %v, expected %v. %s",
					shouldPrune, tt.expectedPrune, tt.description)
			}
		})
	}
}

func TestZAITargetTokens(t *testing.T) {
	pruner := NewConversationPruner(false)

	tests := []struct {
		name         string
		messageCount int
		provider     string
		expectedMin  int
		expectedMax  int
	}{
		{
			name:         "ZAI Small Conversation",
			messageCount: 10,
			provider:     "zai",
			expectedMin:  108800,
			expectedMax:  108800,
		},
		{
			name:         "ZAI Medium Conversation",
			messageCount: 30,
			provider:     "zai",
			expectedMin:  98560,
			expectedMax:  98560,
		},
		{
			name:         "ZAI Large Conversation",
			messageCount: 60,
			provider:     "zai",
			expectedMin:  89600,
			expectedMax:  89600,
		},
		{
			name:         "OpenAI High Threshold",
			messageCount: 10,
			provider:     "openai",
			expectedMin:  108800,
			expectedMax:  108800,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := pruner.getTargetTokensForProvider(tt.messageCount, tt.provider, 128000)
			if target < tt.expectedMin || target > tt.expectedMax {
				t.Errorf("getTargetTokensForProvider() = %v, expected between %v and %v",
					target, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestZAIPruningIntegration(t *testing.T) {
	debug := true
	pruner := NewConversationPruner(debug)
	optimizer := NewConversationOptimizer(true, debug)

	// Create test messages
	messages := []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Help me understand this codebase."},
		{Role: "assistant", Content: "I'll help you analyze the codebase structure."},
	}

	// Test with ZAI provider - should not prune small conversation
	maxTokens := 128000
	currentTokens := 1000 // Small conversation

	pruned := pruner.PruneConversation(messages, currentTokens, maxTokens, optimizer, "zai", false)

	if len(pruned) != len(messages) {
		t.Errorf("Expected no pruning for small conversation, got %d messages from %d original",
			len(pruned), len(messages))
	}
}
