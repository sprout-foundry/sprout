//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/llm"
)

func main() {
	fmt.Println("=== Testing Conversation Pruning Strategies ===")

	// Create test messages
	messages := createTestMessages()

	// Create pruner
	pruner := agent.NewConversationPruner(true) // debug mode
	optimizer := agent.NewConversationOptimizer(true, true)

	// Test different strategies
	strategies := []agent.PruningStrategy{
		agent.PruneStrategySlidingWindow,
		agent.PruneStrategyImportance,
		agent.PruneStrategyHybrid,
		agent.PruneStrategyAdaptive,
	}

	for _, strategy := range strategies {
		fmt.Printf("\n--- Testing %s Strategy ---\n", strategy)
		pruner.SetStrategy(strategy)
		pruner.SetThreshold(0.5) // Prune at 50% for testing

		// Estimate tokens
		totalTokens := 0
		for _, msg := range messages {
			totalTokens += llm.EstimateTokens(msg.Content)
		}

		maxTokens := totalTokens * 2 // Set max to 2x current for testing
		currentTokens := totalTokens

		fmt.Printf("Messages: %d, Tokens: %d, Max: %d\n", len(messages), currentTokens, maxTokens)

		// Test pruning
		pruned := pruner.PruneConversation(messages, currentTokens, maxTokens, optimizer)

		prunedTokens := 0
		for _, msg := range pruned {
			prunedTokens += llm.EstimateTokens(msg.Content)
		}

		fmt.Printf("After pruning: %d messages, %d tokens (%.1f%% reduction)\n",
			len(pruned), prunedTokens,
			float64(totalTokens-prunedTokens)/float64(totalTokens)*100)
	}

	fmt.Println("\n✅ Pruning test complete!")
}

func createTestMessages() []api.Message {
	messages := []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Help me build a web application"},
		{Role: "assistant", Content: "I'll help you build a web application. Let me start by understanding your requirements."},
		{Role: "user", Content: "I need a React app with user authentication"},
		{Role: "assistant", Content: "I'll help you create a React app with user authentication. Let me set up the project structure."},
	}

	// Add some tool results to simulate real conversation
	for i := 0; i < 10; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Tool call result for read_file: src/component%d.js\nconst Component%d = () => {\n  // Component code here...\n  return <div>Component %d</div>;\n}", i, i, i),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("I've reviewed component%d.js. Let me check the next file.", i),
		})
	}

	// Add some recent important messages
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Error: Cannot find module 'react-router-dom'",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I see the error. Let me install the missing dependency and fix the import.",
	})

	return messages
}
