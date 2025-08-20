package agent

import (
	"fmt"
	"runtime"
	"strconv"
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

	// Resume support: if a prior state exists, resume in skip-prompt mode automatically
	if HasSavedAgentState() {
		if cfg.SkipPrompt {
			if st, err := LoadAgentState(); err == nil {
				logger.LogProcessStep("🔁 Resuming prior agent state from .ledit/run_state.json")
				st.applyTo(context)
			} else {
				logger.LogProcessStep("⚠️ Failed to load prior agent state; starting fresh")
			}
		} else {
			// Interactive: ask user
			if logger.AskForConfirmation("Resume prior agent run from .ledit/run_state.json?", true, false) {
				if st, err := LoadAgentState(); err == nil {
					logger.LogProcessStep("🔁 Resuming prior agent state from .ledit/run_state.json")
					st.applyTo(context)
				} else {
					logger.LogProcessStep("⚠️ Failed to load prior agent state; starting fresh")
				}
			}
		}
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	for context.IterationCount < context.MaxIterations {
		context.IterationCount++
		logger.LogProcessStep(fmt.Sprintf(" Agent Iteration %d/%d", context.IterationCount, context.MaxIterations))

		// Opportunistically run lightweight tools before LLM evaluation
		if context.IterationCount == 1 && (context.IntentAnalysis == nil || len(context.IntentAnalysis.EstimatedFiles) == 0) {
			_ = executeWorkspaceInfo(context)
			_ = executeListFiles(context, 10)
		}

		// Smart early action selection based on user intent patterns
		lower := strings.ToLower(context.UserIntent)
		userIntent := context.UserIntent

		// Pattern 1: Direct command execution - if it looks like a command or simple operation
		if isDirectCommandPattern(lower) {
			if commands := extractDirectCommands(userIntent); len(commands) > 0 {
				context.Logger.LogProcessStep("🚀 Detected direct command pattern - executing immediately")
				if err := executeShellCommands(context, commands); err == nil {
					context.ExecutedOperations = append(context.ExecutedOperations, "Direct command execution completed")
					context.IsCompleted = true
					break
				}
			}
		}

		// Pattern 2: Search/investigation - quick grep for obvious search terms
		if strings.Contains(lower, "find") || strings.Contains(lower, "search") || strings.Contains(lower, "grep") {
			terms := []string{}
			for _, w := range strings.Fields(lower) {
				if len(w) > 2 {
					terms = append(terms, w)
				}
			}
			if len(terms) > 0 {
				_ = executeGrepSearch(context, terms)
			}
		}

		// Pattern 3: Simple file operations - direct execution
		if isSimpleFileOperation(lower) {
			if commands := extractFileOperationCommands(userIntent); len(commands) > 0 {
				context.Logger.LogProcessStep("📁 Detected simple file operation - executing immediately")
				if err := executeShellCommands(context, commands); err == nil {
					context.ExecutedOperations = append(context.ExecutedOperations, "Direct file operation completed")
					context.IsCompleted = true
					break
				}
			}
		}

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

		// Telemetry hook
		if context.Config.TelemetryEnabled {
			logTelemetry(context.Config.TelemetryFile, telemetryEvent{
				Timestamp: time.Now(), Policy: PolicyVersion, Variant: context.Config.PolicyVariant,
				Intent: context.UserIntent, Iteration: context.IterationCount, Action: evaluation.NextAction, Status: evaluation.Status,
			})
		}

		// AB hook: allow switching small behaviors based on PolicyVariant (placeholder for future)
		_ = context.Config.PolicyVariant

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
			// Check if this is actually a command execution plan
			if context.CurrentPlan != nil && len(context.CurrentPlan.EditOperations) > 0 {
				if context.CurrentPlan.EditOperations[0].FilePath == "command" {
					// This is a command execution plan, not file editing
					err = executeCommandPlan(context)
				} else {
					// Regular file editing plan
					err = executeEditOperations(context)
				}
			} else {
				err = executeEditOperations(context)
			}
			// Validate that operations were recorded; otherwise, evaluator must not advance
			if err == nil && (context.CurrentPlan == nil || len(context.CurrentPlan.EditOperations) == 0) {
				return fmt.Errorf("executor JSON/plan invariant violation: no operations available post-execution")
			}
		case "run_command":
			err = executeShellCommands(context, evaluation.Commands)
		case "validate":
			err = executeValidation(context)
		case "revise_plan":
			err = executeRevisePlan(context, evaluation)
		case "workspace_info":
			err = executeWorkspaceInfo(context)
		case "grep_search":
			err = executeGrepSearch(context, evaluation.Commands)
		case "list_files":
			// commands[0] may specify a limit; default 10
			limit := 10
			if len(evaluation.Commands) > 0 {
				if v, perr := strconv.Atoi(strings.TrimSpace(evaluation.Commands[0])); perr == nil && v > 0 {
					limit = v
				}
			}
			err = executeListFiles(context, limit)
		case "micro_edit":
			err = executeMicroEdit(context)
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
			_ = ClearAgentState()
			break
		}
		if evaluation.Status == "completed" || evaluation.NextAction == "completed" {
			context.Logger.LogProcessStep("✅ Agent determined task is completed successfully")
			context.IsCompleted = true
			_ = ClearAgentState()
			break
		}

		if err := summarizeContextIfNeeded(context); err != nil {
			logger.LogProcessStep(fmt.Sprintf("⚠️ Context summarization failed: %v", err))
		}
		if context.IterationCount == context.MaxIterations {
			logger.LogProcessStep("⚠️ Maximum iterations reached, completing execution")
			break
		}

		// Persist resumable state each iteration
		if err := SaveAgentState(context); err != nil {
			logger.LogProcessStep("⚠️ Failed to save agent run state: " + err.Error())
		}
	}

	duration := time.Since(context.StartTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	logger.LogProcessStep(fmt.Sprintf("🎉 Adaptive agent execution completed in %d iterations", context.IterationCount))
	return nil
}

// isDirectCommandPattern detects if user intent looks like a direct command
func isDirectCommandPattern(lowerIntent string) bool {
	// Direct command patterns
	directPatterns := []string{
		"run ", "execute ", "start ", "stop ", "restart ", "kill ",
		"install ", "uninstall ", "update ", "upgrade ", "build ", "compile ",
		"test ", "check ", "verify ", "validate ",
		"list ", "show ", "display ", "print ",
		"create ", "make ", "generate ", "setup ",
		"delete ", "remove ", "clean ", "clear ",
		"find ", "search ", "grep ", "locate ",
		"copy ", "move ", "rename ", "backup ",
		"git ", "npm ", "yarn ", "pip ", "go ", "cargo ",
	}

	for _, pattern := range directPatterns {
		if strings.Contains(lowerIntent, pattern) {
			return true
		}
	}

	// Command-like patterns (starts with common commands)
	if strings.HasPrefix(lowerIntent, "git ") ||
		strings.HasPrefix(lowerIntent, "npm ") ||
		strings.HasPrefix(lowerIntent, "yarn ") ||
		strings.HasPrefix(lowerIntent, "pip ") ||
		strings.HasPrefix(lowerIntent, "go ") ||
		strings.HasPrefix(lowerIntent, "cargo ") ||
		strings.HasPrefix(lowerIntent, "docker ") ||
		strings.HasPrefix(lowerIntent, "kubectl ") {
		return true
	}

	return false
}

// extractDirectCommands extracts executable commands from user intent
func extractDirectCommands(userIntent string) []string {
	// Simple extraction - if it looks like a command, treat it as such
	lower := strings.ToLower(userIntent)

	// Common command mappings
	commandMap := map[string]string{
		"list files":        "ls -la",
		"show files":        "ls -la",
		"check status":      "systemctl status",
		"show processes":    "ps aux",
		"check disk usage":  "df -h",
		"show disk space":   "df -h",
		"check memory":      "free -h",
		"show memory usage": "free -h",
		"list processes":    "ps aux",
		"show running jobs": "jobs",
		"check git status":  "git status",
		"show git status":   "git status",
		"run tests":         "go test ./...", // or npm test, etc.
		"build project":     "make build",    // or go build, etc.
	}

	for phrase, command := range commandMap {
		if strings.Contains(lower, phrase) {
			return []string{command}
		}
	}

	// If it starts with a known command prefix, use it as-is
	if strings.HasPrefix(lower, "git ") ||
		strings.HasPrefix(lower, "npm ") ||
		strings.HasPrefix(lower, "yarn ") ||
		strings.HasPrefix(lower, "pip ") ||
		strings.HasPrefix(lower, "go ") ||
		strings.HasPrefix(lower, "cargo ") ||
		strings.HasPrefix(lower, "docker ") ||
		strings.HasPrefix(lower, "kubectl ") {
		return []string{userIntent}
	}

	return []string{}
}

// isSimpleFileOperation detects basic file operations
func isSimpleFileOperation(lowerIntent string) bool {
	fileOps := []string{
		"create file", "make file", "new file",
		"delete file", "remove file", "rm file",
		"copy file", "cp file",
		"move file", "mv file", "rename file",
		"show file", "display file", "cat file", "view file",
		"edit file", "modify file", "change file",
	}

	for _, op := range fileOps {
		if strings.Contains(lowerIntent, op) {
			return true
		}
	}

	return false
}

// extractFileOperationCommands converts file operation intent to commands
func extractFileOperationCommands(userIntent string) []string {
	lower := strings.ToLower(userIntent)

	// Simple file operation mappings
	if strings.Contains(lower, "show file") || strings.Contains(lower, "display file") || strings.Contains(lower, "view file") {
		if strings.Contains(userIntent, "README") {
			return []string{"cat README.md"}
		}
		if strings.Contains(userIntent, "package.json") {
			return []string{"cat package.json"}
		}
		if strings.Contains(userIntent, "go.mod") {
			return []string{"cat go.mod"}
		}
	}

	if strings.Contains(lower, "list files") || strings.Contains(lower, "show files") {
		return []string{"ls -la"}
	}

	if strings.Contains(lower, "check permissions") || strings.Contains(lower, "show permissions") {
		return []string{"ls -la"}
	}

	if strings.Contains(lower, "check size") || strings.Contains(lower, "show size") {
		return []string{"ls -lh"}
	}

	return []string{}
}
