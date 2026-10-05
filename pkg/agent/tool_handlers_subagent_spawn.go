package agent

import (
	"context"
	"fmt"
	"time"
)

func handleRunSubagent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Phase 1: Parse args, validate, resolve persona/model, build enhanced prompt
	spec, err := prepareSubagentLaunch(ctx, a, args)
	if err != nil {
		return "", err
	}
	background, isolate, err := resolveSubagentBackground(a, spec.persona, args)
	if err != nil {
		return "", err
	}
	if background {
		return startBackgroundSubagent(a, spec, isolate)
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
	var result *SubagentResult
	if isolate {
		result = runIsolatedSubagent(ctx, a, spec, fmt.Sprintf("subagent-%d", time.Now().UnixNano()), false)
	} else {
		result = a.GetSubagentRunner().Run(ctx, spec.enhancedPrompt, SubagentOptions{
			Persona:      spec.persona,
			Model:        spec.model,
			Provider:     spec.provider,
			SystemPrompt: spec.systemPromptText,
			WorkingDir:   spec.workingDir,
			Role:         spec.role,
		})
	}
	printSubagentDone(spec.persona, result)
	return finishSubagentRun(ctx, a, spec, result)
}

// finishSubagentRun turns a completed subagent run into the tool result the
// primary sees: merges its file changes, tracks cost, applies the security
// and failure handling, and marshals the result envelope. Shared by the
// blocking and background paths.
func finishSubagentRun(ctx context.Context, a *Agent, spec *subagentLaunchSpec, result *SubagentResult) (string, error) {
	// Merge the subagent's tracked changes into the primary's ChangeTracker
	// so list_changes, recover_file, and revert_my_changes see subagent edits.
	a.MergeSubagentChanges(result.FileChanges, spec.persona)

	// Build resultMap from SubagentResult. resultMap is preserved
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
	if budgetMsg := handleSubagentBudgetExceeded(a, resultMap, result); budgetMsg != "" {
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
