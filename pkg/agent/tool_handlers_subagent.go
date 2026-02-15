package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE = 1024 * 1024     // 1MB
	MAX_PARALLEL_SUBAGENTS    = 5
)

// Tool handler implementations for subagent operations

// extractFilePathsFromPrompt uses regex to find file paths mentioned in a prompt.
// Returns a deduplicated list of file paths that exist in the workspace.
func extractFilePathsFromPrompt(prompt string) []string {
	// Common patterns for file paths in prompts:
	// 1. "modify/create/delete FILE_PATH"
	// 2. "in FILE_PATH"
	// 3. "file FILE_PATH"
	// 4. quoted paths: "path/to/file.go" or 'path/to/file.go'
	// 5. Backtick paths: `path/to/file.go`
	// 6. Paths with extensions: .go, .js, .py, .ts, .tsx, .jsx, .md, .json, .yaml, .yml, .txt, etc.

	patterns := []string{
		`"(?:[a-zA-Z]:)?[\w/\-\\.]+\.[\w]+"`,                                         // Double-quoted paths with extension
		`'(?:[a-zA-Z]:)?[\w/\-\\.]+\.[\w]+'`,                                         // Single-quoted paths with extension
		"`(?:[a-zA-Z]:)?[\\w/\\-\\.]+\\.[\\w]+`",                                     // Backtick paths with extension
		`(?:modify|create|delete|update|edit|write|read)\s+["']?([\w/\-\.]+\.[\w]+)`, // Action words + path
		`(?:in|file|at)\s+["']?([\w/\-\.]+\.[\w]+)`,                                  // Prepositions + path
	}

	seen := make(map[string]bool)
	var filePaths []string

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(prompt, -1)

		for _, match := range matches {
			// Use the last capturing group if available, otherwise use the full match
			var path string
			if len(match) > 1 {
				path = match[len(match)-1]
			} else {
				path = match[0]
			}

			// Clean up the path (remove quotes, backticks)
			path = strings.Trim(path, "\"'`")
			path = strings.TrimSpace(path)

			// Validate it looks like a file path (contains extension or slash)
			if path != "" && (strings.Contains(path, ".") || strings.Contains(path, "/") || strings.Contains(path, "\\")) {
				// Check if file exists in workspace
				if _, err := os.Stat(path); err == nil {
					if !seen[path] {
						seen[path] = true
						filePaths = append(filePaths, path)
					}
				}
			}
		}
	}

	return filePaths
}

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
				if strings.Contains(trimmedLine, "✅ Passed") {
					buildStatus = "passed"
				} else if strings.Contains(trimmedLine, "✅ Failed") || strings.Contains(trimmedLine, "❌ Failed") {
					buildStatus = "failed"
				}
			}

			// Extract test status and counts
			if strings.Contains(trimmedLine, "Test:") || strings.Contains(trimmedLine, "Tests:") {
				if strings.Contains(trimmedLine, "✅ Passed") {
					testStatus = "passed"
				} else if strings.Contains(trimmedLine, "✅ Failed") || strings.Contains(trimmedLine, "❌ Failed") {
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
		return "", err
	}

	// Log the subagent execution for visibility
	a.ToolLog("spawning subagent", fmt.Sprintf("task=%q", truncateString(prompt, 60)))
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

	// Parse auto_files parameter (default: true)
	autoFiles := true // Default to true
	if autoFilesVal, ok := args["auto_files"]; ok && autoFilesVal != nil {
		if autoFilesBool, ok := autoFilesVal.(bool); ok {
			autoFiles = autoFilesBool
			a.debugLog("Auto files: %v\n", autoFiles)
		}
	}

	// Parse persona parameter (required, but default to "general" if not specified)
	var persona string
	var systemPromptPath string
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

	// Automatically extract file paths from prompt if auto_files is enabled
	if autoFiles {
		extractedFiles := extractFilePathsFromPrompt(prompt)
		if len(extractedFiles) > 0 {
			a.debugLog("Auto-extracted %d file paths from prompt: %v\n", len(extractedFiles), extractedFiles)
			// Add extracted files that aren't already in the list
			for _, extractedFile := range extractedFiles {
				alreadyIncluded := false
				for _, existingFile := range files {
					if existingFile == extractedFile {
						alreadyIncluded = true
						break
					}
				}
				if !alreadyIncluded {
					files = append(files, extractedFile)
					a.debugLog("Added auto-extracted file: %s\n", extractedFile)
				}
			}
			// Update filesStr to include auto-extracted files
			if filesStr != "" {
				filesStr = strings.Join(files, ",")
			} else {
				filesStr = strings.Join(files, ",")
			}
		}
	}

	// Validate each file path before proceeding
	for _, filePath := range files {
		// Clean the path to eliminate any . or redundant separators
		cleanedPath := filepath.Clean(filePath)
		absPath, err := filepath.Abs(cleanedPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve absolute path for %s: %w", filePath, err)
		}

		// Get workspace directory (current working directory)
		workspaceDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get workspace directory: %w", err)
		}
		absWorkspaceDir, err := filepath.Abs(workspaceDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve absolute workspace path: %w", err)
		}

		// Verify the file is within the workspace
		if !strings.HasPrefix(absPath, absWorkspaceDir+string(filepath.Separator)) && absPath != absWorkspaceDir {
			return "", fmt.Errorf("file path is outside workspace: %s (workspace: %s)", filePath, absWorkspaceDir)
		}

		// Verify the file exists (missing is OK - subagent can create it)
		if _, err := os.Stat(absPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to access file %s: %w", filePath, err)
		}

		a.debugLog("Validated file path: %s -> %s\n", filePath, absPath)
	}

	// Build enhanced prompt with context and files
	enhancedPrompt := new(strings.Builder)

	// Add previous work context section if provided
	if context != "" {
		enhancedPrompt.WriteString("# Previous Work Context\n\n")
		enhancedPrompt.WriteString(context)
		enhancedPrompt.WriteString("\n\n---\n\n")
	}

	// Add recent session work summary if available
	if len(a.taskActions) > 0 {
		enhancedPrompt.WriteString("# Recent Work in This Session\n\n")
		for i, action := range a.taskActions {
			// Show last 10 actions to avoid overwhelming the subagent
			if i >= len(a.taskActions)-10 {
				enhancedPrompt.WriteString(fmt.Sprintf("- %s: %s\n", action.Type, action.Description))
			}
		}
		enhancedPrompt.WriteString("\n---\n\n")
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

	if a.configManager != nil {
		config := a.configManager.GetConfig()

		if persona != "" {
			// Get persona-specific configuration
			subagentType := config.GetSubagentType(persona)
			if subagentType != nil {
				provider = config.GetSubagentTypeProvider(persona)
				model = config.GetSubagentTypeModel(persona)
				systemPromptPath = subagentType.SystemPrompt
				a.debugLog("Using persona '%s': provider=%s model=%s system_prompt=%s\n",
					persona, provider, model, systemPromptPath)
			} else {
				a.debugLog("Warning: Persona '%s' not found or disabled, using default subagent config\n", persona)
				provider = config.GetSubagentProvider()
				model = config.GetSubagentModel()
			}
		} else {
			// No persona specified, use default subagent config
			provider = config.GetSubagentProvider()
			model = config.GetSubagentModel()
			a.debugLog("Using subagent provider=%s model=%s from config\n", provider, model)
		}
	} else {
		a.debugLog("Warning: No config manager available, using defaults\n")
		provider = "" // Will use system default
		model = ""   // Will use system default
	}

	// Create a streaming callback for real-time output
	streamCallback := func(line string, taskID string) {
		// Format the output line for display
		// Don't show context percentage since this is subagent output, not parent agent
		const subagentGray = "\033[38;5;244m" // Even lighter gray for subagent output
		const reset = "\033[0m"

		// Clean ANSI codes from the line to avoid display issues
		cleanLine := stripAnsiCodes(line)

		// Skip empty lines
		if strings.TrimSpace(cleanLine) == "" {
			return
		}

		// Format: → Subagent: <output>
		// For parallel subagents: → [task-id] Subagent: <output>
		var prefix string
		if taskID != "" && taskID != "task-0" {
			prefix = fmt.Sprintf("[%s] ", taskID)
		}

		message := fmt.Sprintf("%s→ %sSubagent: %s%s\n", subagentGray, prefix, cleanLine, reset)
		a.printLineInternal(message)
	}

	resultMap, err := tools.RunSubagent(enhancedPrompt.String(), model, provider, streamCallback, systemPromptPath)
	if err != nil {
		a.debugLog("Subagent spawn error: %v\n", err)
		return "", err
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

	// Check if subagent failed with security-related errors
	// When running as a subagent (LEDIT_FROM_AGENT=1), we can't prompt the user
	// So we need to delegate the security decision back to the primary agent
	if os.Getenv("LEDIT_FROM_AGENT") == "1" {
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
			tokensUsed, tools.GetSubagentMaxTokens(), stdout)

		a.ToolLog("subagent", fmt.Sprintf("budget_exceeded=true tokens=%s", tokensUsed))
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

	// Log subagent completion
	a.ToolLog("subagent completed", fmt.Sprintf("exit_code=%s", exitCode))
	a.debugLog("Subagent spawn result: %s\n", string(jsonBytes))
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
		return "", fmt.Errorf("missing tasks, prompts, or subagents argument")
	}

	// Parse the tasks array
	tasksSlice, ok := tasksRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("tasks/prompts must be an array")
	}

	// Log the parallel subagent execution for visibility
	a.ToolLog("spawning parallel subagents", fmt.Sprintf("%d tasks", len(tasksSlice)))
	a.debugLog("Spawning %d parallel subagents\n", len(tasksSlice))

	var parallelTasks []tools.ParallelSubagentTask
	for i, taskRaw := range tasksSlice {
		task := tools.ParallelSubagentTask{}

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
				return "", err
			}
			task.Prompt = prompt

			// Note: model and provider are set from configuration, not from LLM parameters
			// This ensures consistent subagent behavior configured by the user
		} else {
			return "", fmt.Errorf("each task must be a string or an object")
		}

		parallelTasks = append(parallelTasks, task)
	}

	// Get configured subagent model/provider and apply to all tasks
	var subagentProvider, subagentModel string
	if a.configManager != nil {
		config := a.configManager.GetConfig()
		subagentProvider = config.GetSubagentProvider()
		subagentModel = config.GetSubagentModel()
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

	// Validate number of parallel tasks
	if len(parallelTasks) > MAX_PARALLEL_SUBAGENTS {
		return "", fmt.Errorf("too many parallel tasks: %d exceeds max of %d", len(parallelTasks), MAX_PARALLEL_SUBAGENTS)
	}

	a.debugLog("Spawning %d parallel subagents\n", len(parallelTasks))

	// Create a streaming callback for real-time output (same as single subagent)
	streamCallback := func(line string, taskID string) {
		// Format the output line for display
		const subagentGray = "\033[38;5;244m" // Even lighter gray for subagent output
		const reset = "\033[0m"

		// Clean ANSI codes from the line to avoid display issues
		cleanLine := stripAnsiCodes(line)

		// Skip empty lines
		if strings.TrimSpace(cleanLine) == "" {
			return
		}

		// Format: → [task-id] Subagent: <output>
		var prefix string
		if taskID != "" && taskID != "task-0" {
			prefix = fmt.Sprintf("[%s] ", taskID)
		}

		message := fmt.Sprintf("%s→ %sSubagent: %s%s\n", subagentGray, prefix, cleanLine, reset)
		a.printLineInternal(message)
	}

	resultMap, err := tools.RunParallelSubagents(parallelTasks, false, streamCallback)
	if err != nil {
		a.debugLog("Parallel subagents spawn error: %v\n", err)
		return "", err
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
	if os.Getenv("LEDIT_FROM_AGENT") == "1" {
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

	// Log parallel subagent completion
	logMessage := fmt.Sprintf("%d tasks", len(resultMap))
	if len(failedTasks) > 0 {
		logMessage += fmt.Sprintf(" (%d failed)", len(failedTasks))
	}
	a.ToolLog("parallel subagents completed", logMessage)
	return string(jsonBytes), nil
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
