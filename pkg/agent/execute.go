package agent

import (
	"fmt"
	"runtime"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Execute is the main public interface for running the agent loop programmatically.
// Retained for orchestration and multi-agent callers; routes to the optimized v2 loop.
func Execute(userIntent string, cfg *config.Config, logger *utils.Logger) (*types.AgentTokenUsage, error) {
	logger.LogProcessStep("🚀 Starting optimized agent execution (v2)...")
	logger.LogProcessStep("🛡️ Policy version: " + PolicyVersion)

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: agent.Execute started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	tokenUsage := &types.AgentTokenUsage{}
	logger.LogProcessStep("🔄 Executing optimized agent...")
	err := RunSimplifiedAgent(userIntent, cfg.SkipPrompt, cfg.OrchestrationModel)
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

	// Pricing summary
	intentCost := llm.CalculateCost(intentUsage, orchestratorModel)
	planningCost := llm.CalculateCost(planningUsage, orchestratorModel)
	progressCost := llm.CalculateCost(progressUsage, orchestratorModel)
	codegenCost := llm.CalculateCost(codegenUsage, editingModel)
	validationCost := llm.CalculateCost(validationUsage, editingModel)
	totalCost := intentCost + planningCost + progressCost + codegenCost + validationCost
	totalTokens := intentUsage.TotalTokens + planningUsage.TotalTokens + progressUsage.TotalTokens + codegenUsage.TotalTokens + validationUsage.TotalTokens
	logger.LogProcessStep(fmt.Sprintf("💰 Agent total: tokens=%d, cost=$%.4f (intent=%.4f, plan=%.4f, progress=%.4f, code=%.4f, validate=%.4f)", totalTokens, totalCost, intentCost, planningCost, progressCost, codegenCost, validationCost))
	return tokenUsage, nil
}
