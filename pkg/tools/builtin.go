package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/ui"
)

// executeBuiltinTool handles built-in tools for backward compatibility
func (e *Executor) executeBuiltinTool(ctx context.Context, toolName string, args map[string]interface{}) (*Result, error) {
	switch toolName {
	case "read_file":
		return e.executeReadFile(ctx, args)
	case "ask_user":
		return e.executeAskUser(ctx, args)
	case "run_shell_command":
		return e.executeShellCommand(ctx, args)
	case "workspace_context":
		return e.executeWorkspaceContext(ctx, args)
	case "search_web":
		return e.executeWebSearch(ctx, args)
	case "delete_file":
		return e.executeDeleteFile(ctx, args)
	case "replace_file_content":
		return e.executeReplaceFileContent(ctx, args)
	case "edit_file_section":
		return e.executeEditFileSection(ctx, args)
	case "validate_file":
		return e.executeValidateFile(ctx, args)
	default:
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("unknown built-in tool: %s", toolName)},
		}, nil
	}
}

func (e *Executor) executeReadFile(ctx context.Context, args map[string]interface{}) (*Result, error) {
	filePath, ok := args["target_file"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"read_file requires 'target_file' parameter"},
		}, nil
	}

	content, err := filesystem.ReadFile(filePath)
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to read file %s: %v", filePath, err)},
		}, nil
	}

	return &Result{
		Success: true,
		Output:  string(content),
		Metadata: map[string]interface{}{
			"target_file": filePath,
			"file_size":   len(content),
		},
	}, nil
}

func (e *Executor) executeDeleteFile(ctx context.Context, args map[string]interface{}) (*Result, error) {
	filePath, ok := args["target_file"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"delete_file requires 'target_file' parameter"},
		}, nil
	}

	err := os.Remove(filePath)
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to delete file: %v", err)},
		}, nil
	}

	return &Result{
		Success: true,
		Output:  fmt.Sprintf("File '%s' deleted successfully", filePath),
	}, nil
}

func (e *Executor) executeReplaceFileContent(ctx context.Context, args map[string]interface{}) (*Result, error) {
	filePath, ok := args["target_file"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"replace_file_content requires 'target_file' parameter"},
		}, nil
	}

	newContent, ok := args["new_content"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"replace_file_content requires 'new_content' parameter"},
		}, nil
	}

	err := filesystem.WriteFile(filePath, []byte(newContent))
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to write file: %v", err)},
		}, nil
	}

	return &Result{
		Success: true,
		Output:  fmt.Sprintf("File '%s' replaced successfully", filePath),
	}, nil
}

func (e *Executor) executeAskUser(ctx context.Context, args map[string]interface{}) (*Result, error) {
	question, ok := args["question"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"ask_user requires 'question' parameter"},
		}, nil
	}

	if e.config.SkipPrompt {
		return &Result{
			Success: true,
			Output:  "User interaction skipped in non-interactive mode",
			Metadata: map[string]interface{}{
				"skipped": true,
			},
		}, nil
	}

	ui.Out().Printf("\n🤖 Question: %s\n", question)
	ui.Out().Print("Your answer: ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to read user input: %v", err)},
		}, nil
	}

	answer = strings.TrimSpace(answer)
	return &Result{
		Success: true,
		Output:  answer,
		Metadata: map[string]interface{}{
			"question": question,
		},
	}, nil
}

func (e *Executor) executeShellCommand(ctx context.Context, args map[string]interface{}) (*Result, error) {
	command, ok := args["command"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"run_shell_command requires 'command' parameter"},
		}, nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &Result{
			Success: false,
			Output:  string(output),
			Errors:  []string{fmt.Sprintf("command failed: %v", err)},
			Metadata: map[string]interface{}{
				"command":   command,
				"exit_code": cmd.ProcessState.ExitCode(),
			},
		}, nil
	}

	return &Result{
		Success: true,
		Output:  string(output),
		Metadata: map[string]interface{}{
			"command":   command,
			"exit_code": 0,
		},
	}, nil
}

func (e *Executor) executeWorkspaceContext(ctx context.Context, args map[string]interface{}) (*Result, error) {
	action, _ := args["action"].(string)

	// Default to overview if no action specified
	if strings.TrimSpace(action) == "" {
		action = "overview"
	}

	switch strings.ToLower(action) {
	case "overview":
		output := e.buildWorkspaceOverview()
		return &Result{
			Success:  true,
			Output:   output,
			Metadata: map[string]interface{}{"action": "overview"},
		}, nil

	case "search":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return &Result{
				Success: false,
				Errors:  []string{"workspace_context search requires 'query' parameter"},
			}, nil
		}
		output := e.searchWorkspaceFiles(query)
		return &Result{
			Success:  true,
			Output:   output,
			Metadata: map[string]interface{}{"action": "search", "query": query},
		}, nil

	default:
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("unknown workspace_context action: %s. Use 'overview' or 'search'", action)},
		}, nil
	}
}

func (e *Executor) executeWebSearch(ctx context.Context, args map[string]interface{}) (*Result, error) {
	query, ok := args["query"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"search_web requires 'query' parameter"},
		}, nil
	}

	// For now, return a placeholder result
	// TODO: Implement proper web search functionality
	result := fmt.Sprintf("Web search results for: %s", query)

	return &Result{
		Success: true,
		Output:  result,
		Metadata: map[string]interface{}{
			"query":        query,
			"result_count": 0,
		},
	}, nil
}

func (e *Executor) executeEditFileSection(ctx context.Context, args map[string]interface{}) (*Result, error) {
	filePath, ok := args["target_file"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"edit_file_section requires 'target_file' parameter"},
		}, nil
	}

	oldText, hasOld := args["old_text"].(string)
	newText, hasNew := args["new_text"].(string)

	if !hasOld || !hasNew {
		return &Result{
			Success: false,
			Errors:  []string{"edit_file_section requires 'old_text' and 'new_text' parameters"},
		}, nil
	}

	// Read current file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to read file %s: %v", filePath, err)},
		}, nil
	}

	// Replace the text
	originalContent := string(content)
	if !strings.Contains(originalContent, oldText) {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("old_text not found in file %s", filePath)},
		}, nil
	}

	newContent := strings.Replace(originalContent, oldText, newText, 1)

	// Write the modified content back
	err = os.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to write file %s: %v", filePath, err)},
		}, nil
	}

	return &Result{
		Success: true,
		Output:  fmt.Sprintf("Successfully edited file %s", filePath),
		Metadata: map[string]interface{}{
			"target_file":     filePath,
			"old_text_length": len(oldText),
			"new_text_length": len(newText),
			"content_changed": len(newContent) != len(originalContent),
		},
	}, nil
}

func (e *Executor) executeValidateFile(ctx context.Context, args map[string]interface{}) (*Result, error) {
	target, ok := args["target_file"].(string)
	if !ok || strings.TrimSpace(target) == "" {
		return &Result{Success: false, Errors: []string{"validate_file requires 'target_file'"}}, nil
	}
	vtype, _ := args["validation_type"].(string)
	if strings.TrimSpace(vtype) == "" {
		vtype = "basic"
	}

	// Decide validation commands based on file type
	cmds := []string{}
	if strings.HasSuffix(strings.ToLower(target), ".go") {
		// syntax check via gofmt
		cmds = append(cmds, fmt.Sprintf("gofmt -e -l %s", target))
		// vet the file (package-level); may require package path
		cmds = append(cmds, fmt.Sprintf("go vet %s", target))
		// attempt to build the module/package to catch compile errors
		cmds = append(cmds, "go build")
	} else {
		// Generic: just check file exists and is readable
		if _, err := os.Stat(target); err != nil {
			return &Result{Success: false, Errors: []string{fmt.Sprintf("file not accessible: %v", err)}}, nil
		}
		return &Result{Success: true, Output: fmt.Sprintf("Validated %s (non-Go file): exists and readable", target)}, nil
	}

	var outputBuilder strings.Builder
	allOK := true
	for _, c := range cmds {
		select {
		case <-ctx.Done():
			return &Result{Success: false, Errors: []string{"validation cancelled or timed out"}}, nil
		default:
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", c)
		out, err := cmd.CombinedOutput()
		outputBuilder.WriteString("$ ")
		outputBuilder.WriteString(c)
		outputBuilder.WriteString("\n")
		if len(out) > 0 {
			outputBuilder.Write(out)
			if out[len(out)-1] != '\n' {
				outputBuilder.WriteString("\n")
			}
		}
		if err != nil {
			allOK = false
			outputBuilder.WriteString(fmt.Sprintf("(error: %v)\n", err))
		}
	}

	return &Result{
		Success: allOK,
		Output:  outputBuilder.String(),
		Metadata: map[string]interface{}{
			"target_file":     target,
			"validation_type": vtype,
			"passed":          allOK,
		},
	}, nil
}

// ParseToolCallArguments parses tool call arguments from JSON string
func ParseToolCallArguments(arguments string) (map[string]interface{}, error) {
	if strings.TrimSpace(arguments) == "" {
		return make(map[string]interface{}), nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	return args, nil
}

// buildWorkspaceOverview creates a structured overview of the workspace
func (e *Executor) buildWorkspaceOverview() string {
	var builder strings.Builder
	builder.WriteString("=== Workspace Overview ===\n")

	// Count files by directory and type
	dirCounts := make(map[string]int)
	extCounts := make(map[string]int)
	totalFiles := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip common ignore patterns
		if e.shouldSkipFile(path) {
			return nil
		}

		totalFiles++
		dir := filepath.Dir(path)
		if dir == "." {
			dir = "root"
		}
		dirCounts[dir]++

		ext := filepath.Ext(path)
		if ext == "" {
			ext = "no-extension"
		}
		extCounts[ext]++

		return nil
	})

	if err != nil {
		builder.WriteString("Error scanning workspace: " + err.Error() + "\n")
		return builder.String()
	}

	builder.WriteString(fmt.Sprintf("Total files: %d\n\n", totalFiles))

	// Show top directories
	builder.WriteString("Files by directory:\n")
	for dir, count := range dirCounts {
		if count > 0 {
			builder.WriteString(fmt.Sprintf("  %s: %d files\n", dir, count))
		}
	}

	// Show file types
	builder.WriteString("\nFiles by type:\n")
	for ext, count := range extCounts {
		if count > 0 {
			builder.WriteString(fmt.Sprintf("  %s: %d files\n", ext, count))
		}
	}

	return builder.String()
}

// searchWorkspaceFiles searches for a query across workspace files
func (e *Executor) searchWorkspaceFiles(query string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("=== Search Results for '%s' ===\n", query))

	matchCount := 0
	queryLower := strings.ToLower(query)

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if e.shouldSkipFile(path) {
			return nil
		}

		// Limit file size to avoid huge files
		if info.Size() > 1024*1024 { // 1MB limit
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		contentStr := string(content)
		if !strings.Contains(strings.ToLower(contentStr), queryLower) {
			return nil
		}

		matchCount++
		builder.WriteString(fmt.Sprintf("\n--- %s ---\n", path))

		// Show matching lines with context
		lines := strings.Split(contentStr, "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), queryLower) {
				lineNum := i + 1
				builder.WriteString(fmt.Sprintf("%d: %s\n", lineNum, strings.TrimSpace(line)))
			}
		}

		// Limit results to avoid overwhelming output
		if matchCount >= 10 {
			return fmt.Errorf("stopping search - max results reached")
		}

		return nil
	})

	if err != nil && !strings.Contains(err.Error(), "stopping search") {
		builder.WriteString("Error during search: " + err.Error() + "\n")
	}

	if matchCount == 0 {
		builder.WriteString("No matches found.\n")
	} else {
		builder.WriteString(fmt.Sprintf("\nFound %d matching files.\n", matchCount))
	}

	return builder.String()
}

// shouldSkipFile determines if a file should be skipped during workspace operations
func (e *Executor) shouldSkipFile(path string) bool {
	// Skip hidden files and directories
	if strings.Contains(path, "/.") {
		return true
	}

	// Skip common build/cache directories
	skipDirs := []string{
		"node_modules", ".git", "vendor", "target", "build",
		"dist", "__pycache__", ".vscode", ".idea",
	}

	for _, skipDir := range skipDirs {
		if strings.Contains(path, skipDir) {
			return true
		}
	}

	// Skip binary/media files
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := []string{
		".exe", ".bin", ".so", ".dylib", ".dll", ".class",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg",
		".mp4", ".avi", ".mov", ".mp3", ".wav", ".pdf",
		".zip", ".tar", ".gz", ".rar",
	}

	for _, binExt := range binaryExts {
		if ext == binExt {
			return true
		}
	}

	return false
}
