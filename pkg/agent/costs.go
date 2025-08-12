package agent

import (
	"fmt"
	"time"
)

// PrintTokenUsageSummary prints a summary of token usage
func PrintTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration) {
	printTokenUsageSummary(tokenUsage, duration)
}

// printTokenUsageSummary prints a summary of token usage for the agent execution
func printTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration) {
	fmt.Printf("\n💰 Token Usage Summary:\n")
	fmt.Printf("├─ Intent Analysis: %d tokens\n", tokenUsage.IntentAnalysis)
	fmt.Printf("├─ Planning (Orchestration): %d tokens\n", tokenUsage.Planning)
	fmt.Printf("├─ Code Generation (Editing): %d tokens\n", tokenUsage.CodeGeneration)
	fmt.Printf("├─ Validation: %d tokens\n", tokenUsage.Validation)
	fmt.Printf("├─ Progress Evaluation: %d tokens\n", tokenUsage.ProgressEvaluation)

	if tokenUsage.Total == 0 {
		tokenUsage.Total = tokenUsage.IntentAnalysis + tokenUsage.Planning + tokenUsage.CodeGeneration + tokenUsage.Validation + tokenUsage.ProgressEvaluation
	}
	fmt.Printf("└─ Total Usage: %d tokens\n", tokenUsage.Total)
	tokensPerSecond := float64(tokenUsage.Total) / duration.Seconds()
	fmt.Printf("⚡ Performance: %.1f tokens/second\n", tokensPerSecond)
}
