// Subagent spawn and dispatch logic.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ---------------------------------------------------------------------------
// handleRunSubagent — single subagent spawn/dispatch
// ---------------------------------------------------------------------------

func handleRunSubagent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	prompt, err := convertToString(args["prompt"], "prompt")
	if err != nil {
		return "", fmt.Errorf("failed to convert prompt parameter: %w", err)
	}

	a.Logger().Debug("Spawning subagent with task: %s\n", truncateString(prompt, 100))

	// Parse optional context parameter
	var context string
	if ctxVal, ok := args["context"]; ok && ctxVal != nil {
		if ctxStr, ok := ctxVal.(string); ok && ctxStr != "" {
			context = ctxStr
			a.Logger().Debug("Subagent context provided: %s\n", truncateString(context, 100))
		}
	}

	// Parse optional files parameter (comma-separated list)
	var files []string
	var filesStr string
	if filesVal, ok := args["files"]; ok && filesVal != nil {
		if filesRaw, ok := filesVal.(string); ok && filesRaw != "" {
			// Split by comma and trim spaces
			rawFiles := strings.Split(filesRaw, ",")
			for _, f := range rawFiles {
				if f = strings.TrimSpace(f); f != "" {
					files = append(files, f)
				}
			}
			filesStr = strings.Join(files, ",")
			a.Logger().Debug("Subagent files provided: %s\n", filesStr)
		}
	}

	// Parse optional working_dir parameter
	var workingDir string
	if wdVal, ok := args["working_dir"]; ok && wdVal != nil {
		if wdStr, ok := wdVal.(string); ok && wdStr != "" {
			workingDir = wdStr
			a.Logger().Debug("Subagent working_dir specified: %s\n", workingDir)
		}
	}

	// Validate working_dir if provided
	if workingDir != "" {
		// Expand ~ to $HOME
		if strings.HasPrefix(workingDir, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to resolve home directory: %w", err)
			}
			workingDir = filepath.Join(homeDir, workingDir[2:])
		}

		// Resolve to absolute path
		absWorkingDir, err := filepath.Abs(workingDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve working_dir: %w", err)
		}

		// Resolve symlinks to prevent symlink escape attacks
		resolvedWorkingDir, err := filepath.EvalSymlinks(absWorkingDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve working_dir symlinks: %w", err)
		}

		// Verify target exists and is a directory (use resolved path)
		info, err := os.Stat(resolvedWorkingDir)
		if err != nil {
			return "", fmt.Errorf("working_dir does not exist: %s", resolvedWorkingDir)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("working_dir is not a directory: %s", resolvedWorkingDir)
		}

		// Verify resolved (symlink-target) path is within $HOME
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		// Also resolve $HOME itself in case it's a symlink
		resolvedHome, err := filepath.EvalSymlinks(homeDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory symlinks: %w", err)
		}
		if !isPathInWorkspace(resolvedWorkingDir, resolvedHome) {
			return "", fmt.Errorf("working_dir resolves outside $HOME via symlink: %s -> %s", absWorkingDir, resolvedWorkingDir)
		}

		workingDir = resolvedWorkingDir
	}

	// Parse persona parameter (required, but default to "general" if not specified)
	var persona string
	var systemPromptPath string
	var systemPromptText string
	personaExplicitlyProvided := false
	if personaVal, ok := args["persona"]; ok && personaVal != nil {
		if personaStr, ok := personaVal.(string); ok && personaStr != "" {
			persona = personaStr
			personaExplicitlyProvided = true
			a.Logger().Debug("Subagent persona specified: %s\n", persona)
		}
	}

	// Default to the configured default persona if not specified, falling back
	// to "general" if no default is set. This lets users redirect default
	// spawns via config without editing the catalog.
	if persona == "" {
		if cfg := a.GetConfig(); cfg != nil && strings.TrimSpace(cfg.DefaultSubagentPersona) != "" {
			persona = strings.TrimSpace(cfg.DefaultSubagentPersona)
		} else {
			persona = personas.IDGeneral
		}
		a.Logger().Debug("No persona specified, using default: %s\n", persona)
	}
	persona = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(persona)), "-", "_")

	// Resolve workspace root once for all file validations.
	// In daemon mode the process cwd may differ from the workspace, so we use
	// the agent's workspace root (set via SetWorkspaceRoot) rather than os.Getwd().
	absWorkspaceDir, err := filepath.Abs(a.currentWorkspaceRoot())
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute workspace path: %w", err)
	}

	// Track absolute paths of all files for workspace root computation
	var absFilePaths []string
	var outsidePaths []string

	// Validate each file path before proceeding
	for _, filePath := range files {
		// Clean the path to eliminate any . or redundant separators
		cleanedPath := filepath.Clean(filePath)
		var absPath string
		if filepath.IsAbs(cleanedPath) {
			absPath = cleanedPath
		} else {
			// Resolve relative paths against the workspace root, not the process cwd
			absPath = filepath.Join(absWorkspaceDir, cleanedPath)
		}

		// Track absolute path for later workspace root computation
		absFilePaths = append(absFilePaths, absPath)

		// Check if file is outside workspace and not in /tmp
		isOutsideWorkspace := !isPathInWorkspace(absPath, absWorkspaceDir)
		isInTmp := isPathInTmp(absPath)

		if isOutsideWorkspace && !isInTmp {
			outsidePaths = append(outsidePaths, absPath)
		}

		// Verify the file exists (missing is OK - subagent can create it)
		if _, err := os.Stat(absPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to access file %s: %w", filePath, err)
		}

		a.Logger().Debug("Validated file path: %s -> %s\n", filePath, absPath)
	}

	// If there are files outside the workspace, prompt for user approval
	var subagentWorkspaceRoot string = absWorkspaceDir // Default to current workspace root

	if len(outsidePaths) > 0 {
		// Check for auto-approval conditions
		// Unsafe mode bypasses filesystem security checks automatically
		alreadyApproved := a.GetUnsafeMode()
		if !alreadyApproved {
			// Per-folder allowlist: only auto-approve if EVERY outside
			// path is covered by a folder the user previously approved.
			// The old global flag here was the safety bug — approving
			// one path silently allowed all paths for the session.
			alreadyApproved = true
			for _, p := range outsidePaths {
				if !a.IsFolderSessionAllowed(p) {
					alreadyApproved = false
					break
				}
			}
		}

		if !alreadyApproved {
			// CRITICAL: When running as a subagent, we CANNOT prompt for user confirmation
			// because stdin is /dev/null. Instead, we must reject the request.
			if a.IsSubagent() {
				a.Logger().Debug("Subagent encountered external workspace request, cannot prompt for approval (running as subagent)\n")
				return "", fmt.Errorf("file paths outside workspace require user approval: %v (cannot prompt from subagent context)", outsidePaths)
			}

			// Build approval prompt
			outsidePathsStr := strings.Join(outsidePaths, ", ")
			prompt := fmt.Sprintf("Subagent requests access to files outside the working directory:\n  %s\n\nAllow? This will start the subagent in a directory that covers these files.", outsidePathsStr)

			// Prefer webui approval path when a browser tab is connected
			agentConfig := a.GetConfig()
			logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
			canPrompt := logger != nil && logger.IsInteractive() && !a.IsSubagent()

			if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && !a.IsSubagent() && a.HasActiveWebUIClients() {
				// WEBUI: request approval via event bus for the browser dialog
				extras := map[string]string{
					"risk_type": "Subagent External Workspace",
					"target":    outsidePathsStr,
				}
				if !mgr.RequestToolApproval(a.GetEventBus(), a.GetEventClientID(), a.GetEventUserID(), "run_subagent", "CAUTION", prompt, extras) {
					a.Logger().Debug("User rejected subagent access to external workspace\n")
					return "", fmt.Errorf("file paths outside workspace rejected by user: %v", outsidePaths)
				}
				a.Logger().Debug("User approved subagent access to external workspace via webui\n")
			} else if canPrompt {
				// CLI: prompt user interactively via terminal stdin
				cliPrompt := "[WARN] Subagent External Workspace\n\n" + prompt + "\n\nAllow? (yes/no): "
				if !logger.AskForConfirmation(cliPrompt, false, false) {
					a.Logger().Debug("User rejected subagent access to external workspace\n")
					return "", fmt.Errorf("file paths outside workspace rejected by user: %v", outsidePaths)
				}
				a.Logger().Debug("User approved subagent access to external workspace via CLI\n")
			} else {
				// No prompting available (non-interactive): reject
				a.Logger().Debug("Cannot prompt for subagent external workspace approval (non-interactive)\n")
				return "", fmt.Errorf("file paths outside workspace require approval but prompting is not available: %v", outsidePaths)
			}

			// Mark each outside path's parent as session-allowed so
			// the subagent doesn't re-prompt for the same files.
			// Phase 3 will offer the user a "once vs folder" choice
			// in the dialog itself; for now we widen to parents.
			for _, p := range outsidePaths {
				a.AddSessionAllowedFolder(filepath.Dir(p))
			}
		} else {
			a.Logger().Debug("Auto-approving subagent external workspace (unsafe mode or session bypass)\n")
		}

		// Compute common parent directory of all files as the new workspace root
		subagentWorkspaceRoot = commonParent(absFilePaths)
		a.Logger().Debug("Computed subagent workspace root: %s (from %d file paths)\n", subagentWorkspaceRoot, len(absFilePaths))
	}

	// If working_dir is explicitly specified, override the subagent workspace root
	if workingDir != "" {
		// Warn if any referenced files fall outside the working_dir scope
		for _, absPath := range absFilePaths {
			if !isPathInWorkspace(absPath, workingDir) && !isPathInTmp(absPath) {
				a.Logger().Debug("Warning: file %s is outside working_dir %s; subagent may not be able to access it\n", absPath, workingDir)
			}
		}
		subagentWorkspaceRoot = workingDir
		a.Logger().Debug("Overriding subagent workspace root with working_dir: %s\n", subagentWorkspaceRoot)
	}

	// Build enhanced prompt with context and files
	enhancedPrompt := new(strings.Builder)

	// Add previous work context section if provided
	if context != "" {
		enhancedPrompt.WriteString("# Previous Work Context\n\n")
		enhancedPrompt.WriteString(context)
		enhancedPrompt.WriteString("\n\n---\n\n")
	}

	// Add relevant files section if provided
	if len(files) > 0 {
		enhancedPrompt.WriteString("# Relevant Files\n\n")
		for _, filePath := range files {
			enhancedPrompt.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))
			content, err := tools.ReadFile(ctx, filePath)
			if err != nil {
				enhancedPrompt.WriteString(fmt.Sprintf("[Error reading file: %v]\n\n", err))
				a.Logger().Debug("Failed to read file %s for subagent context: %v\n", filePath, err)
			} else {
				enhancedPrompt.WriteString(content)
				enhancedPrompt.WriteString("\n\n")
			}
		}
		enhancedPrompt.WriteString("---\n\n")
	}

	// Add task section
	enhancedPrompt.WriteString("# Your Task\n\n")
	enhancedPrompt.WriteString(prompt)

	a.Logger().Debug("Spawning subagent with enhanced prompt (length: %d)\n", enhancedPrompt.Len())

	// Validate enhanced prompt size
	if enhancedPrompt.Len() > MAX_SUBAGENT_CONTEXT_SIZE {
		return "", fmt.Errorf("enhanced prompt exceeds maximum size of %d bytes", MAX_SUBAGENT_CONTEXT_SIZE)
	}

	// Get subagent provider and model from configuration
	// If persona is specified, use persona-specific provider/model
	var provider string
	var model string
	explicitSubagentConfig := false

	if a.configManager != nil {
		config := a.configManager.GetConfig()

		if persona != "" {
			// Get persona-specific configuration
			subagentType := config.GetSubagentType(persona)
			if subagentType != nil {
				// Check LocalOnly flag - reject in cloud mode
				if subagentType.LocalOnly && !a.IsLocalMode() {
					return "", fmt.Errorf("persona '%s' is local-only and cannot be used as a subagent in cloud mode", persona)
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
					return "", fmt.Errorf("persona '%s' is not spawnable from %q (delegatable=false and not listed in spawner's can_spawn_non_delegatable)", persona, a.GetActivePersona())
				}
				// No persona can spawn itself — orthogonal to spawn_policy.
				currentPersona := a.GetActivePersona()
				if currentPersona != "" && currentPersona == persona {
					return "", fmt.Errorf("persona '%s' cannot spawn itself (prevents self-recursion)", persona)
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

	// Print the provider/model being used for this subagent
	displayProvider := provider
	if displayProvider == "" {
		displayProvider = "default"
	}
	displayModel := model
	if displayModel == "" {
		displayModel = "default"
	}
	publishSubagentActivity(ctx, a, "spawn", fmt.Sprintf("Starting %s", persona), map[string]interface{}{
		"persona":     persona,
		"provider":    displayProvider,
		"model":       displayModel,
		"is_parallel": false,
	})
	printSubagentStart(persona, displayProvider, displayModel)

	runner := a.GetSubagentRunner()
	result := runner.Run(ctx, enhancedPrompt.String(), SubagentOptions{
		Persona:      persona,
		Model:        model,
		Provider:     provider,
		SystemPrompt: systemPromptText,
		WorkingDir:   workingDir,
	})
	printSubagentDone(persona, result)

	// SP-059 Phase 2c (missing step): merge the subagent's tracked
	// changes into the primary's ChangeTracker so list_changes,
	// recover_file, and revert_my_changes see subagent edits.
	a.MergeSubagentChanges(result.FileChanges, persona)

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

	// Truncate output if it exceeds size limit
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

	// Add context_used field
	if context != "" {
		resultMap["context_used"] = "true"
	} else {
		resultMap["context_used"] = "false"
	}

	// Add files_used field
	if filesStr != "" {
		resultMap["files_used"] = filesStr
	} else {
		resultMap["files_used"] = ""
	}

	// Add working_dir field
	if workingDir != "" {
		resultMap["working_dir"] = workingDir
	} else {
		resultMap["working_dir"] = ""
	}

	// Check if subagent failed with security-related errors
	// When running as a subagent, we can't prompt the user
	// So we need to delegate the security decision back to the primary agent
	if a.IsSubagent() {
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
			return errorMsg, nil
		}
	}

	// For non-subagent context (primary agent), check if the subagent failed
	// and add a clear message to prevent retry loops
	exitCode := "0"
	if ec, ok := resultMap["exit_code"]; ok {
		exitCode = ec
	}
	completionMessage := "Subagent completed"
	if exitCode != "0" {
		completionMessage = fmt.Sprintf("Subagent failed (exit code %s)", exitCode)
	}
	publishSubagentActivity(ctx, a, "complete", completionMessage, map[string]interface{}{
		"persona":     persona,
		"exit_code":   exitCode,
		"is_parallel": false,
	})

	// Flush any remaining buffered output before completing
	flushAllSubagentBuffers(a)

	// Check if subagent exceeded token budget
	budgetExceeded := false
	if be, ok := resultMap["budget_exceeded"]; ok {
		budgetExceeded = be == "true"
	}

	if budgetExceeded {
		// Subagent exceeded token budget - provide clear guidance to primary agent
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
		return errorMsg, nil
	}

	if exitCode != "0" {
		// Subagent failed - add clear message to prevent infinite retry loops
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
			return errorMsg, nil
		}

		// For other errors, add a warning but don't prevent retries entirely
		// The agent may still retry, but we add tracking to prevent infinite loops
		a.Logger().Debug("Subagent failed with exit code %s\n", exitCode)
		// Add error indicator to result map
		resultMap["error"] = fmt.Sprintf("Subagent failed with exit code %s. Error output: %s", exitCode, stderr)
	}

	// SP-059 Phase 2a/2d: marshal the typed envelope (preserves all old
	// JSON keys for LLM compat) plus the new status / metrics / manifest
	// fields. The Status enum supersedes the SUBAGENT_* sentinel string
	// prefixes for in-process callers — the sentinels themselves still
	// appear in earlier returned error messages so model-side behavior is
	// unchanged.
	ret := buildSubagentReturn(resultMap, result, statusFromResult(result, resultMap))
	jsonStr, jsonErr := ret.MarshalJSONIndent()
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal subagent result: %w", jsonErr)
	}

	a.Logger().Debug("Subagent spawn result: %s\n", jsonStr)
	return jsonStr, nil
}

// ---------------------------------------------------------------------------
// handleRunParallelSubagents — parallel dispatch
// ---------------------------------------------------------------------------

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

	a.Logger().Debug("Spawning %d parallel subagents\n", len(tasksSlice))

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

	a.Logger().Debug("Spawning %d parallel subagents\n", len(parallelTasks))

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
	printParallelSubagentStart(len(parallelTasks), displayProvider, displayModel)

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
	opts := SubagentOptions{}
	if a.configManager != nil {
		maxParallel := a.configManager.GetConfig().GetSubagentMaxParallel()
		if maxParallel > 16 {
			maxParallel = 16
		}
		opts.MaxConcurrentSubagents = maxParallel
	}
	results := runner.RunParallel(ctx, tasks, opts)

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
					a.TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens, totalCost, cachedTokens, 0)
					a.Logger().Debug("Tracked parallel subagent [%s] costs: %d tokens, $%.6f\n", taskID, totalTokens, totalCost)
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

				a.Logger().Debug("Parallel subagent [%s] failed with security error, delegating to primary agent\n", taskID)
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

		a.Logger().Debug("Parallel subagents failed with security errors, stopping retry loop\n")
		return errorMsg, nil
	}

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal parallel subagents result: %w", jsonErr)
	}

	a.Logger().Debug("Parallel subagents spawn result: %s\n", string(jsonBytes))

	return string(jsonBytes), nil

}
