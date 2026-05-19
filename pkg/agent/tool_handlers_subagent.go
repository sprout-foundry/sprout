package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE    = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE   = 1024 * 1024      // 1MB
	BATCH_SIZE                  = 50               // Number of lines to batch before publishing
	DefaultSubagentTokenBudget  = 2_000_000        // Default token budget for subagents
)

func handleRunSubagent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	prompt, err := convertToString(args["prompt"], "prompt")
	if err != nil {
		return "", fmt.Errorf("failed to convert prompt parameter: %w", err)
	}

	a.debugLog("Spawning subagent with task: %s\n", truncateString(prompt, 100))

	// Parse optional context parameter
	var context string
	if ctxVal, ok := args["context"]; ok && ctxVal != nil {
		if ctxStr, ok := ctxVal.(string); ok && ctxStr != "" {
			context = ctxStr
			a.debugLog("Subagent context provided: %s\n", truncateString(context, 100))
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
			a.debugLog("Subagent files provided: %s\n", filesStr)
		}
	}

	// Parse optional working_dir parameter
	var workingDir string
	if wdVal, ok := args["working_dir"]; ok && wdVal != nil {
		if wdStr, ok := wdVal.(string); ok && wdStr != "" {
			workingDir = wdStr
			a.debugLog("Subagent working_dir specified: %s\n", workingDir)
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
	if personaVal, ok := args["persona"]; ok && personaVal != nil {
		if personaStr, ok := personaVal.(string); ok && personaStr != "" {
			persona = personaStr
			a.debugLog("Subagent persona specified: %s\n", persona)
		}
	}

	// Default to "general" persona if not specified
	if persona == "" {
		persona = "general"
		a.debugLog("No persona specified, using default: general\n")
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

		a.debugLog("Validated file path: %s -> %s\n", filePath, absPath)
	}

	// If there are files outside the workspace, prompt for user approval
	var subagentWorkspaceRoot string = absWorkspaceDir // Default to current workspace root

	if len(outsidePaths) > 0 {
		// Check for auto-approval conditions
		// Unsafe mode bypasses filesystem security checks automatically
		alreadyApproved := a.GetUnsafeMode()
		if !alreadyApproved {
			// If user already approved filesystem access this session, skip re-prompting
			alreadyApproved = a.IsSecurityBypassApproved()
		}

		if !alreadyApproved {
			// CRITICAL: When running as a subagent, we CANNOT prompt for user confirmation
			// because stdin is /dev/null. Instead, we must reject the request.
			if a.IsSubagent() {
				a.debugLog("Subagent encountered external workspace request, cannot prompt for approval (running as subagent)\n")
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
					a.debugLog("User rejected subagent access to external workspace\n")
					return "", fmt.Errorf("file paths outside workspace rejected by user: %v", outsidePaths)
				}
				a.debugLog("User approved subagent access to external workspace via webui\n")
			} else if canPrompt {
				// CLI: prompt user interactively via terminal stdin
				cliPrompt := "[WARN] Subagent External Workspace\n\n" + prompt + "\n\nAllow? (yes/no): "
				if !logger.AskForConfirmation(cliPrompt, false, false) {
					a.debugLog("User rejected subagent access to external workspace\n")
					return "", fmt.Errorf("file paths outside workspace rejected by user: %v", outsidePaths)
				}
				a.debugLog("User approved subagent access to external workspace via CLI\n")
			} else {
				// No prompting available (non-interactive): reject
				a.debugLog("Cannot prompt for subagent external workspace approval (non-interactive)\n")
				return "", fmt.Errorf("file paths outside workspace require approval but prompting is not available: %v", outsidePaths)
			}

			// Mark that user has approved filesystem access this session
			a.SetSecurityBypassApproved()
		} else {
			a.debugLog("Auto-approving subagent external workspace (unsafe mode or session bypass)\n")
		}

		// Compute common parent directory of all files as the new workspace root
		subagentWorkspaceRoot = commonParent(absFilePaths)
		a.debugLog("Computed subagent workspace root: %s (from %d file paths)\n", subagentWorkspaceRoot, len(absFilePaths))
	}

	// If working_dir is explicitly specified, override the subagent workspace root
	if workingDir != "" {
		// Warn if any referenced files fall outside the working_dir scope
		for _, absPath := range absFilePaths {
			if !isPathInWorkspace(absPath, workingDir) && !isPathInTmp(absPath) {
				a.debugLog("Warning: file %s is outside working_dir %s; subagent may not be able to access it\n", absPath, workingDir)
			}
		}
		subagentWorkspaceRoot = workingDir
		a.debugLog("Overriding subagent workspace root with working_dir: %s\n", subagentWorkspaceRoot)
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
				a.debugLog("Failed to read file %s for subagent context: %v\n", filePath, err)
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

	a.debugLog("Spawning subagent with enhanced prompt (length: %d)\n", enhancedPrompt.Len())

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
				a.debugLog("Using persona '%s': provider=%s model=%s system_prompt=%s\n",
					persona, provider, model, systemPromptPath)
				a.warnSubagentFallback(fmt.Sprintf("persona '%s'", persona), strings.TrimSpace(subagentType.Provider), strings.TrimSpace(subagentType.Model), provider, model)
			} else {
				a.debugLog("Warning: Persona '%s' not found or disabled, using default subagent config\n", persona)
				provider = config.GetSubagentProvider()
				model = config.GetSubagentModel()
				a.warnSubagentFallback("default subagent config", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
			}
		} else {
			// No persona specified, use default subagent config
			provider = config.GetSubagentProvider()
			model = config.GetSubagentModel()
			a.debugLog("Using subagent provider=%s model=%s from config\n", provider, model)
			a.warnSubagentFallback("default subagent config", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
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
			a.debugLog("Inheriting parent agent provider/model: provider=%s model=%s\n", provider, model)
		}
	} else {
		a.debugLog("Warning: No config manager available, using parent agent defaults\n")
		provider = a.GetProvider()
		model = a.GetModel()
		a.warnSubagentFallback("missing config manager", "", "", provider, model)
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
			a.debugLog("Loaded system prompt from %s\n", absPromptPath)
		} else {
			a.debugLog("Failed to load system prompt from %s: %v\n", absPromptPath, err)
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
	_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[~] Spawning subagent [%s]: provider=%s, model=%s\n", persona, displayProvider, displayModel)))

	runner := a.GetSubagentRunner()
	result := runner.Run(ctx, enhancedPrompt.String(), SubagentOptions{
		Persona:      persona,
		Model:        model,
		Provider:     provider,
		SystemPrompt: systemPromptText,
		WorkingDir:   workingDir,
	})

	// Convert SubagentResult to resultMap format for backward compatibility
	resultMap := map[string]string{
		"stdout":          result.Output,
		"stderr":          "",
		"exit_code":       "0",
		"completed":       "true",
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
		a.debugLog("Subagent error: %v\n", result.Error)
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

	// Extract summary from stdout
	if stdout, ok := resultMap["stdout"]; ok {
		summary := extractSubagentSummary(stdout)
		summaryJSON, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			a.debugLog("Failed to marshal summary: %v\n", err)
			resultMap["summary"] = fmt.Sprintf("Error creating summary: %v", err)
		} else {
			resultMap["summary"] = string(summaryJSON)
			a.debugLog("Extracted subagent summary: %s\n", string(summaryJSON))
		}

		// Track subagent costs in parent agent's totals
		// Parse the cost metrics from the summary and add to parent's tracking
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
				a.debugLog("Tracked subagent costs: %d tokens, $%.6f\n", totalTokens, totalCost)
			}
		}
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

			a.debugLog("Subagent failed with security error, delegating to primary agent\n")
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

		a.debugLog("Subagent exceeded token budget, returning partial output to primary agent\n")
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

			a.debugLog("Subagent failed with security error, stopping retry loop\n")
			return errorMsg, nil
		}

		// For other errors, add a warning but don't prevent retries entirely
		// The agent may still retry, but we add tracking to prevent infinite loops
		a.debugLog("Subagent failed with exit code %s\n", exitCode)
		// Add error indicator to result map
		resultMap["error"] = fmt.Sprintf("Subagent failed with exit code %s. Error output: %s", exitCode, stderr)
	}

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal subagent result: %w", jsonErr)
	}

	a.debugLog("Subagent spawn result: %s\n", string(jsonBytes))
	return string(jsonBytes), nil
}
