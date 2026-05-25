package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE    = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE   = 1024 * 1024      // 1MB
	BATCH_SIZE                  = 50               // Number of lines to batch before publishing
	DefaultSubagentTokenBudget  = 2_000_000        // Default token budget for subagents
)

// MILESTONE_PHASES defines phases that trigger immediate publish without batching
var MILESTONE_PHASES = []string{"spawn", "complete", "step"}

// subagentBatchBuffer holds buffered output for a subagent task
type subagentBatchBuffer struct {
	lines      []string
	lineCount  int
	taskID     string
	persona    string
	isParallel bool
}

// Global batch buffer manager
var (
	batchBuffers = make(map[string]*subagentBatchBuffer)
	bufferMu     sync.Mutex
)

// flushSubagentBatch publishes buffered lines and clears the buffer
func flushSubagentBatch(buffer *subagentBatchBuffer, a *Agent, toolCallID, toolName string) {
	if len(buffer.lines) == 0 {
		return
	}

	// Publish all buffered lines as a batch
	batchMessage := strings.Join(buffer.lines, "\n")
	details := map[string]interface{}{
		"task_id":     buffer.taskID,
		"persona":     buffer.persona,
		"is_parallel": buffer.isParallel,
		"batch_size":  len(buffer.lines),
	}

	a.publishEvent(events.EventTypeSubagentActivity, events.SubagentActivityEvent(toolCallID, toolName, "output", batchMessage, details))
}

// cleanupSubagentBatch flushes any remaining buffered output for a task
func cleanupSubagentBatch(taskID string, a *Agent, toolCallID, toolName string) {
	bufferMu.Lock()
	defer bufferMu.Unlock()

	if buffer, exists := batchBuffers[taskID]; exists {
		if len(buffer.lines) > 0 {
			flushSubagentBatch(buffer, a, toolCallID, toolName)
		}
		// Remove the buffer to free memory
		delete(batchBuffers, taskID)
	}
}

func publishSubagentActivity(ctx context.Context, a *Agent, phase, message string, details map[string]interface{}) {
	if a == nil {
		return
	}
	message = strings.TrimSpace(stripAnsiCodes(message))
	if message == "" {
		return
	}
	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	// Check if this is a milestone phase - publish immediately
	isMilestone := false
	for _, milestone := range MILESTONE_PHASES {
		if phase == milestone {
			isMilestone = true
			break
		}
	}

	// Extract task ID from details for batching
	taskID := ""
	if tid, ok := details["task_id"]; ok {
		if tidStr, ok := tid.(string); ok {
			taskID = tidStr
		}
	}

	// If milestone phase, publish immediately without batching
	if isMilestone {
		// Clean up any pending batch buffers before publishing milestone
		if taskID != "" {
			cleanupSubagentBatch(taskID, a, toolCallID, toolName)
		}
		a.publishEvent(events.EventTypeSubagentActivity, events.SubagentActivityEvent(toolCallID, toolName, phase, message, details))
		return
	}

	// For output lines, use batching
	bufferMu.Lock()

	// Get or create buffer for this task
	if taskID == "" {
		taskID = toolCallID
	}

	buffer, exists := batchBuffers[taskID]
	if !exists {
		// Extract persona and is_parallel safely
		persona := ""
		if p, ok := details["persona"]; ok {
			if pStr, ok := p.(string); ok {
				persona = pStr
			}
		}
		isParallel := false
		if p, ok := details["is_parallel"]; ok {
			if pBool, ok := p.(bool); ok {
				isParallel = pBool
			}
		}

		buffer = &subagentBatchBuffer{
			lines:      make([]string, 0, BATCH_SIZE),
			lineCount:  0,
			taskID:     taskID,
			persona:    persona,
			isParallel: isParallel,
		}
		batchBuffers[taskID] = buffer
	}

	// Add line to buffer
	buffer.lines = append(buffer.lines, message)
	buffer.lineCount++

	// Check if batch is full - clear buffer first, then flush
	if buffer.lineCount >= BATCH_SIZE {
		buffer.lines = buffer.lines[:0]
		buffer.lineCount = 0
		bufferMu.Unlock()
		flushSubagentBatch(buffer, a, toolCallID, toolName)
		bufferMu.Lock()
	}

	bufferMu.Unlock()
}

// Tool handler implementations for subagent operations

// extractSubagentSummary parses stdout from a subagent execution to extract key information
// Optimized to avoid regex compilation in loops and process only relevant lines
func extractSubagentSummary(stdout string) map[string]string {
	summary := make(map[string]string)

	// Pre-compile regex patterns once (outside the loop)
	passedRe := regexp.MustCompile(`(\d+)\s+passed`)
	failedRe := regexp.MustCompile(`(\d+)\s+failed`)
	todoRe := regexp.MustCompile(`(Added|Marked|Created|Updated|Completed|Removed)\s+(\d+)\s+todos?`)
	cmdRe := regexp.MustCompile(`(?:command|Running):\s+([^\n]+)`)

	// Compile metrics regex patterns once
	totalTokensRe := regexp.MustCompile(`total_tokens=(\d+)`)
	promptTokensRe := regexp.MustCompile(`prompt_tokens=(\d+)`)
	completionTokensRe := regexp.MustCompile(`completion_tokens=(\d+)`)
	totalCostRe := regexp.MustCompile(`total_cost=([\d.]+)`)
	cachedTokensRe := regexp.MustCompile(`cached_tokens=(\d+)`)

	lines := strings.Split(stdout, "\n")

	var fileChanges []string
	var buildStatus string
	var testStatus string
	var errors []string
	var todosCreated []string
	var commandsExecuted []string
	var testPassCount, testFailCount int

	// Process lines but limit to first 10,000 to avoid excessive processing
	maxLines := 10000
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	for _, line := range lines {
		// Skip empty lines early
		if line == "" {
			continue
		}

		// Only trim if needed (check if line has leading/trailing whitespace)
		trimmedLine := line
		if line[0] == ' ' || line[0] == '\t' || line[len(line)-1] == ' ' || line[len(line)-1] == '\t' {
			trimmedLine = strings.TrimSpace(line)
		}

		// Fast-path checks using byte operations for common prefixes
		if len(trimmedLine) > 0 {
			firstChar := trimmedLine[0]

			// Extract file operations (fast ASCII checks)
			switch firstChar {
			case 'C', 'c':
				if strings.HasPrefix(trimmedLine, "Created:") || strings.HasPrefix(trimmedLine, "Wrote") {
					file := strings.TrimSpace(trimmedLine[8:])
					if strings.HasPrefix(trimmedLine, "Created:") {
						file = strings.TrimSpace(trimmedLine[8:])
					} else if strings.HasPrefix(trimmedLine, "Wrote") {
						file = strings.TrimSpace(trimmedLine[6:])
					}
					fileChanges = append(fileChanges, "Created: "+file)
				}
			case 'M', 'm':
				if strings.HasPrefix(trimmedLine, "Modified:") {
					file := strings.TrimSpace(trimmedLine[9:])
					fileChanges = append(fileChanges, "Modified: "+file)
				}
			case 'D', 'd':
				if strings.HasPrefix(trimmedLine, "Deleted:") {
					file := strings.TrimSpace(trimmedLine[8:])
					fileChanges = append(fileChanges, "Deleted: "+file)
				}
			case 'U', 'u':
				if strings.HasPrefix(trimmedLine, "Updated:") {
					file := strings.TrimSpace(trimmedLine[8:])
					fileChanges = append(fileChanges, "Updated: "+file)
				}
			case 'E', 'e':
				if strings.HasPrefix(trimmedLine, "Error:") || strings.HasPrefix(trimmedLine, "error:") {
					errors = append(errors, trimmedLine)
				}
			case 'S', 's':
				if strings.HasPrefix(trimmedLine, "SUBAGENT_METRICS:") {
					// Parse the metrics using pre-compiled regex
					if matches := totalTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_total_tokens"] = matches[1]
					}
					if matches := promptTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_prompt_tokens"] = matches[1]
					}
					if matches := completionTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_completion_tokens"] = matches[1]
					}
					if matches := totalCostRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_total_cost"] = matches[1]
					}
					if matches := cachedTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_cached_tokens"] = matches[1]
					}
					continue // Skip further processing for metrics lines
				}
			}

			// Extract build status (only if line contains "Build:")
			if strings.Contains(trimmedLine, "Build:") {
				if strings.Contains(trimmedLine, "[OK] Passed") {
					buildStatus = "passed"
				} else if strings.Contains(trimmedLine, "[OK] Failed") || strings.Contains(trimmedLine, "[FAIL] Failed") {
					buildStatus = "failed"
				}
			}

			// Extract test status and counts
			if strings.Contains(trimmedLine, "Test:") || strings.Contains(trimmedLine, "Tests:") {
				if strings.Contains(trimmedLine, "[OK] Passed") {
					testStatus = "passed"
				} else if strings.Contains(trimmedLine, "[OK] Failed") || strings.Contains(trimmedLine, "[FAIL] Failed") {
					testStatus = "failed"
				}

				// Extract test counts using pre-compiled regex
				if matches := passedRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &testPassCount)
				}
				if matches := failedRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &testFailCount)
				}
			}

			// Extract todo list operations
			if strings.Contains(trimmedLine, "TodoWrite") || strings.Contains(trimmedLine, "todo list") {
				if matches := todoRe.FindStringSubmatch(trimmedLine); len(matches) > 2 {
					todosCreated = append(todosCreated, matches[0])
				}
			}

			// Extract shell commands executed
			if strings.Contains(trimmedLine, "$") || strings.Contains(trimmedLine, "shell_command") {
				if strings.HasPrefix(trimmedLine, "$") {
					cmd := strings.TrimSpace(trimmedLine[1:])
					if cmd != "" {
						commandsExecuted = append(commandsExecuted, cmd)
					}
				}
				if strings.Contains(trimmedLine, "Executing command") || strings.Contains(trimmedLine, "Running") {
					if matches := cmdRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						commandsExecuted = append(commandsExecuted, strings.TrimSpace(matches[1]))
					}
				}
			}
		}
	}

	if len(fileChanges) > 0 {
		summary["files"] = strings.Join(fileChanges, "; ")
	}
	if buildStatus != "" {
		summary["build_status"] = buildStatus
	}
	if testStatus != "" {
		summary["test_status"] = testStatus
		if testPassCount > 0 || testFailCount > 0 {
			summary["test_counts"] = fmt.Sprintf("%d passed, %d failed", testPassCount, testFailCount)
		}
	}
	if len(errors) > 0 {
		summary["errors"] = strings.Join(errors, "; ")
	}
	if len(todosCreated) > 0 {
		summary["todos"] = strings.Join(todosCreated, "; ")
	}
	if len(commandsExecuted) > 0 {
		// Limit to first 10 commands to avoid overwhelming output
		if len(commandsExecuted) > 10 {
			commandsExecuted = commandsExecuted[:10]
			summary["commands"] = strings.Join(commandsExecuted, "; ") + "..."
		} else {
			summary["commands"] = strings.Join(commandsExecuted, "; ")
		}
	}

	return summary
}

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
	if personaVal, ok := args["persona"]; ok && personaVal != nil {
		if personaStr, ok := personaVal.(string); ok && personaStr != "" {
			persona = personaStr
			a.Logger().Debug("Subagent persona specified: %s\n", persona)
		}
	}

	// Default to "general" persona if not specified
	if persona == "" {
		persona = "general"
		a.Logger().Debug("No persona specified, using default: general\n")
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
			// If user already approved filesystem access this session, skip re-prompting
			alreadyApproved = a.IsSecurityBypassApproved()
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

			// Mark that user has approved filesystem access this session
			a.SetSecurityBypassApproved()
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

		// Check if there's a role matching this persona name (SP-007-1).
		// Roles are user-defined YAML configs that can override persona defaults.
		// Resolution order: workspace → global.
		roleFound := false
		if persona != "" {
			if rm := a.configManager.GetRoleManager(); rm != nil {
				if role, err := rm.Resolve(persona); err == nil && role != nil {
					// Use role config to set provider/model/system prompt
					if role.Provider != "" {
						provider = role.Provider
					}
					if role.Model != "" {
						model = role.Model
					}
					if role.SystemPrompt != "" {
						systemPromptText = role.SystemPrompt
					}
					a.Logger().Debug("Using role '%s': provider=%s model=%s\n", persona, role.Provider, role.Model)
					if role.Provider != "" || role.Model != "" {
						explicitSubagentConfig = true
					}
					roleFound = true
				}
			}
		}

		if !roleFound && persona != "" {
			// Get persona-specific configuration
			subagentType := config.GetSubagentType(persona)
			if subagentType != nil {
				// Check LocalOnly flag - reject in cloud mode
				if subagentType.LocalOnly && !a.IsLocalMode() {
					return "", fmt.Errorf("persona '%s' is local-only and cannot be used as a subagent in cloud mode", persona)
				}
				// Check Delegatable flag - reject non-delegatable personas.
				// Exception: EA personas (executive_assistant) can spawn any persona
				// as a subagent, regardless of the delegatable flag. This enables
				// the three-level delegation chain: EA (depth 0) → orchestrator
				// (depth 1) → coder/tester (depth 2).
				if !subagentType.Delegatable && !a.hasEASpawnAuthority() {
					return "", fmt.Errorf("persona '%s' is not designed to be used as a subagent (delegatable=false)", persona)
				}
				// EA cannot spawn another EA — prevents infinite nesting chains.
				if a.rootPersonaID == "executive_assistant" && persona == "executive_assistant" {
					return "", fmt.Errorf("executive_assistant cannot spawn another executive_assistant (prevents infinite nesting)")
				}
				// No persona can spawn itself — prevents self-recursion.
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
				a.warnSubagentFallback(fmt.Sprintf("persona '%s'", persona), strings.TrimSpace(subagentType.Provider), strings.TrimSpace(subagentType.Model), provider, model)
			} else {
				a.Logger().Debug("Warning: Persona '%s' not found or disabled, using default subagent config\n", persona)
				provider = config.GetSubagentProvider()
				model = config.GetSubagentModel()
				a.warnSubagentFallback("default subagent config", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
			}
		} else if persona == "" {
			// No persona specified, use default subagent config
			provider = config.GetSubagentProvider()
			model = config.GetSubagentModel()
			a.Logger().Debug("Using subagent provider=%s model=%s from config\n", provider, model)
			a.warnSubagentFallback("default subagent config", strings.TrimSpace(config.SubagentProvider), strings.TrimSpace(config.SubagentModel), provider, model)
		}
		// If roleFound is true, skip persona resolution — the role already set provider/model.

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
	} else {
		a.Logger().Debug("Warning: No config manager available, using parent agent defaults\n")
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

	// Extract summary from stdout
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
				a.Logger().Debug("Tracked subagent costs: %d tokens, $%.6f\n", totalTokens, totalCost)
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

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal subagent result: %w", jsonErr)
	}

	a.Logger().Debug("Subagent spawn result: %s\n", string(jsonBytes))
	return string(jsonBytes), nil
}

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
	opts := SubagentOptions{}
	if a.configManager != nil {
		maxParallel := a.configManager.GetConfig().GetSubagentMaxParallel()
		if maxParallel > 16 {
			maxParallel = 16
		}
		opts.MaxConcurrentSubagents = maxParallel
	}
	results := runner.RunParallel(ctx, tasks, opts)

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

func (a *Agent) warnSubagentFallback(scope, configuredProvider, configuredModel, effectiveProvider, effectiveModel string) {
	usesProviderFallback := configuredProvider == "" && strings.TrimSpace(effectiveProvider) != ""
	usesModelFallback := configuredModel == "" && strings.TrimSpace(effectiveModel) != ""
	if !usesProviderFallback && !usesModelFallback {
		return
	}

	provider := strings.TrimSpace(effectiveProvider)
	if provider == "" {
		provider = "<system default>"
	}
	model := strings.TrimSpace(effectiveModel)
	if model == "" {
		model = "<provider default>"
	}

	a.PrintLineAsync(fmt.Sprintf("[WARN] Subagent fallback active (%s): provider=%s model=%s", scope, provider, model))
}

// Helper functions for subagent handlers

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripAnsiCodes removes ANSI escape codes from a string
func stripAnsiCodes(s string) string {
	// ANSI escape code regex pattern
	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiEscape.ReplaceAllString(s, "")
}

// isPathInWorkspace checks if a path is within the workspace directory
func isPathInWorkspace(path, workspaceDir string) bool {
	if path == workspaceDir {
		return true
	}
	return strings.HasPrefix(path, workspaceDir+string(filepath.Separator))
}

// isPathInTmp checks if a path is in /tmp/ for temporary file access
func isPathInTmp(path string) bool {
	// Check for /tmp/ or /var/folders/.../T/ (macOS temp dir) or any path containing tmp
	return strings.Contains(path, "/tmp/") ||
		strings.Contains(path, "/var/folders/.../T/") ||
		strings.Contains(strings.ToLower(path), "/tmp/")
}

// commonParent finds the common parent directory of multiple paths
func commonParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	result := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p+string(filepath.Separator), result+string(filepath.Separator)) && p != result {
			result = filepath.Dir(result)
			if result == "/" || result == "." {
				return result
			}
		}
	}
	return result
}

// flushAllSubagentBuffers flushes all pending batch buffers
func flushAllSubagentBuffers(a *Agent) {
	bufferMu.Lock()
	defer bufferMu.Unlock()

	for taskID, buffer := range batchBuffers {
		if len(buffer.lines) > 0 {
			toolCallID := taskID
			toolName := "subagent"
			flushSubagentBatch(buffer, a, toolCallID, toolName)
			// Delete buffer immediately after flushing to prevent memory leak
			delete(batchBuffers, taskID)
		}
	}
}
