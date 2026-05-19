package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func handleRunParallelSubagents(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Accept "tasks", "prompts", or "subagents" parameter names for LLM flexibility
	var tasksRaw interface{}
	var ok bool

	if tasksRaw, ok = args["tasks"]; !ok {
		if tasksRaw, ok = args["prompts"]; !ok {
			tasksRaw, ok = args["subagents"]
		}
	}
	if !ok {
		return "", agenterrors.NewInvalidInputError("missing tasks, prompts, or subagents argument", nil)
	}

	// Parse the tasks array
	tasksSlice, ok := tasksRaw.([]interface{})
	if !ok {
		return "", agenterrors.NewInvalidInputError("tasks/prompts must be an array", nil)
	}

	a.debugLog("Spawning %d parallel subagents\n", len(tasksSlice))

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
				return "", fmt.Errorf("failed to convert prompt parameter: %w", err)
			}
			task.Prompt = prompt

			// Note: model and provider are set from configuration, not from LLM parameters
			// This ensures consistent subagent behavior configured by the user
		} else {
			return "", agenterrors.NewInvalidInputError("each task must be a string or an object", nil)
		}

		parallelTasks = append(parallelTasks, task)
	}

	// Get configured subagent model/provider and apply to all tasks
	var subagentProvider, subagentModel string
	if a.configManager != nil {
		config := a.configManager.GetConfig()
		subagentProvider = config.GetSubagentProvider()
		subagentModel = config.GetSubagentModel()
		a.warnSubagentFallback("parallel subagent defaults", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), subagentProvider, subagentModel)

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

	// Apply configuration to all tasks (overriding any empty values)
	for i := range parallelTasks {
		if subagentProvider != "" && parallelTasks[i].Provider == "" {
			parallelTasks[i].Provider = subagentProvider
		}
		if subagentModel != "" && parallelTasks[i].Model == "" {
			parallelTasks[i].Model = subagentModel
		}
	}

	// Check if parallel subagents are enabled
	if a.configManager != nil && !a.configManager.GetConfig().GetSubagentParallelEnabled() {
		return "", agenterrors.NewPermanentError("parallel subagents are disabled in configuration. Use run_subagent for sequential execution instead.", nil)
	}

	// Validate number of parallel tasks against configured max
	if a.configManager != nil {
		maxParallel := a.configManager.GetConfig().GetSubagentMaxParallel()
		if len(parallelTasks) > maxParallel {
			return "", agenterrors.NewInvalidInputError(fmt.Sprintf("too many parallel tasks: %d exceeds configured max of %d", len(parallelTasks), maxParallel), nil)
		}
	}

	a.debugLog("Spawning %d parallel subagents\n", len(parallelTasks))

	// Print the provider/model being used for these parallel subagents
	displayProvider := subagentProvider
	if displayProvider == "" {
		displayProvider = "default"
	}
	displayModel := subagentModel
	if displayModel == "" {
		displayModel = "default"
	}
	publishSubagentActivity(ctx, a, "spawn", fmt.Sprintf("Starting %d parallel subagents", len(parallelTasks)), map[string]interface{}{
		"provider":    displayProvider,
		"model":       displayModel,
		"is_parallel": true,
		"task_count":  len(parallelTasks),
	})
	_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[~] Spawning %d parallel subagents: provider=%s, model=%s\n", len(parallelTasks), displayProvider, displayModel)))

	runner := a.GetSubagentRunner()
	var tasks []SubagentTask
	for _, pt := range parallelTasks {
		tasks = append(tasks, SubagentTask{
			ID:       pt.ID,
			Prompt:   pt.Prompt,
			Model:    pt.Model,
			Provider: pt.Provider,
		})
	}
	results := runner.RunParallel(ctx, tasks, SubagentOptions{})

	// Convert to resultMap format for backward compatibility
	resultMap := make(map[string]map[string]string)
	for _, r := range results {
		resultMap[r.ID] = map[string]string{
			"stdout":          r.Output,
			"stderr":          "",
			"exit_code":       "0",
			"completed":       "true",
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
	failedCount := 0
	for _, result := range resultMap {
		if result["exit_code"] != "0" {
			failedCount++
		}
	}
	completionMessage := fmt.Sprintf("Parallel subagents completed (%d tasks)", len(resultMap))
	if failedCount > 0 {
		completionMessage = fmt.Sprintf("Parallel subagents finished with %d failure(s)", failedCount)
	}
	publishSubagentActivity(ctx, a, "complete", completionMessage, map[string]interface{}{
		"is_parallel": true,
		"task_count":  len(resultMap),
		"failures":    failedCount,
	})

	// Clean up any remaining batch buffers for all tasks
	for taskID := range resultMap {
		cleanupSubagentBatch(taskID, a, "", "")
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
					a.TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens, totalCost, cachedTokens)
					a.debugLog("Tracked parallel subagent [%s] costs: %d tokens, $%.6f\n", taskID, totalTokens, totalCost)
				}
			}
		}
	}

	// Check for security errors in any of the parallel subagents
	// When running as a subagent, we need to delegate security decisions to the primary agent
	if a.IsSubagent() {
		for taskID, result := range resultMap {
			exitCode := result["exit_code"]
			stderr := result["stderr"]

			// Check for filesystem security errors or failures
			if strings.Contains(stderr, "outside working directory") ||
				strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
				strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
				strings.Contains(stderr, "security warning") ||
				exitCode != "0" {

				// One of the parallel subagents encountered a security error or failed
				// Return a special error format that tells the primary agent to stop retrying
				errorMsg := fmt.Sprintf("SUBAGENT_SECURITY_ERROR: A parallel subagent encountered a security-related error or requires user authorization.\n\n"+
					"Task ID: %s\n"+
					"Exit code: %s\n"+
					"Stderr: %s\n"+
					"Stdout: %s\n\n"+
					"IMPORTANT: This subagent task requires user authorization or encountered a blocking error. "+
					"Do NOT retry this parallel subagent call with the same parameters. "+
					"Instead, inform the user about the error and ask for guidance on how to proceed.",
					taskID, exitCode, stderr, result["stdout"])

				a.debugLog("Parallel subagent [%s] failed with security error, delegating to primary agent\n", taskID)
				return errorMsg, nil
			}
		}
	}

	// Flush any remaining buffered output for parallel subagents
	flushAllSubagentBuffers(a)

	// For non-subagent context (primary agent), check if any subagent failed
	// and add a clear message to prevent retry loops
	var failedTasks []string
	var securityErrors []string

	for taskID, result := range resultMap {
		exitCode := result["exit_code"]
		stderr := result["stderr"]
		stdout := result["stdout"]

		if exitCode != "0" {
			// Check for specific error patterns that indicate we should stop retrying
			if strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
				strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
				strings.Contains(stderr, "security") ||
				strings.Contains(stdout, "SUBAGENT_SECURITY_ERROR") {

				// This is a security/authorization error - don't retry
				securityErrors = append(securityErrors, fmt.Sprintf(
					"Task %s: exit code %s, error: %s", taskID, exitCode, stderr))
			} else {
				// Other failures - track but allow potential retry
				failedTasks = append(failedTasks, fmt.Sprintf(
					"Task %s: exit code %s", taskID, exitCode))
				result["error"] = fmt.Sprintf("Subagent failed with exit code %s. Error output: %s", exitCode, stderr)
			}
		}
	}

	// If we have security errors, return a clear error message to prevent retry loops
	if len(securityErrors) > 0 {
		errorMsg := fmt.Sprintf("SUBAGENT_FAILED: One or more parallel subagents encountered security or authorization errors that prevent them from completing their tasks.\n\n"+
			"%s\n\n"+
			"These errors require user intervention. Do NOT retry the parallel subagent call. "+
			"Instead, report the errors to the user and ask for guidance.",
			strings.Join(securityErrors, "\n"))

		a.debugLog("Parallel subagents failed with security errors, stopping retry loop\n")
		return errorMsg, nil
	}

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal parallel subagents result: %w", jsonErr)
	}

	a.debugLog("Parallel subagents spawn result: %s\n", string(jsonBytes))

	return string(jsonBytes), nil
}
