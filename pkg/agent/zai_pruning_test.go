package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestZAIPruningThresholds(t *testing.T) {
	debug := true
	pruner := NewConversationPruner(debug)

	// Test ZAI-specific thresholds
	maxTokens := 128000 // ZAI's context limit

	tests := []struct {
		name          string
		currentTokens int
		provider      string
		expectedPrune bool
		description   string
	}{
		{
			name:          "ZAI Low Context",
			currentTokens: 50000,
			provider:      "zai",
			expectedPrune: false,
			description:   "50K tokens should not trigger pruning for ZAI",
		},
		{
			name:          "ZAI At 85% Threshold",
			currentTokens: 108800, // 85% of 128K
			provider:      "zai",
			expectedPrune: false,
			description:   "85% should not trigger pruning with 92% threshold",
		},
		{
			name:          "ZAI At 100K Tokens",
			currentTokens: 100000,
			provider:      "zai",
			expectedPrune: false,
			description:   "100K tokens should not trigger pruning with 92% and sufficient remaining headroom",
		},
		{
			name:          "ZAI At 90% Threshold",
			currentTokens: 115200, // 90% of 128K
			provider:      "zai",
			expectedPrune: false,
			description:   "90% should not trigger pruning with 92% threshold",
		},
		{
			name:          "ZAI At 92% Threshold",
			currentTokens: 117760, // 92% of 128K
			provider:      "zai",
			expectedPrune: true,
			description:   "92% should trigger pruning",
		},
		{
			name:          "ZAI Remaining Below Min Available",
			currentTokens: 121500, // leaves <8K
			provider:      "zai",
			expectedPrune: true,
			description:   "remaining tokens below min available threshold should trigger pruning",
		},
		{
			name:          "ZAI Below Thresholds",
			currentTokens: 70000,
			provider:      "zai",
			expectedPrune: false,
			description:   "70K tokens should not trigger pruning for ZAI (below 60% threshold)",
		},
		{
			name:          "OpenAI Default Behavior",
			currentTokens: 110000,
			provider:      "openai",
			expectedPrune: false,
			description:   "110K should not trigger pruning when under 92% and with sufficient remaining tokens",
		},
		{
			name:          "Other Provider Default",
			currentTokens: 70000,
			provider:      "anthropic",
			expectedPrune: false,
			description:   "70K should not trigger pruning for other providers (below 92%)",
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
