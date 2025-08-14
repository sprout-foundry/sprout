package agent

import (
	"fmt"
	"runtime"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/utils"
)

// RunAgentMode is the main public interface for command line usage
func RunAgentMode(userIntent string, skipPrompt bool, model string) error {
	fmt.Printf("🤖 Agent mode: Analyzing your intent...\n")

	utils.LogUserPrompt(userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger := utils.GetLogger(false)
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	if model != "" {
		cfg.OrchestrationModel = model
	}
	cfg.SkipPrompt = skipPrompt
	// Enable interactive tool-calling for code flow (planner/executor/evaluator tools)
	cfg.Interactive = true
	cfg.CodeToolsEnabled = true
	_ = llm.InitPricingTable()

	fmt.Printf("🎯 Intent: %s\n", userIntent)
	logger := utils.GetLogger(cfg.SkipPrompt)

	overallStart := time.Now()
	_ = WriteRunSnapshot(cfg, fmt.Sprintf("v1-%d", overallStart.Unix()))
	tokenUsage, err := Execute(userIntent, cfg, logger)
	if err != nil {
		return err
	}
	overallDuration := time.Since(overallStart)
	PrintTokenUsageSummary(tokenUsage, overallDuration, cfg)
	fmt.Printf("✅ Agent execution completed\n")
	return nil
}

// Execute is the main public interface for running the agent
func Execute(userIntent string, cfg *config.Config, logger *utils.Logger) (*AgentTokenUsage, error) {
	logger.LogProcessStep("🚀 Starting optimized agent execution...")
	logger.LogProcessStep("🛡️ Policy version: " + PolicyVersion)

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: agent.Execute started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	tokenUsage := &AgentTokenUsage{}
	logger.LogProcessStep("🔄 Executing optimized agent...")
	err := runOptimizedAgent(userIntent, cfg, logger, tokenUsage)
	if err != nil {
		logger.LogError(fmt.Errorf("agent execution failed: %w", err))
		return tokenUsage, fmt.Errorf("agent execution failed: %w", err)
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: agent.Execute completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

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
	_ = intentCost + planningCost + progressCost + codegenCost + validationCost // computations done in PrintTokenUsageSummary too
	return tokenUsage, nil
}
