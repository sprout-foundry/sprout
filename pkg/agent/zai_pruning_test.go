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
			expectedPrune: true,
			description:   "85% should trigger pruning for ZAI",
		},
		{
			name:          "ZAI At 100K Ceiling",
			currentTokens: 100000,
			provider:      "zai",
			expectedPrune: true,
			description:   "100K tokens should trigger pruning for ZAI (above 60% threshold)",
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
			currentTokens: 110000, // Close to limit to trigger cached-discount logic
			provider:      "openai",
			expectedPrune: true,
			description:   "110K should trigger pruning for OpenAI (cached-discount provider)",
		},
		{
			name:          "Other Provider Default",
			currentTokens: 70000,
			provider:      "anthropic",
			expectedPrune: true,
			description:   "70K should trigger pruning for other providers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldPrune := pruner.ShouldPrune(tt.currentTokens, maxTokens, tt.provider)
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
			expectedMin:  98800,
			expectedMax:  98800,
		},
		{
			name:         "ZAI Large Conversation",
			messageCount: 60,
			provider:     "zai",
			expectedMin:  88800,
			expectedMax:  88800,
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
			target := pruner.getTargetTokensForProvider(tt.messageCount, tt.provider)
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

	pruned := pruner.PruneConversation(messages, currentTokens, maxTokens, optimizer, "zai")

	if len(pruned) != len(messages) {
		t.Errorf("Expected no pruning for small conversation, got %d messages from %d original",
			len(pruned), len(messages))
	}
}
