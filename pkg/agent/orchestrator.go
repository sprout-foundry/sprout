package agent

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// runOptimizedAgent runs the agent with adaptive decision-making and progress evaluation
func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *AgentTokenUsage) error {
	logger.LogProcessStep("CHECKPOINT: Starting adaptive agent execution")

	context := &AgentContext{
		UserIntent:         userIntent,
		ExecutedOperations: []string{},
		Errors:             []string{},
		ValidationResults:  []string{},
		IterationCount:     0,
		MaxIterations:      25,
		StartTime:          time.Now(),
		TokenUsage:         tokenUsage,
		Config:             cfg,
		Logger:             logger,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	for context.IterationCount < context.MaxIterations {
		context.IterationCount++
		logger.LogProcessStep(fmt.Sprintf(" Agent Iteration %d/%d", context.IterationCount, context.MaxIterations))

		evaluation, evalTokens, err := evaluateProgress(context)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to evaluate progress: %w", err))
			context.Errors = append(context.Errors, fmt.Sprintf("Progress evaluation failed: %v", err))
			evaluation = &ProgressEvaluation{Status: "needs_adjustment", NextAction: "continue", Reasoning: "Fallback due to evaluation failure"}
		}
		context.TokenUsage.ProgressEvaluation += evalTokens

		logger.LogProcessStep(fmt.Sprintf("📊 Progress Status: %s (%d%% complete)", evaluation.Status, evaluation.CompletionPercentage))
		logger.LogProcessStep(fmt.Sprintf("🎯 Next Action: %s", evaluation.NextAction))
		logger.LogProcessStep(fmt.Sprintf("🤔 Reasoning: %s", evaluation.Reasoning))

		if len(evaluation.Concerns) > 0 && evaluation.Status == "critical_error" {
			logger.LogProcessStep("⚠️ Critical concerns:")
			for _, concern := range evaluation.Concerns {
				logger.LogProcessStep(fmt.Sprintf("   • %s", concern))
			}
		}

		switch evaluation.NextAction {
		case "analyze_intent":
			intentAnalysis, tokens, e := analyzeIntentWithMinimalContext(context.UserIntent, context.Config, context.Logger)
			if e != nil {
				err = fmt.Errorf("intent analysis failed: %w", e)
			} else {
				context.IntentAnalysis = intentAnalysis
				context.TokenUsage.IntentAnalysis += tokens
				context.TokenUsage.IntentSplit.Prompt += tokens
				context.ExecutedOperations = append(context.ExecutedOperations, "Intent analysis completed")
				cmdToRun := ""
				if intentAnalysis != nil && intentAnalysis.CanExecuteNow && strings.TrimSpace(intentAnalysis.ImmediateCommand) != "" {
					cmdToRun = strings.TrimSpace(intentAnalysis.ImmediateCommand)
				} else if isSimpleShellCommand(context.UserIntent) {
					cmdToRun = strings.TrimSpace(context.UserIntent)
				}
				if cmdToRun != "" {
					context.Logger.LogProcessStep("🚀 Fast path: executing immediate shell command")
					if err := executeShellCommands(context, []string{cmdToRun}); err != nil {
						context.Logger.LogError(fmt.Errorf("immediate command failed: %w", err))
					} else {
						context.ExecutedOperations = append(context.ExecutedOperations, "Task completed via immediate command execution: "+cmdToRun)
						context.IsCompleted = true
					}
				}
				err = nil
			}
		case "create_plan":
			err = executeCreatePlan(context)
		case "execute_edits":
			err = executeEditOperations(context)
		case "run_command":
			err = executeShellCommands(context, evaluation.Commands)
		case "validate":
			err = executeValidation(context)
		case "revise_plan":
			err = executeRevisePlan(context, evaluation)
		case "completed":
			context.Logger.LogProcessStep("✅ Task marked as completed by agent evaluation")
			context.IsCompleted = true
			err = nil
		case "continue":
			context.Logger.LogProcessStep("▶️ Continuing with current plan")
			err = nil
		default:
			err = fmt.Errorf("unknown action: %s", evaluation.NextAction)
		}
		if err != nil {
			context.Errors = append(context.Errors, fmt.Sprintf("Action execution failed: %v", err))
			logger.LogError(fmt.Errorf("action execution failed: %w", err))
			return fmt.Errorf("agent execution failed: %w", err)
		}

		if context.IsCompleted {
			logger.LogProcessStep("✅ Agent determined task is completed successfully")
			break
		}
		if evaluation.Status == "completed" || evaluation.NextAction == "completed" {
			context.Logger.LogProcessStep("✅ Agent determined task is completed successfully")
			context.IsCompleted = true
			break
		}

		if err := summarizeContextIfNeeded(context); err != nil {
			logger.LogProcessStep(fmt.Sprintf("⚠️ Context summarization failed: %v", err))
		}
		if context.IterationCount == context.MaxIterations {
			logger.LogProcessStep("⚠️ Maximum iterations reached, completing execution")
			break
		}
	}

	duration := time.Since(context.StartTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	logger.LogProcessStep(fmt.Sprintf("🎉 Adaptive agent execution completed in %d iterations", context.IterationCount))
	return nil
}
