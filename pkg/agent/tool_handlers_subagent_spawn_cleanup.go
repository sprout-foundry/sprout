// Subagent spawn cleanup helpers: provider/model resolution and
// post-run result processing (truncation, summary, security errors,
// budget exceeded, final marshaling).
//
// Extracted from tool_handlers_subagent_spawn.go as part of SP-075's
// large-file decomposition.

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// resolveSubagentProviderModel resolves the provider, model, and system
// prompt text for the given persona. Applies persona-specific config,
// global subagent config, and parent fallback in that priority order.
// Loads the system prompt from file if needed.
func resolveSubagentProviderModel(a *Agent, persona string, personaExplicitlyProvided bool, subagentWorkspaceRoot string) (provider, model, systemPromptText string, _ error) {
	var systemPromptPath string
	explicitSubagentConfig := false

	if a.configManager != nil {
		config := a.configManager.GetConfig()

		if persona != "" {
			// Get persona-specific configuration
			subagentType := config.GetSubagentType(persona)
			if subagentType != nil {
				// Check LocalOnly flag - reject in cloud mode
				if subagentType.LocalOnly && !a.IsLocalMode() {
					return "", "", "", agenterrors.NewValidation(fmt.Sprintf("persona '%s' is local-only and cannot be used as a subagent in cloud mode", persona), nil)
				}
				// Spawnability check: a Delegatable=false target may only be
				// spawned when the active persona explicitly lists it in
				// CanSpawnNonDelegatable. This replaces the previous
				// hardcoded "EA can spawn anything" carve-out — the coordinator
				// declares ["orchestrator"] so the canonical
				// coordinator→orchestrator→specialist chain still works, and
				// no additional Go special-cases (EA-can't-spawn-EA,
				// orchestrator-can't-spawn-coordinator) are needed: the
				// missing entries express the policy directly.
				if !subagentType.Delegatable && !a.canSpawnNonDelegatable(persona) {
					return "", "", "", agenterrors.NewValidation(fmt.Sprintf("persona '%s' is not spawnable from %q (delegatable=false and not listed in spawner's can_spawn_non_delegatable)", persona, a.GetActivePersona()), nil)
				}
				// No persona can spawn itself — orthogonal to spawn_policy.
				currentPersona := a.GetActivePersona()
				if currentPersona != "" && currentPersona == persona {
					return "", "", "", agenterrors.NewValidation(fmt.Sprintf("persona '%s' cannot spawn itself (prevents self-recursion)", persona), nil)
				}
				provider = config.GetSubagentTypeProvider(persona)
				model = config.GetSubagentTypeModel(persona)
				systemPromptPath = subagentType.SystemPrompt
				// Inline text takes precedence over file path
				if subagentType.SystemPromptText != "" {
					systemPromptText = subagentType.SystemPromptText
				}
				// Track if persona had explicit provider/model (not from global fallback)
				if subagentType.Provider != "" || subagentType.Model != "" {
					explicitSubagentConfig = true
				}
				a.Logger().Debug("Using persona '%s': provider=%s model=%s system_prompt=%s\n",
					persona, provider, model, systemPromptPath)
				a.warnSubagentFallback(fmt.Sprintf("persona '%s'", persona), strings.TrimSpace(subagentType.Provider), strings.TrimSpace(subagentType.Model), strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
			} else {
				a.Logger().Debug("Warning: Persona '%s' not found or disabled, using default subagent config\n", persona)
				provider = config.GetSubagentProvider()
				model = config.GetSubagentModel()
				a.warnSubagentFallback("default subagent config", "", "", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
			}
		} else {
			// No persona specified, use default subagent config
			provider = config.GetSubagentProvider()
			model = config.GetSubagentModel()
			a.Logger().Debug("Using subagent provider=%s model=%s from config\n", provider, model)
			a.warnSubagentFallback("default subagent config", "", "", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
		}

		// If no explicit subagent config is set (SubagentProvider and SubagentModel are empty
		// and persona doesn't have explicit provider/model), inherit from parent agent.
		// This ensures subagents use the model the user actually selected for the main agent.
		if !explicitSubagentConfig && config.SubagentProvider == "" && config.SubagentModel == "" {
			parentProvider := a.GetProvider()
			parentModel := a.GetModel()
			if parentProvider != "" && parentProvider != "unknown" {
				provider = parentProvider
			}
			if parentModel != "" && parentModel != "unknown" {
				model = parentModel
			}
			a.Logger().Debug("Inheriting parent agent provider/model: provider=%s model=%s\n", provider, model)
		}

		// Log no-persona spawn resolution for observability. persona is defaulted
		// to "general" earlier in this function (or to cfg.DefaultSubagentPersona),
		// so we check the explicit-provided flag rather than the empty string —
		// without this, the log line would never fire.
		if !personaExplicitlyProvided {
			source := "global subagent default"
			if config.SubagentProvider == "" && config.SubagentModel == "" {
				source = "parent fallback"
			}
			a.Logger().Info("no-persona subagent spawn: provider=%s model=%s source=%s (resolved persona=%s)\n", provider, model, source, persona)
		}
	} else {
		a.Logger().Debug("Warning: No config manager available, using parent agent defaults\n")
		provider = a.GetProvider()
		model = a.GetModel()
		a.warnSubagentFallback("missing config manager", "", "", "", "", provider, model)
	}

	// Resolve system prompt: inline text takes precedence over file path.
	// If systemPromptPath is set but systemPromptText is empty, load from file.
	// Resolve relative to workspace root (not process cwd) for daemon mode safety.
	if systemPromptText == "" && systemPromptPath != "" {
		absPromptPath := systemPromptPath
		if !filepath.IsAbs(absPromptPath) {
			absPromptPath = filepath.Join(subagentWorkspaceRoot, systemPromptPath)
		}
		promptBytes, err := os.ReadFile(absPromptPath)
		if err == nil {
			systemPromptText = string(promptBytes)
			a.Logger().Debug("Loaded system prompt from %s\n", absPromptPath)
		} else {
			a.Logger().Debug("Failed to load system prompt from %s: %v\n", absPromptPath, err)
		}
	}

	return provider, model, systemPromptText, nil
}

// ---------------------------------------------------------------------------
// Post-run result processing helpers
// ---------------------------------------------------------------------------

// truncateSubagentOutput truncates stdout/stderr in resultMap if they
// exceed MAX_SUBAGENT_OUTPUT_SIZE.
func truncateSubagentOutput(resultMap map[string]string) {
	if stdout, ok := resultMap["stdout"]; ok {
		if len(stdout) > MAX_SUBAGENT_OUTPUT_SIZE {
			resultMap["stdout"] = stdout[:MAX_SUBAGENT_OUTPUT_SIZE] + "... (truncated, too large)"
		}
	}
	if stderr, ok := resultMap["stderr"]; ok {
		if len(stderr) > MAX_SUBAGENT_OUTPUT_SIZE {
			resultMap["stderr"] = stderr[:MAX_SUBAGENT_OUTPUT_SIZE] + "... (truncated, too large)"
		}
	}
}

// extractAndTrackSubagentSummary extracts a human-readable summary from
// stdout and rolls the subagent's token/cost into the parent agent's totals.
func extractAndTrackSubagentSummary(a *Agent, resultMap map[string]string, result *SubagentResult) {
	// Extract summary from stdout (human-readable file changes, build/test
	// status, etc.). SP-059 Phase 2b: token/cost tracking switched to the
	// structured SubagentResult fields below, no longer regex-scraped from
	// SUBAGENT_METRICS: lines (which silently regressed if a model dropped
	// the line).
	if stdout, ok := resultMap["stdout"]; ok {
		summary := extractSubagentSummary(stdout)
		summaryJSON, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			a.Logger().Debug("Failed to marshal summary: %v\n", err)
			resultMap["summary"] = fmt.Sprintf("Error creating summary: %v", err)
		} else {
			resultMap["summary"] = string(summaryJSON)
			a.Logger().Debug("Extracted subagent summary: %s\n", string(summaryJSON))
		}
	}

	// Roll the subagent's token/cost into the parent agent's totals from
	// the structured SubagentResult — no stdout scraping. Prompt /
	// completion / cached splits are not exposed by SubagentResult today,
	// so they're left at zero; TrackMetricsFromResponse treats them as
	// "unknown split" and still applies the totals correctly.
	if result.TokensUsed > 0 || result.Cost > 0 {
		a.TrackMetricsFromResponse(0, 0, int(result.TokensUsed), result.Cost, 0, 0)
		a.Logger().Debug("Tracked subagent costs: %d tokens, $%.6f\n", result.TokensUsed, result.Cost)
	}
}

// handleSubagentSecurityError checks whether the subagent failed with a
// security-related error and returns a formatted error string when it did.
// Returns "" when no security error is detected.
func handleSubagentSecurityError(a *Agent, resultMap map[string]string) string {
	if !a.IsSubagent() {
		return ""
	}

	stderr := resultMap["stderr"]
	exitCode := resultMap["exit_code"]

	// Check for filesystem security errors
	if strings.Contains(stderr, "outside working directory") ||
		strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
		strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
		strings.Contains(stderr, "security warning") ||
		exitCode != "0" {

		// Subagent encountered a security error or failed
		// Return a special error format that tells the primary agent to stop retrying
		errorMsg := fmt.Sprintf("SUBAGENT_SECURITY_ERROR: The subagent encountered a security-related error or requires user authorization.\n\n"+
			"Exit code: %s\n"+
			"Stderr: %s\n"+
			"Stdout: %s\n\n"+
			"IMPORTANT: This subagent task requires user authorization or encountered a blocking error. "+
			"Do NOT retry this subagent call with the same parameters. "+
			"Instead, inform the user about the error and ask for guidance on how to proceed.",
			exitCode, stderr, resultMap["stdout"])

		a.Logger().Debug("Subagent failed with security error, delegating to primary agent\n")
		return errorMsg
	}
	return ""
}

// handleSubagentBudgetExceeded checks if the subagent exceeded its token
// budget and returns a formatted error string when it did. Returns ""
// when the budget was not exceeded.
func handleSubagentBudgetExceeded(a *Agent, resultMap map[string]string) string {
	budgetExceeded := false
	if be, ok := resultMap["budget_exceeded"]; ok {
		budgetExceeded = be == "true"
	}

	if !budgetExceeded {
		return ""
	}

	stdout := resultMap["stdout"]

	// Get token usage from summary if available
	tokensUsed := "unknown"
	if summary, ok := resultMap["summary"]; ok {
		// Try to extract token count from summary
		if strings.Contains(summary, "subagent_total_tokens") {
			parts := strings.Split(summary, ":")
			for i, part := range parts {
				if strings.Contains(part, "subagent_total_tokens") && i+1 < len(parts) {
					tokenStr := strings.TrimSpace(strings.Split(parts[i+1], ",")[0])
					tokenStr = strings.TrimSuffix(tokenStr, "\"")
					tokensUsed = tokenStr
					break
				}
			}
		}
	}

	errorMsg := fmt.Sprintf("SUBAGENT_TOKEN_BUDGET_EXCEEDED: The subagent consumed its entire token budget and was terminated to control costs.\n\n"+
		"Tokens used: %s\n"+
		"Budget limit: %d tokens\n\n"+
		"The subagent has produced partial output and made progress on the task. "+
		"IMPORTANT: Do NOT automatically retry the subagent with the same prompt. "+
		"Instead, evaluate the partial output below and decide:\n"+
		"1. Is the task complete enough to continue?\n"+
		"2. Can you complete the remaining work yourself?\n"+
		"3. Should you ask the user for guidance on how to proceed?\n\n"+
		"Partial subagent output:\n%s",
		tokensUsed, DefaultSubagentTokenBudget, stdout)

	a.Logger().Debug("Subagent exceeded token budget, returning partial output to primary agent\n")
	return errorMsg
}

// handleSubagentNonSecurityFailure processes non-zero exit codes that are
// not security errors. Returns a formatted error string for security
// failures, or "" for generic failures (which are logged but not returned
// as errors). Mutates resultMap to add an "error" key on failure.
func handleSubagentNonSecurityFailure(a *Agent, resultMap map[string]string) string {
	exitCode := "0"
	if ec, ok := resultMap["exit_code"]; ok {
		exitCode = ec
	}
	if exitCode == "0" {
		return ""
	}

	stderr := resultMap["stderr"]
	stdout := resultMap["stdout"]

	// Check for specific error patterns that indicate we should stop retrying
	if strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
		strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
		strings.Contains(stderr, "security") ||
		strings.Contains(stdout, "SUBAGENT_SECURITY_ERROR") {

		// This is a security/authorization error - don't retry
		errorMsg := fmt.Sprintf("SUBAGENT_FAILED: The subagent encountered a security or authorization error that prevents it from completing the task.\n\n"+
			"Exit code: %s\n"+
			"Error: %s\n\n"+
			"This error requires user intervention. Do NOT retry the subagent call. "+
			"Instead, report the error to the user and ask for guidance.",
			exitCode, stderr)

		a.Logger().Debug("Subagent failed with security error, stopping retry loop\n")
		return errorMsg
	}

	// For other errors, add a warning but don't prevent retries entirely
	// The agent may still retry, but we add tracking to prevent infinite loops
	a.Logger().Debug("Subagent failed with exit code %s\n", exitCode)
	// Add error indicator to result map
	resultMap["error"] = fmt.Sprintf("Subagent failed with exit code %s. Error output: %s", exitCode, stderr)
	return ""
}

// buildSubagentFinalResult marshals the typed envelope and returns the
// JSON string for the handler's return value.
func buildSubagentFinalResult(a *Agent, resultMap map[string]string, result *SubagentResult) (string, error) {
	// SP-059 Phase 2a/2d: marshal the typed envelope (preserves all old
	// JSON keys for LLM compat) plus the new status / metrics / manifest
	// fields. The Status enum supersedes the SUBAGENT_* sentinel string
	// prefixes for in-process callers — the sentinels themselves still
	// appear in earlier returned error messages so model-side behavior is
	// unchanged.
	ret := buildSubagentReturn(resultMap, result, statusFromResult(result, resultMap))
	jsonStr, jsonErr := ret.MarshalJSONIndent()
	if jsonErr != nil {
		return "", agenterrors.NewAgent("subagent.spawn", "failed to marshal subagent result", jsonErr)
	}

	a.Logger().Debug("Subagent spawn result: %s\n", jsonStr)
	return jsonStr, nil
}
