package agent

import (
	"encoding/json" // New import for JSON operations
	"fmt"
	"os"            // New import for file system operations
	"path/filepath" // New import for path manipulation
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
)

// AgentRunCost represents the cost and token usage for a single agent run.
type AgentRunCost struct {
	Timestamp   time.Time `json:"timestamp"`
	TotalTokens int       `json:"total_tokens"`
	TotalCost   float64   `json:"total_cost"`
	DurationMs  int64     `json:"duration_ms"` // Duration in milliseconds
}

// AgentCostHistory is a collection of AgentRunCost entries.
type AgentCostHistory []AgentRunCost

// PrintTokenUsageSummary prints a summary of token usage and costs
func PrintTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration, cfg *config.Config) {
	printTokenUsageSummary(tokenUsage, duration, cfg)
}

// printTokenUsageSummary prints a summary of token usage and costs for the agent execution
func printTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration, cfg *config.Config) {
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

	// --- Cost Summary ---
	orchestratorModel := cfg.OrchestrationModel
	if orchestratorModel == "" {
		orchestratorModel = cfg.EditingModel
	}
	editingModel := cfg.EditingModel

	buildUsage := func(prompt, completion int) llm.TokenUsage {
		return llm.TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	}
	intentUsage := buildUsage(tokenUsage.IntentSplit.Prompt, tokenUsage.IntentSplit.Completion)
	if intentUsage.TotalTokens == 0 && tokenUsage.IntentAnalysis > 0 {
		intentUsage = buildUsage(tokenUsage.IntentAnalysis, 0)
	}
	planningUsage := buildUsage(tokenUsage.PlanningSplit.Prompt, tokenUsage.PlanningSplit.Completion)
	if planningUsage.TotalTokens == 0 && tokenUsage.Planning > 0 {
		planningUsage = buildUsage(tokenUsage.Planning, 0)
	}
	progressUsage := buildUsage(tokenUsage.ProgressSplit.Prompt, tokenUsage.ProgressSplit.Completion)
	if progressUsage.TotalTokens == 0 && tokenUsage.ProgressEvaluation > 0 {
		progressUsage = buildUsage(tokenUsage.ProgressEvaluation, 0)
	}
	codegenUsage := buildUsage(tokenUsage.CodegenSplit.Prompt, tokenUsage.CodegenSplit.Completion)
	if codegenUsage.TotalTokens == 0 && tokenUsage.CodeGeneration > 0 {
		codegenUsage = buildUsage(tokenUsage.CodeGeneration, 0)
	}
	validationUsage := buildUsage(tokenUsage.ValidationSplit.Prompt, tokenUsage.ValidationSplit.Completion)
	if validationUsage.TotalTokens == 0 && tokenUsage.Validation > 0 {
		validationUsage = buildUsage(tokenUsage.Validation, 0)
	}

	intentCost := llm.CalculateCost(intentUsage, orchestratorModel)
	planningCost := llm.CalculateCost(planningUsage, orchestratorModel)
	progressCost := llm.CalculateCost(progressUsage, orchestratorModel)
	codegenCost := llm.CalculateCost(codegenUsage, editingModel)
	validationCost := llm.CalculateCost(validationUsage, editingModel)
	totalCost := intentCost + planningCost + progressCost + codegenCost + validationCost

	fmt.Printf("\n💵 Cost Summary:\n")
	fmt.Printf("├─ Intent Analysis (%s): $%.4f\n", orchestratorModel, intentCost)
	fmt.Printf("├─ Planning (%s): $%.4f\n", orchestratorModel, planningCost)
	fmt.Printf("├─ Progress Evaluation (%s): $%.4f\n", orchestratorModel, progressCost)
	fmt.Printf("├─ Code Generation (%s): $%.4f\n", editingModel, codegenCost)
	fmt.Printf("├─ Validation (%s): $%.4f\n", editingModel, validationCost)

	// Calculate current run cost and add to history
	currentRunCost := totalCost
	fmt.Printf("├─ Current Run Cost: $%.4f\n", currentRunCost)

	runCostEntry := AgentRunCost{
		Timestamp:   time.Now(),
		TotalTokens: tokenUsage.Total,
		TotalCost:   currentRunCost,
		DurationMs:  duration.Milliseconds(),
	}

	history, err := loadAgentCostHistory()
	if err != nil {
		fmt.Printf("Error loading cost history: %v\n", err)
		// If loading fails, history will be nil. append will create a new slice.
		// This means only the current run will be saved if previous history was unreadable.
	}

	history = append(history, runCostEntry)

	if err := saveAgentCostHistory(history); err != nil {
		fmt.Printf("Error saving cost history: %v\n", err)
	}

	// Calculate aggregated total cost from history
	aggregatedTotalCost := 0.0
	for _, entry := range history {
		aggregatedTotalCost += entry.TotalCost
	}

	fmt.Printf("└─ Aggregated Total Cost: $%.4f\n", aggregatedTotalCost)
}

// getAgentCostFilePath returns the full path to the agent cost history file.
func getAgentCostFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	leditDir := filepath.Join(homeDir, ".ledit")
	// Create the .ledit directory if it doesn't exist
	if err := os.MkdirAll(leditDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .ledit directory: %w", err)
	}
	return filepath.Join(leditDir, "agent_costs.json"), nil
}

// loadAgentCostHistory loads the agent cost history from the file.
func loadAgentCostHistory() (AgentCostHistory, error) {
	filePath, err := getAgentCostFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return AgentCostHistory{}, nil // File doesn't exist, return empty history
		}
		return nil, fmt.Errorf("failed to read agent cost history file: %w", err)
	}

	var history AgentCostHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent cost history: %w", err)
	}
	return history, nil
}

// saveAgentCostHistory saves the agent cost history to the file.
func saveAgentCostHistory(history AgentCostHistory) error {
	filePath, err := getAgentCostFilePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(history, "", "  ") // Use MarshalIndent for pretty printing
	if err != nil {
		return fmt.Errorf("failed to marshal agent cost history: %w", err)
	}

	// Write the file with read/write permissions for owner, read-only for others
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent cost history file: %w", err)
	}
	return nil
}
