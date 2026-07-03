// Subagent spawn and dispatch logic.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// handleRunSubagent — single subagent spawn/dispatch
// ---------------------------------------------------------------------------

func handleRunSubagent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Phase 1: Parse args, validate, resolve persona/model, build enhanced prompt
	spec, err := prepareSubagentLaunch(ctx, a, args)
	if err != nil {
		return "", err
	}

	// Print the provider/model being used for this subagent
	displayProvider := spec.provider
	if displayProvider == "" {
		displayProvider = "default"
	}
	displayModel := spec.model
	if displayModel == "" {
		displayModel = "default"
	}
	publishSubagentActivity(ctx, a, "spawn", fmt.Sprintf("Starting %s", spec.persona), map[string]interface{}{
		"persona":     spec.persona,
		"provider":    displayProvider,
		"model":       displayModel,
		"is_parallel": false,
	})
	printSubagentStart(spec.persona, displayProvider, displayModel)

	// Phase 2: Run the subagent
	runner := a.GetSubagentRunner()
	result := runner.Run(ctx, spec.enhancedPrompt, SubagentOptions{
		Persona:      spec.persona,
		Model:        spec.model,
		Provider:     spec.provider,
		SystemPrompt: spec.systemPromptText,
		WorkingDir:   spec.workingDir,
	})
	printSubagentDone(spec.persona, result)

	// SP-059 Phase 2c (missing step): merge the subagent's tracked
	// changes into the primary's ChangeTracker so list_changes,
	// recover_file, and revert_my_changes see subagent edits.
	a.MergeSubagentChanges(result.FileChanges, spec.persona)

	// Phase 3: Build resultMap from SubagentResult
	// SP-059 Phase 2a: build the typed envelope. resultMap is preserved
	// for the legacy code paths below that still mutate it via string
	// keys (file change extraction, security re-prompt, etc.) — both
	// views are kept in sync at the marshal site.
	resultMap := map[string]string{
		"stdout":          result.Output,
		"stderr":          "",
		"exit_code":       "0",
		"completed":       "true",
		"output_complete": fmt.Sprintf("%t", result.OutputComplete),
		"timed_out":       "false",
		"budget_exceeded": fmt.Sprintf("%t", result.BudgetExceeded),
		"elapsed_seconds": fmt.Sprintf("%.1f", result.Elapsed.Seconds()),
		"tokens_used":     fmt.Sprintf("%d", result.TokensUsed),
		"cost":            fmt.Sprintf("%.6f", result.Cost),
		"tool_calls":      fmt.Sprintf("%d", result.ToolCalls),
	}
	if result.Error != nil {
		resultMap["exit_code"] = "1"
		resultMap["stderr"] = result.Error.Error()
		a.Logger().Debug("Subagent error: %v\n", result.Error)
	}

	// Phase 4: Post-run processing
	// Truncate output if it exceeds size limit
	truncateSubagentOutput(resultMap)

	// Extract summary and track costs
	extractAndTrackSubagentSummary(a, resultMap, result)

	// Add context_used field
	if spec.context != "" {
		resultMap["context_used"] = "true"
	} else {
		resultMap["context_used"] = "false"
	}

	// Add files_used field
	if spec.filesStr != "" {
		resultMap["files_used"] = spec.filesStr
	} else {
		resultMap["files_used"] = ""
	}

	// Add working_dir field
	if spec.workingDir != "" {
		resultMap["working_dir"] = spec.workingDir
	} else {
		resultMap["working_dir"] = ""
	}

	// Check if subagent failed with security-related errors
	// When running as a subagent, we can't prompt the user
	// So we need to delegate the security decision back to the primary agent
	if securityMsg := handleSubagentSecurityError(a, resultMap); securityMsg != "" {
		return securityMsg, nil
	}

	// Publish completion event
	exitCode := "0"
	if ec, ok := resultMap["exit_code"]; ok {
		exitCode = ec
	}
	completionMessage := "Subagent completed"
	if exitCode != "0" {
		completionMessage = fmt.Sprintf("Subagent failed (exit code %s)", exitCode)
	}
	publishSubagentActivity(ctx, a, "complete", completionMessage, map[string]interface{}{
		"persona":     spec.persona,
		"exit_code":   exitCode,
		"is_parallel": false,
	})

	// Flush any remaining buffered output before completing
	flushAllSubagentBuffers(a)

	// Check if subagent exceeded token budget
	if budgetMsg := handleSubagentBudgetExceeded(a, resultMap); budgetMsg != "" {
		return budgetMsg, nil
	}

	// For non-subagent context (primary agent), check if the subagent failed
	// and add a clear message to prevent retry loops
	if failMsg := handleSubagentNonSecurityFailure(a, resultMap); failMsg != "" {
		return failMsg, nil
	}

	// Marshal and return the final result
	return buildSubagentFinalResult(a, resultMap, result)
}

// ---------------------------------------------------------------------------
// handleRunParallelSubagents — parallel dispatch
// ---------------------------------------------------------------------------

func handleRunParallelSubagents(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Phase 1: Parse tasks from arguments
	parallelTasks, err := parseParallelTasks(args)
	if err != nil {
		return "", err
	}

	a.Logger().Debug("Spawning %d parallel subagents\n", len(parallelTasks))

	// Resolve subagent provider/model configuration
	subagentProvider, subagentModel := resolveParallelSubagentConfig(a)
	applyParallelTaskConfig(parallelTasks, subagentProvider, subagentModel)

	// Validate parallel subagent configuration
	if err := validateParallelSubagentConfig(a, parallelTasks); err != nil {
		return "", err
	}

	a.Logger().Debug("Spawning %d parallel subagents\n", len(parallelTasks))

	// Print subagent info and notify event bus
	publishParallelSubagentStart(ctx, a, subagentProvider, subagentModel, len(parallelTasks))

	// Build task list and run
	runner := a.GetSubagentRunner()
	tasks := buildParallelSubagentTasks(parallelTasks)
	opts := SubagentOptions{}
	if a.configManager != nil {
		maxParallel := a.configManager.GetConfig().GetSubagentMaxParallel()
		if maxParallel > 16 {
			maxParallel = 16
		}
		opts.MaxConcurrentSubagents = maxParallel
	}
	results := runner.RunParallel(ctx, tasks, opts)

	// Phase 2: Collect and process results
	resultMap, failedCount := collectParallelResults(results, tasks, a)
	publishParallelSubagentComplete(ctx, a, resultMap, failedCount)

	// Clean up any remaining batch buffers for all tasks
	for taskID := range resultMap {
		cleanupSubagentBatch(taskID, a, "", "")
	}

	// Phase 3: Security checks and failure handling
	if handled, result := handleParallelSubagentSecurityResult(resultMap, a); handled {
		return result, nil
	}

	// Flush any remaining buffered output for parallel subagents
	flushAllSubagentBuffers(a)

	// For non-subagent context, check if any subagent failed
	if handled, result := handleParallelSubagentFailureResult(resultMap, a); handled {
		return result, nil
	}

	// Marshal and return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", agenterrors.NewAgent("subagent.spawn", "failed to marshal parallel subagents result", jsonErr)
	}

	a.Logger().Debug("Parallel subagents spawn result: %s\n", string(jsonBytes))
	return string(jsonBytes), nil
}

// ---------------------------------------------------------------------------
// parseParallelTasks — parse the tasks/prompts/subagents argument from args
// ---------------------------------------------------------------------------

// parseParallelTasks extracts and parses the tasks array from the argument
// map, supporting both string and object task formats. Returns the parsed
// tasks or an error if the input is invalid.
func parseParallelTasks(args map[string]interface{}) ([]SubagentTask, error) {
	// Accept "tasks", "prompts", or "subagents" parameter names for LLM flexibility
	var tasksRaw interface{}
	var ok bool

	if tasksRaw, ok = args["tasks"]; !ok {
		if tasksRaw, ok = args["prompts"]; !ok {
			tasksRaw, ok = args["subagents"]
		}
	}
	if !ok {
		return nil, agenterrors.NewInvalidInputError("missing tasks, prompts, or subagents argument", nil)
	}

	// Parse the tasks array
	tasksSlice, ok := tasksRaw.([]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("tasks/prompts must be an array", nil)
	}

	var parallelTasks []SubagentTask
	for i, taskRaw := range tasksSlice {
		task := SubagentTask{}

		// Support two formats:
		// 1. Simple string: "task description"
		// 2. Object: {"id": "task-id", "prompt": "task description", ...}
		if taskStr, ok := taskRaw.(string); ok {
			// Simple string format - auto-generate ID
			task.ID = fmt.Sprintf("task-%d", i+1)
			task.Prompt = taskStr
		} else if taskMap, ok := taskRaw.(map[string]interface{}); ok {
			// Object format
			if id, ok := taskMap["id"].(string); ok {
				task.ID = id
			} else {
				// Auto-generate ID if not provided
				task.ID = fmt.Sprintf("task-%d", i+1)
			}

			prompt, err := convertToString(taskMap["prompt"], "prompt")
			if err != nil {
				return nil, agenterrors.NewValidation(fmt.Sprintf("failed to convert prompt parameter: %v", err), nil)
			}
			task.Prompt = prompt

			// Note: model and provider are set from configuration, not from LLM parameters
			// This ensures consistent subagent behavior configured by the user
		} else {
			return nil, agenterrors.NewInvalidInputError("each task must be a string or an object", nil)
		}

		parallelTasks = append(parallelTasks, task)
	}

	return parallelTasks, nil
}

// ---------------------------------------------------------------------------
// collectParallelResults — merge changes, build resultMap, and track costs
// ---------------------------------------------------------------------------

// collectParallelResults merges subagent file changes into the parent agent's
// ChangeTracker, converts SubagentResult slices into the legacy resultMap
// format, counts failures, and tracks LLM usage costs. Returns the resultMap
// and the number of failed tasks.
func collectParallelResults(results []*SubagentResult, tasks []SubagentTask, a *Agent) (map[string]map[string]string, int) {
	// SP-059 Phase 2c (missing step): merge each subagent's tracked
	// changes into the primary's ChangeTracker so list_changes,
	// recover_file, and revert_my_changes see subagent edits. Build a
	// taskID -> persona lookup so merged changes are attributed to the
	// correct subagent persona.
	personaByID := make(map[string]string, len(tasks))
	for _, t := range tasks {
		personaByID[t.ID] = t.Persona
	}
	for _, r := range results {
		a.MergeSubagentChanges(r.FileChanges, personaByID[r.ID])
	}

	// Convert to resultMap format for backward compatibility
	resultMap := make(map[string]map[string]string)
	for _, r := range results {
		resultMap[r.ID] = map[string]string{
			"stdout":          r.Output,
			"stderr":          "",
			"exit_code":       "0",
			"completed":       "true",
			"output_complete": fmt.Sprintf("%t", r.OutputComplete),
			"timed_out":       "false",
			"budget_exceeded": fmt.Sprintf("%t", r.BudgetExceeded),
			"elapsed_seconds": fmt.Sprintf("%.1f", r.Elapsed.Seconds()),
			"tokens_used":     fmt.Sprintf("%d", r.TokensUsed),
			"cost":            fmt.Sprintf("%.6f", r.Cost),
			"tool_calls":      fmt.Sprintf("%d", r.ToolCalls),
		}
		if r.Error != nil {
			resultMap[r.ID]["exit_code"] = "1"
			resultMap[r.ID]["stderr"] = r.Error.Error()
		}
	}

	// Count failures
	failedCount := 0
	for _, result := range resultMap {
		if result["exit_code"] != "0" {
			failedCount++
		}
	}

	// Track costs from all parallel subagents
	for taskID, result := range resultMap {
		if stdout, ok := result["stdout"]; ok {
			summary := extractSubagentSummary(stdout)

			// Track subagent costs in parent agent's totals
			if totalTokensStr, ok := summary["subagent_total_tokens"]; ok {
				if totalCostStr, ok := summary["subagent_total_cost"]; ok {
					promptTokensStr := summary["subagent_prompt_tokens"]
					completionTokensStr := summary["subagent_completion_tokens"]
					cachedTokensStr := summary["subagent_cached_tokens"]

					// Parse the values
					var totalTokens, promptTokens, completionTokens, cachedTokens int
					var totalCost float64
					fmt.Sscanf(totalTokensStr, "%d", &totalTokens)
					fmt.Sscanf(promptTokensStr, "%d", &promptTokens)
					fmt.Sscanf(completionTokensStr, "%d", &completionTokens)
					fmt.Sscanf(cachedTokensStr, "%d", &cachedTokens)
					fmt.Sscanf(totalCostStr, "%f", &totalCost)

					// Add to parent agent's totals using TrackMetricsFromResponse
					a.TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens, totalCost, cachedTokens, 0)
					a.Logger().Debug("Tracked parallel subagent [%s] costs: %d tokens, $%.6f\n", taskID, totalTokens, totalCost)
				}
			}
		}
	}

	return resultMap, failedCount
}

// ---------------------------------------------------------------------------
// resolveParallelSubagentConfig — resolve provider/model for parallel tasks
// ---------------------------------------------------------------------------

// resolveParallelSubagentConfig resolves the effective provider and model for
// parallel subagent tasks, checking config, fallback warnings, and parent
// agent inheritance.
func resolveParallelSubagentConfig(a *Agent) (string, string) {
	var subagentProvider, subagentModel string
	if a.configManager != nil {
		config := a.configManager.GetConfig()
		subagentProvider = config.GetSubagentProvider()
		subagentModel = config.GetSubagentModel()
		a.warnSubagentFallback("parallel subagent defaults", "", "", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), subagentProvider, subagentModel)

		// If no explicit subagent config, inherit from parent agent's runtime values.
		if config.SubagentProvider == "" && config.SubagentModel == "" {
			if parentProvider := a.GetProvider(); parentProvider != "" && parentProvider != "unknown" {
				subagentProvider = parentProvider
			}
			if parentModel := a.GetModel(); parentModel != "" && parentModel != "unknown" {
				subagentModel = parentModel
			}
		}
	} else {
		subagentProvider = a.GetProvider()
		subagentModel = a.GetModel()
	}
	return subagentProvider, subagentModel
}

// ---------------------------------------------------------------------------
// applyParallelTaskConfig — apply provider/model to all tasks
// ---------------------------------------------------------------------------

// applyParallelTaskConfig applies the resolved provider/model configuration
// to all parallel tasks, overriding any empty values.
func applyParallelTaskConfig(tasks []SubagentTask, provider, model string) {
	for i := range tasks {
		if provider != "" && tasks[i].Provider == "" {
			tasks[i].Provider = provider
		}
		if model != "" && tasks[i].Model == "" {
			tasks[i].Model = model
		}
	}
}

// ---------------------------------------------------------------------------
// validateParallelSubagentConfig — validate parallel subagent configuration
// ---------------------------------------------------------------------------

// validateParallelSubagentConfig checks whether parallel subagents are
// enabled and whether the number of tasks is within the configured limit.
func validateParallelSubagentConfig(a *Agent, tasks []SubagentTask) error {
	if a.configManager != nil && !a.configManager.GetConfig().GetSubagentParallelEnabled() {
		return agenterrors.NewPermanentError("parallel subagents are disabled in configuration. Use run_subagent for sequential execution instead.", nil)
	}

	if a.configManager != nil {
		maxParallel := a.configManager.GetConfig().GetSubagentMaxParallel()
		if len(tasks) > maxParallel {
			return agenterrors.NewInvalidInputError(fmt.Sprintf("too many parallel tasks: %d exceeds configured max of %d", len(tasks), maxParallel), nil)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// publishParallelSubagentStart — print and publish start event
// ---------------------------------------------------------------------------

// publishParallelSubagentStart publishes a spawn event and prints the
// parallel subagent start message with provider/model info.
func publishParallelSubagentStart(ctx context.Context, a *Agent, provider, model string, count int) {
	displayProvider := provider
	if displayProvider == "" {
		displayProvider = "default"
	}
	displayModel := model
	if displayModel == "" {
		displayModel = "default"
	}
	publishSubagentActivity(ctx, a, "spawn", fmt.Sprintf("Starting %d parallel subagents", count), map[string]interface{}{
		"provider":    displayProvider,
		"model":       displayModel,
		"is_parallel": true,
		"task_count":  count,
	})
	printParallelSubagentStart(count, displayProvider, displayModel)
}

// ---------------------------------------------------------------------------
// buildParallelSubagentTasks — build trimmed task list for runner
// ---------------------------------------------------------------------------

// buildParallelSubagentTasks creates a copy of the task list with only the
// fields that the subagent runner needs (ID, Prompt, Model, Provider).
// Persona and WorkingDir are intentionally excluded because the parallel
// runner resolves them from SubagentOptions, not from individual task
// struct fields. This matches the original pre-refactor behavior exactly.
func buildParallelSubagentTasks(tasks []SubagentTask) []SubagentTask {
	result := make([]SubagentTask, len(tasks))
	for i, pt := range tasks {
		result[i] = SubagentTask{
			ID:       pt.ID,
			Prompt:   pt.Prompt,
			Model:    pt.Model,
			Provider: pt.Provider,
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// publishParallelSubagentComplete — publish completion event
// ---------------------------------------------------------------------------

// publishParallelSubagentComplete publishes a completion event for parallel
// subagents with task count and failure information.
func publishParallelSubagentComplete(ctx context.Context, a *Agent, resultMap map[string]map[string]string, failedCount int) {
	completionMessage := fmt.Sprintf("Parallel subagents completed (%d tasks)", len(resultMap))
	if failedCount > 0 {
		completionMessage = fmt.Sprintf("Parallel subagents finished with %d failure(s)", failedCount)
	}
	publishSubagentActivity(ctx, a, "complete", completionMessage, map[string]interface{}{
		"is_parallel": true,
		"task_count":  len(resultMap),
		"failures":    failedCount,
	})
}

// ---------------------------------------------------------------------------
// handleParallelSubagentSecurityResult — security error delegation
// ---------------------------------------------------------------------------

// handleParallelSubagentSecurityResult checks if any parallel subagent
// encountered a security-related error when running as a subagent. Returns
// true and an error message if a security error was found, and false if
// execution should continue normally.
func handleParallelSubagentSecurityResult(resultMap map[string]map[string]string, a *Agent) (bool, string) {
	if !a.IsSubagent() {
		return false, ""
	}

	for taskID, result := range resultMap {
		exitCode := result["exit_code"]
		stderr := result["stderr"]

		if strings.Contains(stderr, "outside working directory") ||
			strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
			strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
			strings.Contains(stderr, "security warning") ||
			exitCode != "0" {

			errorMsg := fmt.Sprintf("SUBAGENT_SECURITY_ERROR: A parallel subagent encountered a security-related error or requires user authorization.\n\n"+
				"Task ID: %s\n"+
				"Exit code: %s\n"+
				"Stderr: %s\n"+
				"Stdout: %s\n\n"+
				"IMPORTANT: This subagent task requires user authorization or encountered a blocking error. "+
				"Do NOT retry this parallel subagent call with the same parameters. "+
				"Instead, inform the user about the error and ask for guidance on how to proceed.",
				taskID, exitCode, stderr, result["stdout"])

			a.Logger().Debug("Parallel subagent [%s] failed with security error, delegating to primary agent\n", taskID)
			return true, errorMsg
		}
	}
	return false, ""
}

// ---------------------------------------------------------------------------
// handleParallelSubagentFailureResult — non-subagent failure detection
// ---------------------------------------------------------------------------

// handleParallelSubagentFailureResult checks for failed or security-blocked
// parallel subagents when running as the primary agent (not a subagent).
// Returns true and an error message if failures should prevent retry loops.
func handleParallelSubagentFailureResult(resultMap map[string]map[string]string, a *Agent) (bool, string) {
	var securityErrors []string

	for taskID, result := range resultMap {
		exitCode := result["exit_code"]
		stderr := result["stderr"]
		stdout := result["stdout"]

		if exitCode != "0" {
			if strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
				strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
				strings.Contains(stderr, "security") ||
				strings.Contains(stdout, "SUBAGENT_SECURITY_ERROR") {
				securityErrors = append(securityErrors, fmt.Sprintf(
					"Task %s: exit code %s, error: %s", taskID, exitCode, stderr))
			} else {
				result["error"] = fmt.Sprintf("Subagent failed with exit code %s. Error output: %s", exitCode, stderr)
			}
		}
	}

	if len(securityErrors) > 0 {
		errorMsg := fmt.Sprintf("SUBAGENT_FAILED: One or more parallel subagents encountered security or authorization errors that prevent them from completing their tasks.\n\n"+
			"%s\n\n"+
			"These errors require user intervention. Do NOT retry the parallel subagent call. "+
			"Instead, report the errors to the user and ask for guidance.",
			strings.Join(securityErrors, "\n"))

		a.Logger().Debug("Parallel subagents failed with security errors, stopping retry loop\n")
		return true, errorMsg
	}

	return false, ""
}
