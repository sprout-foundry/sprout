package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/filesystem"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE  = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE = 1024 * 1024      // 1MB
	MAX_PARALLEL_SUBAGENTS    = 5
)

// Tool handler implementations

func handleShellCommand(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	command := args["command"].(string)

	// Block git write operations - these must use the git tool for approval
	// Read-only operations (status, log, diff, etc.) are allowed through shell_command
	if isGitWriteCommand(command) {
		return "", fmt.Errorf("git write operations require the git tool for approval. Please use the git tool instead (operation: '%s')", command)
	}

	a.ToolLog("executing command", command)
	return a.executeShellCommandWithTruncation(ctx, command)
}

// isGitWriteCommand checks if a command is a git write operation (which should use git tool for approval)
func isGitWriteCommand(command string) bool {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "git ") {
		return false
	}

	// Extract the git subcommand (e.g., "git log" -> "log")
	// Handle git -c flag and other options before subcommand
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return false // Not a complete git command
	}

	// Find the actual subcommand (skip "git" and any leading flags like -c, -C, etc.)
	subcommand := ""
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		// Skip common git options that appear before subcommand
		if strings.HasPrefix(part, "-") {
			continue
		}
		subcommand = part
		break
	}

	if subcommand == "" {
		return false
	}

	// Normalize subcommand (remove dashes, handle branch -d/-D as "branch")
	subcommand = strings.TrimPrefix(subcommand, "--")
	subcommand = strings.TrimPrefix(subcommand, "-")

	// Handle special case: "branch -d" or "branch -D"
	if subcommand == "branch" && len(parts) > 2 {
		// If there's a -d or -D flag, it's a write operation
		for i := 2; i < len(parts); i++ {
			if parts[i] == "-d" || parts[i] == "-D" {
				return true
			}
		}
	}

	// Check if it's a write operation
	writeCommands := []string{
		"commit", "push", "add", "rm", "mv", "reset",
		"rebase", "merge", "checkout", "tag", "clean",
		"stash", "am", "apply", "cherry-pick", "revert",
		"branch", // branch with flags is handled above
	}

	for _, writeCmd := range writeCommands {
		if subcommand == writeCmd {
			return true
		}
	}

	return false
}

// handleGitOperation handles git operations with approval for write operations
func handleGitOperation(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract operation parameter
	operationParam, err := convertToString(args["operation"], "operation")
	if err != nil {
		return "", err
	}

	// Parse and validate the operation type
	operation := tools.GitOperationType(operationParam)

	// Validate that the operation type is known
	if !isValidGitOperation(operation) {
		validOpNames := []string{"commit", "push", "add", "rm", "mv", "reset", "rebase", "merge", "checkout", "branch_delete", "tag", "clean", "stash", "am", "apply", "cherry_pick", "revert"}
		return "", fmt.Errorf("invalid git operation type '%s'. Valid operations: %s. For read-only operations like status, log, diff, etc., use shell_command instead.",
			operationParam, strings.Join(validOpNames, ", "))
	}

	// Extract args parameter (optional)
	var argsStr string
	if argsParam, exists := args["args"]; exists {
		argsStr, _ = convertToString(argsParam, "args")
	}

	// Log the operation
	a.ToolLog("executing git operation", fmt.Sprintf("%s %s", operation, argsStr))

	// For commit operations, use the commit command directly
	if operation == tools.GitOpCommit {
		return handleGitCommitOperation(a)
	}

	// Create an approval prompter
	approvalPrompter := &gitApprovalPrompterAdapter{agent: a}

	// Execute the git operation
	result, err := tools.ExecuteGitOperation(ctx, tools.GitOperation{
		Operation: operation,
		Args:      argsStr,
	}, "", nil, approvalPrompter)

	if err != nil {
		return "", err
	}

	return result, nil
}

// isValidGitOperation checks if a git operation type is valid
func isValidGitOperation(op tools.GitOperationType) bool {
	// All valid operations are write operations
	validOps := []tools.GitOperationType{
		tools.GitOpCommit, tools.GitOpPush, tools.GitOpAdd, tools.GitOpRm,
		tools.GitOpMv, tools.GitOpReset, tools.GitOpRebase,
		tools.GitOpMerge, tools.GitOpCheckout, tools.GitOpBranchDelete,
		tools.GitOpTag, tools.GitOpClean, tools.GitOpStash,
		tools.GitOpAm, tools.GitOpApply, tools.GitOpCherryPick, tools.GitOpRevert,
	}

	for _, validOp := range validOps {
		if op == validOp {
			return true
		}
	}

	return false
}

// handleGitCommitOperation handles git commit operations
// Note: For the full interactive commit flow, users should use the /commit slash command
func handleGitCommitOperation(a *Agent) (string, error) {
	// Check for staged changes first
	stagedOutput, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to check for staged changes: %w", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Use 'git add' to stage files first.", nil
	}

	// For commit operations, we use a simple git commit with a message prompt
	// This is a simplified version - for full interactive commit flow, users should use /commit
	return "", fmt.Errorf("git commit requires the interactive commit flow. Please use the '/commit' slash command instead for the full interactive experience with message generation")
}

// gitApprovalPrompterAdapter implements the GitApprovalPrompter interface using the Agent
type gitApprovalPrompterAdapter struct {
	agent *Agent
}

// PromptForApproval prompts the user for approval to execute a git write operation
func (a *gitApprovalPrompterAdapter) PromptForApproval(command string) (bool, error) {
	// Build the approval prompt
	prompt := fmt.Sprintf("Execute git command: %s", command)

	// Define choices
	choices := []ChoiceOption{
		{Label: "Approve", Value: "y"},
		{Label: "Cancel", Value: "n"},
	}

	// Show the command to be executed
	fmt.Printf("\n🔒 Git Operation Requires Approval\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("\n")

	// Prompt for choice
	choice, err := a.agent.PromptChoice(prompt, choices)
	if err != nil {
		// If UI is not available, fall back to stdin prompt
		if err == ErrUINotAvailable {
			return tools.PromptForGitApprovalStdin(command)
		}
		return false, err
	}

	return choice == "y", nil
}

func handleReadFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Get file path - supports both "path" (new) and "file_path" (legacy)
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	// Parse view_range (Claude Code style: [start, end])
	var startLine, endLine int
	var hasRange bool

	if viewRange, exists := args["view_range"]; exists {
		if arr, ok := viewRange.([]interface{}); ok && len(arr) == 2 {
			if s, ok := toInt(arr[0]); ok {
				startLine = s
				if e, ok := toInt(arr[1]); ok {
					endLine = e
					hasRange = true
				}
			}
		}
	}

	// Log and execute
	if hasRange {
		a.ToolLog("reading file", fmt.Sprintf("%s (lines %d-%d)", path, startLine, endLine))
		a.debugLog("Reading file: %s (lines %d-%d)\n", path, startLine, endLine)
		result, err := tools.ReadFileWithRange(ctx, path, startLine, endLine)

		if err != nil {
			ctx2 := handleFileSecurityError(ctx, a, "read_file", path, err)
			if ctx2 != ctx {
				result, err = tools.ReadFileWithRange(ctx2, path, startLine, endLine)
			}
		}

		a.debugLog("Read file result: %s, error: %v\n", result, err)

		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s (lines %d-%d)", path, startLine, endLine), path)
		}

		return result, err
	}

	a.ToolLog("reading file", path)
	a.debugLog("Reading file: %s\n", path)
	result, err := tools.ReadFile(ctx, path)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "read_file", path, err)
		if ctx2 != ctx {
			result, err = tools.ReadFile(ctx2, path)
		}
	}

	a.debugLog("Read file result: %s, error: %v\n", result, err)

	if err == nil {
		a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s", path), path)
	}

	return result, err
}

// convertToString safely converts a parameter to string with proper error handling
func convertToString(param interface{}, paramName string) (string, error) {
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case map[string]interface{}:
		// If it's a map, try to convert to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("parameter '%s' is an object that cannot be converted to string: %w", paramName, err)
		}
		return string(jsonBytes), nil
	case nil:
		return "", fmt.Errorf("parameter '%s' is missing or null", paramName)
	default:
		return "", fmt.Errorf("parameter '%s' has invalid type %T, expected string", paramName, param)
	}
}

// getFilePath extracts file path from args, supporting both "path" (new) and "file_path" (legacy)
func getFilePath(args map[string]interface{}) (string, error) {
	if path, exists := args["path"]; exists {
		return convertToString(path, "path")
	}
	if filePath, exists := args["file_path"]; exists {
		return convertToString(filePath, "file_path")
	}
	return "", fmt.Errorf("parameter 'path' is required")
}

// getRequiredString extracts a required string parameter
func getRequiredString(args map[string]interface{}, key string) (string, error) {
	val, exists := args[key]
	if !exists {
		return "", fmt.Errorf("parameter '%s' is required", key)
	}
	return convertToString(val, key)
}

// toInt converts an interface{} to int, handling float64 from JSON
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func handleWriteFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	content, err := getRequiredString(args, "content")
	if err != nil {
		return "", err
	}

	a.ToolLog("writing file", fmt.Sprintf("%s (%d bytes)", path, len(content)))
	a.debugLog("Writing file: %s\n", path)

	if trackErr := a.TrackFileWrite(path, content); trackErr != nil {
		a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, path, content)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "write_file", path, err)
		if ctx2 != ctx {
			result, err = tools.WriteFile(ctx2, path, content)
		}
	}

	a.debugLog("Write file result: %s, error: %v\n", result, err)

	// Invalidate cached file metadata when file is successfully written
	// This prevents stale line counts from misleading the model
	if err == nil && a.optimizer != nil {
		a.optimizer.InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "write", content))
		a.debugLog("Published file_changed event: %s (write)\n", path)
	}

	return result, err
}

func handleEditFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	oldStr, err := getRequiredString(args, "old_str")
	if err != nil {
		return "", err
	}

	newStr, err := getRequiredString(args, "new_str")
	if err != nil {
		return "", err
	}

	// Read original for diff
	originalContent, err := tools.ReadFile(ctx, path)
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

	a.ToolLog("editing file", fmt.Sprintf("%s (replacing %d bytes → %d bytes)", path, len(oldStr), len(newStr)))
	a.debugLog("Editing file: %s\n", path)
	a.debugLog("Old string: %s\n", oldStr)
	a.debugLog("New string: %s\n", newStr)

	if trackErr := a.TrackFileEdit(path, oldStr, newStr); trackErr != nil {
		a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(ctx, path, oldStr, newStr)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "edit_file", path, err)
		if ctx2 != ctx {
			originalContent, err = tools.ReadFile(ctx2, path)
			if err != nil {
				return "", fmt.Errorf("failed to read original file for diff: %w", err)
			}
			result, err = tools.EditFile(ctx2, path, oldStr, newStr)
		}
	}

	a.debugLog("Edit file result: %s, error: %v\n", result, err)

	// Invalidate cached file metadata when file is successfully edited
	// This prevents stale line counts from misleading the model
	if err == nil && a.optimizer != nil {
		a.optimizer.InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		var eventContent string
		if eventContent, err = tools.ReadFile(ctx, path); err == nil {
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", eventContent))
			a.debugLog("Published file_changed event: %s (edit)\n", path)
		} else {
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", ""))
			a.debugLog("Published file_changed event: %s (edit, no content)\n", path)
		}
	}

	// Display diff if successful
	if err == nil {
		newContent, readErr := tools.ReadFile(ctx, path)
		if readErr == nil {
			a.ShowColoredDiff(originalContent, newContent, 50)
		}
	}

	return result, err
}

// handleCreate creates a new file with content. Fails if file already exists.
func handleCreate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	fileText, err := getRequiredString(args, "file_text")
	if err != nil {
		return "", err
	}

	a.ToolLog("creating file", fmt.Sprintf("%s (%d bytes)", path, len(fileText)))
	a.debugLog("Creating file: %s\n", path)

	// Fail if file exists (safe create)
	if filesystem.FileExists(path) {
		return "", fmt.Errorf("file already exists: %s (use write_file to overwrite)", path)
	}

	result, err := tools.WriteFile(ctx, path, fileText)
	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "create", path, err)
		if ctx2 != ctx {
			result, err = tools.WriteFile(ctx2, path, fileText)
		}
	}

	a.debugLog("Create file result: %s, error: %v\n", result, err)

	if err == nil {
		if trackErr := a.TrackFileWrite(path, fileText); trackErr != nil {
			a.debugLog("Warning: Failed to track file create: %v\n", trackErr)
		}
	}

	if err == nil && a.eventBus != nil {
		a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "create", fileText))
		a.debugLog("Published file_changed event: %s (create)\n", path)
	}

	return result, err
}

func handleTodoWrite(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		return "", fmt.Errorf("missing todos argument")
	}

	// Parse the todos array
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("todos must be an array")
	}

	var todos []tools.TodoItem

	for _, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each todo must be an object")
		}

		todo := tools.TodoItem{}

		if content, ok := todoMap["content"].(string); ok {
			todo.Content = content
		}
		if status, ok := todoMap["status"].(string); ok {
			todo.Status = status
		}
		if priority, ok := todoMap["priority"].(string); ok {
			todo.Priority = priority
		}
		if id, ok := todoMap["id"].(string); ok {
			todo.ID = id
		}

		if todo.Content == "" {
			return "", fmt.Errorf("each todo requires content")
		}
		if todo.Status == "" {
			return "", fmt.Errorf("each todo requires status")
		}
		todos = append(todos, todo)
	}

	a.debugLog("TodoWrite: processing %d todos\n", len(todos))
	result := tools.TodoWrite(todos)
	a.debugLog("TodoWrite result: %s\n", result)
	return result, nil
}

func handleTodoRead(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.debugLog("TodoRead: returning current todo list\n")
	todos := tools.TodoRead()
	if len(todos) == 0 {
		return "No todos", nil
	}

	var result strings.Builder
	for _, todo := range todos {
		status := todo.Status
		if status == "in_progress" {
			status = "active"
		}
		result.WriteString(fmt.Sprintf("- [%s] %s\n", status[:1], todo.Content))
	}
	return result.String(), nil
}

func handleValidateBuild(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.debugLog("Running build validation\n")

	result, err := tools.ValidateBuild()
	a.debugLog("Build validation result: %s, error: %v\n", result, err)
	return result, err
}

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
		model = ""    // Will use system default
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

// Helper function for string truncation
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

// handleSearchFiles implements a cross-platform content search with sensible defaults and ignores
const (
	defaultSearchMaxResults = 50
	defaultSearchMaxBytes   = 20 * 1024
	defaultSearchLineLength = 240
)

func normalizePositiveInt(value any) int {
	const maxInt = int(^uint(0) >> 1)
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int8:
		if v > 0 {
			return int(v)
		}
	case int16:
		if v > 0 {
			return int(v)
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 && v <= int64(maxInt) {
			return int(v)
		}
	case uint:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint8:
		if v > 0 {
			return int(v)
		}
	case uint16:
		if v > 0 {
			return int(v)
		}
	case uint32:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint64:
		if v > 0 && v <= uint64(maxInt) {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return normalizePositiveInt(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return normalizePositiveInt(i)
		}
	}
	return 0
}

func handleSearchFiles(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	var pattern string
	if p, ok := args["search_pattern"].(string); ok {
		pattern = p
	} else if p, ok := args["pattern"].(string); ok {
		pattern = p
	} else {
		return "", fmt.Errorf("missing required parameter 'search_pattern'")
	}

	root := "."
	if v, ok := args["directory"].(string); ok && strings.TrimSpace(v) != "" {
		root = v
	}

	glob := ""
	if v, ok := args["file_glob"].(string); ok {
		glob = v
	} else if v, ok := args["file_pattern"].(string); ok {
		glob = v
	}

	caseSensitive := false
	if v, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = v
	}

	maxResults := defaultSearchMaxResults
	if v, ok := args["max_results"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxResults = normalized
		}
	}

	maxBytes := defaultSearchMaxBytes
	if v, ok := args["max_bytes"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxBytes = normalized
		}
	}

	a.ToolLog("searching files", fmt.Sprintf("pattern=%q in %s", pattern, root))
	a.debugLog("Searching files: pattern=%q, root=%s, max_results=%d\n", pattern, root, maxResults)

	// Prepare matcher: try regex first, then fallback to substring
	var re *regexp.Regexp
	var err error
	if caseSensitive {
		re, err = regexp.Compile(pattern)
	} else {
		re, err = regexp.Compile("(?i)" + pattern)
	}
	useRegex := err == nil

	// Default excluded directories
	excluded := map[string]bool{
		".git":         true,
		"node_modules": true,
		".ledit":       true,
		".venv":        true,
		"dist":         true,
		"build":        true,
		".cache":       true,
	}

	matched := 0
	var b strings.Builder
	searchCapped := false

	// Limit per-file read to avoid huge files (in bytes)
	const maxFileSize = 2 * 1024 * 1024 // 2MB

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if searchCapped {
			return io.EOF
		}
		if err != nil {
			return nil // skip on error
		}
		name := d.Name()
		if d.IsDir() {
			if excluded[name] {
				return filepath.SkipDir
			}
			// Skip hidden dirs unless explicitly included via pattern/glob (keep simple)
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(name, ".env") {
				if name != "." && name != ".." {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Glob filter
		if glob != "" {
			// Use base name for typical patterns
			if ok, _ := filepath.Match(glob, name); !ok {
				return nil
			}
		}

		// Basic binary guard by extension
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp",
			".pdf", ".zip", ".tar", ".gz", ".rar", ".7z",
			".mp3", ".wav", ".ogg", ".flac", ".aac",
			".mp4", ".avi", ".mov", ".wmv", ".mkv",
			".exe", ".dll", ".so", ".dylib", ".bin",
			".db", ".sqlite", ".ico", ".woff", ".woff2", ".ttf":
			return nil
		}

		// Open file and scan
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Size cap
		if info, err := f.Stat(); err == nil && info.Size() > maxFileSize {
			// Read only first maxFileSize bytes
			r := io.LimitReader(f, maxFileSize)
			buf := make([]byte, maxFileSize)
			n, _ := io.ReadFull(r, buf)
			buf = buf[:n]
			// naive binary check: look for NUL
			if bytesIndexByte(buf, 0) >= 0 {
				return nil
			}
			// search within this chunk by lines
			if searchBufferLines(&b, path, string(buf), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
				searchCapped = true
				return io.EOF // stop walking by returning non-nil? better: track and stop later
			}
			return nil
		}

		content, err := io.ReadAll(f)
		if err != nil {
			return nil
		}
		// binary check
		if bytesIndexByte(content, 0) >= 0 {
			return nil
		}
		if searchBufferLines(&b, path, string(content), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
			searchCapped = true
			return io.EOF
		}
		return nil
	})

	if walkErr != nil && walkErr != io.EOF {
		return "", fmt.Errorf("search failed: %v", walkErr)
	}

	if matched == 0 {
		return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, root), nil
	}
	return b.String(), nil
}

func handleWebSearch(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for web_search tool")
	}

	query := args["query"].(string)
	a.ToolLog("searching web", fmt.Sprintf("query=%q", truncateString(query, 50)))
	a.debugLog("Performing web search: %s\n", query)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for web search")
	}

	result, err := tools.WebSearch(query, a.configManager)
	a.debugLog("Web search error: %v\n", err)
	return result, err
}

func handleFetchURL(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for fetch_url tool")
	}

	url := args["url"].(string)
	a.ToolLog("fetching URL", fmt.Sprintf("url=%q", truncateString(url, 50)))
	a.debugLog("Fetching URL: %s\n", url)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for URL fetch")
	}

	result, err := tools.FetchURL(url, a.configManager)
	a.debugLog("Fetch URL error: %v\n", err)
	return result, err
}

func handleAnalyzeUIScreenshot(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_ui_screenshot tool")
	}

	imagePath := args["image_path"].(string)
	a.debugLog("Analyzing UI screenshot: %s\n", imagePath)

	result, err := tools.AnalyzeImage(imagePath, "", "frontend")
	a.debugLog("Analyze UI screenshot error: %v\n", err)
	return result, err
}

func handleAnalyzeImageContent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_image_content tool")
	}

	imagePath := args["image_path"].(string)
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}
	analysisMode := "general"
	if v, ok := args["analysis_mode"].(string); ok && strings.TrimSpace(v) != "" {
		analysisMode = v
	}

	a.debugLog("Analyzing image: %s (mode=%s)\n", imagePath, analysisMode)

	result, err := tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
	a.debugLog("Analyze image content error: %v\n", err)
	return result, err
}

func handleViewHistory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	limit := 10
	if v, ok := args["limit"].(int); ok {
		limit = v
	} else if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	fileFilter := ""
	if v, ok := args["file_filter"].(string); ok {
		fileFilter = strings.TrimSpace(v)
	}

	var sincePtr *time.Time
	sinceDisplay := ""
	if raw, ok := args["since"].(string); ok && strings.TrimSpace(raw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("invalid time format for 'since': %s. Use ISO 8601 format like '2024-01-01T10:00:00Z'", raw)
		}
		sincePtr = &parsed
		sinceDisplay = parsed.Format(time.RFC3339)
	}

	showContent := false
	if v, ok := args["show_content"].(bool); ok {
		showContent = v
	}

	logParts := []string{fmt.Sprintf("limit=%d", limit)}
	if fileFilter != "" {
		logParts = append(logParts, fmt.Sprintf("file~%s", fileFilter))
	}
	if sincePtr != nil {
		logParts = append(logParts, fmt.Sprintf("since=%s", sinceDisplay))
	}
	if showContent {
		logParts = append(logParts, "with_content")
	}

	a.debugLog("Executing view_history with limit=%d, file_filter=%q, since=%s, show_content=%v\n", limit, fileFilter, sinceDisplay, showContent)

	res, err := tools.ViewHistory(limit, fileFilter, sincePtr, showContent)
	if err != nil {
		return "", err
	}

	a.debugLog("view_history metadata: %+v\n", res.Metadata)
	return res.Output, nil
}

func handleRollbackChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	revisionID := ""
	if v, ok := args["revision_id"].(string); ok {
		revisionID = strings.TrimSpace(v)
	}

	filePath := ""
	if v, ok := args["file_path"].(string); ok {
		filePath = strings.TrimSpace(v)
	}

	confirm := false
	if v, ok := args["confirm"].(bool); ok {
		confirm = v
	}

	a.debugLog("Executing rollback_changes with revision_id=%q, file_path=%q, confirm=%v\n", revisionID, filePath, confirm)

	res, err := tools.RollbackChanges(revisionID, filePath, confirm)
	if err != nil {
		return "", err
	}

	a.debugLog("rollback_changes success=%v metadata=%+v\n", res.Success, res.Metadata)
	return res.Output, nil
}

// bytesIndexByte is a small helper to avoid importing bytes for one call
func bytesIndexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// searchBufferLines scans lines of content and appends matches; returns true if max reached
func searchBufferLines(b *strings.Builder, path, content string, re *regexp.Regexp, pattern string, caseSensitive, useRegex bool, matched *int, max int, maxBytes int) bool {
	// Normalize to forward slashes for readability
	norm := filepath.ToSlash(path)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if maxBytes > 0 && b.Len() >= maxBytes {
			return true
		}
		if *matched >= max {
			return true
		}
		ok := false
		if useRegex {
			ok = re.FindStringIndex(line) != nil
		} else {
			if caseSensitive {
				ok = strings.Contains(line, pattern)
			} else {
				ok = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
			}
		}
		if ok {
			lineOut := line
			if defaultSearchLineLength > 0 && len(lineOut) > defaultSearchLineLength {
				lineOut = truncateString(lineOut, defaultSearchLineLength)
			}
			// Format similar to grep: path:line:content
			fmt.Fprintf(b, "%s:%d:%s\n", norm, i+1, lineOut)
			*matched++
			if maxBytes > 0 && b.Len() >= maxBytes {
				return true
			}
		}
	}
	return false
}
