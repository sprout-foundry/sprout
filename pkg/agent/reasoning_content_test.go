package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestReasoningContentPreservation(t *testing.T) {
	debug := true
	optimizer := NewConversationOptimizer(true, debug)
	pruner := NewConversationPruner(debug)

	// Create test messages with reasoning content
	messages := []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Help me understand this code."},
		{
			Role:             "assistant",
			Content:          "I'll analyze the code structure for you.",
			ReasoningContent: "The user wants to understand the codebase. I should look at the main files and provide a clear explanation.",
		},
		{
			Role:    "tool",
			Content: "Tool call result for read_file: main.go\npackage main\n\nfunc main() {\n    fmt.Println(\"Hello World\")\n}",
		},
		{
			Role:             "assistant",
			Content:          "This is a simple Hello World program.",
			ReasoningContent: "The code is straightforward - just a main function that prints Hello World. I should explain this clearly.",
		},
	}

	// Test optimization preserves reasoning content
	optimized := optimizer.OptimizeConversation(messages)

	for i, msg := range optimized {
		if i == 2 || i == 4 { // Assistant messages with reasoning
			if msg.ReasoningContent == "" {
				t.Errorf("Reasoning content was lost during optimization at message %d", i)
			}
			if msg.ReasoningContent != messages[i].ReasoningContent {
				t.Errorf("Reasoning content was modified during optimization at message %d: got %q, want %q",
					i, msg.ReasoningContent, messages[i].ReasoningContent)
			}
		}
	}

	// Test pruning preserves reasoning content
	maxTokens := 128000
	currentTokens := 1000 // Small amount to avoid triggering pruning

	pruned := pruner.PruneConversation(optimized, currentTokens, maxTokens, optimizer, "zai")

	for i, msg := range pruned {
		if i == 2 || i == 4 { // Assistant messages with reasoning
			if msg.ReasoningContent == "" {
				t.Errorf("Reasoning content was lost during pruning at message %d", i)
			}
			if msg.ReasoningContent != messages[i].ReasoningContent {
				t.Errorf("Reasoning content was modified during pruning at message %d: got %q, want %q",
					i, msg.ReasoningContent, messages[i].ReasoningContent)
			}
		}
	}

	// Test token estimation includes reasoning content
	originalTokens := pruner.estimateTokens(messages)
	optimizedTokens := pruner.estimateTokens(optimized)
	prunedTokens := pruner.estimateTokens(pruned)

	if optimizedTokens > originalTokens {
		t.Errorf("Optimization should not increase tokens: got %d, want <= %d", optimizedTokens, originalTokens)
	}

	if prunedTokens > optimizedTokens {
		t.Errorf("Pruning should not increase tokens: got %d, want <= %d", prunedTokens, optimizedTokens)
	}

	t.Logf("Token counts - Original: %d, Optimized: %d, Pruned: %d",
		originalTokens, optimizedTokens, prunedTokens)
}

func TestReasoningContentInTokenCalculation(t *testing.T) {
	pruner := NewConversationPruner(false)

	messages := []api.Message{
		{
			Role:             "assistant",
			Content:          "Simple response",
			ReasoningContent: "Complex reasoning content that should be counted in tokens",
		},
	}

	tokens := pruner.estimateTokens(messages)

	// Should count both content and reasoning content
	expectedMin := len("Simple response") + len("Complex reasoning content that should be counted in tokens")
	expectedTokens := expectedMin / 4 // Rough token estimate

	if tokens < expectedTokens-1 { // Allow for rounding differences
		t.Errorf("Token estimation too low: got %d, expected at least %d", tokens, expectedTokens)
	}

	t.Logf("Message tokens: %d (content: %d chars, reasoning: %d chars)",
		tokens, len("Simple response"), len("Complex reasoning content that should be counted in tokens"))
}
