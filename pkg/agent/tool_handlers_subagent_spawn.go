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
			Persona:       spec.persona,
			Model:         spec.model,
			Provider:      spec.provider,
			SystemPrompt:  spec.systemPromptText,
			WorkingDir:    spec.workingDir,
			Role:          spec.role,
			MaxIterations: spec.maxIterations,
			Timeout:       spec.timeout,
		})
	}

	// Verify a worker-role result before the primary relies on it; on
	// failure, retry once on the coder model (the escalation). The
	// verification outcome rides on the envelope so it is never silent.
	// Non-worker subagents are not subject to worker verification, so they
	// carry the zero outcome (no verified/escalated fields).
	var verification workerVerifyResult
	// workerBefore captures the worker's first result before the escalation
	// (nil unless the task is a worker task), so the outcome record can tell
	// whether the coder model or the worker model produced the final result.
	var workerBefore *SubagentResult
	if isWorkerRoleTask(spec.role) {
		workerBefore = result
		verification = verifyWorkerResult(a, spec, result)
		if !verification.Verified {
			result, verification = escalateWorkerRun(ctx, a, spec, result, verification)
		}
	}

	printSubagentDone(spec.persona, result)
	return finishSubagentRun(ctx, a, spec, result, workerBefore, verification)
}

// escalateWorkerRun retries a worker task that failed verification ONCE on
// the coder model and re-verifies the escalated result. It records the
// escalation on the returned verification so the caller can surface it. The
// retry happens at most once — a second failure is reported, not retried
// again.
func escalateWorkerRun(ctx context.Context, a *Agent, spec *subagentLaunchSpec, failed *SubagentResult, first workerVerifyResult) (*SubagentResult, workerVerifyResult) {
	a.Logger().Info("worker task failed verification (%s); escalating to the coder model\n", first.Reason)
	publishSubagentActivity(ctx, a, "escalate", fmt.Sprintf("Worker result failed verification: %s", first.Reason), map[string]interface{}{
		"persona":     spec.persona,
		"is_parallel": false,
		"reason":      first.Reason,
	})

	escalated, err := escalateWorkerTask(ctx, a, spec)
	if err != nil || escalated == nil {
		// The escalation could not run: surface the original failure rather
		// than a success-looking envelope.
		reason := first.Reason
		if err != nil {
			reason = first.Reason + "; escalation failed: " + err.Error()
		}
		return failed, workerVerifyResult{Verified: false, Reason: reason, Escalated: true}
	}

	second := verifyWorkerResult(a, spec, escalated)
	second.Escalated = true
	if !second.Verified {
		second.Reason = "escalated run also failed verification: " + second.Reason
	}
	return escalated, second
}

// finishSubagentRun turns a completed subagent run into the tool result the
// primary sees: merges its file changes, tracks cost, applies the security
// and failure handling, and marshals the result envelope. Shared by the
// blocking and background paths. workerBefore is the worker-role result
// BEFORE the single escalation (nil when no escalation ran or the task is
// not a worker task); it lets the outcome record attribute the final model
// correctly (the coder model when the escalation produced the result).
func finishSubagentRun(ctx context.Context, a *Agent, spec *subagentLaunchSpec, result *SubagentResult, workerBefore *SubagentResult, verification workerVerifyResult) (string, error) {
	// Record this delegated task's outcome (the model that produced the
	// final result, its type, the verification result, and its tokens/cost)
	// in the session stats and as an event a host can aggregate. Exactly one
	// record per delegated task: the blocking and background paths both flow
	// through here, and the parallel path records on its own.
	a.recordDelegatedOutcome(spec, result, workerBefore, verification)

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

	// A worker-role task whose result failed verification (even after the
	// single escalation) must never be handed to the primary as an
	// unqualified success: mark it failed and carry the reason.
	if isWorkerRoleTask(spec.role) && !verification.Verified {
		resultMap["exit_code"] = "1"
		resultMap["stderr"] = "worker verification failed: " + verification.Reason
		resultMap["error"] = "worker verification failed: " + verification.Reason
	}

	// Marshal and return the final result
	return buildSubagentFinalResult(a, resultMap, result, verification)
}

// ---------------------------------------------------------------------------
// handleRunParallelSubagents — parallel dispatch
// ---------------------------------------------------------------------------
