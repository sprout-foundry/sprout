package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
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
	case "edit_file_section":
		return e.executeEditFileSection(ctx, args)
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
	query, _ := args["query"].(string)

	if strings.TrimSpace(action) == "" {
		action = "load_tree"
	}

	// Load workspace file from .ledit/workspace.json
	ws, err := loadWorkspaceInfo()
	if err != nil {
		return &Result{Success: false, Errors: []string{fmt.Sprintf("failed to load workspace: %v", err)}}, nil
	}

	switch strings.ToLower(action) {
	case "load_tree":
		out := buildCompactTree(ws)
		return &Result{Success: true, Output: out, Metadata: map[string]interface{}{"action": action}}, nil
	case "search_keywords":
		if strings.TrimSpace(query) == "" {
			return &Result{Success: false, Errors: []string{"search_keywords requires non-empty 'query'"}}, nil
		}
		out := searchWorkspaceKeywords(ws, query, 100, 3)
		return &Result{Success: true, Output: out, Metadata: map[string]interface{}{"action": action, "query": query}}, nil
	case "load_summary":
		out := buildWorkspaceOverview(ws)
		return &Result{Success: true, Output: out, Metadata: map[string]interface{}{"action": action}}, nil
	default:
		return &Result{Success: false, Errors: []string{fmt.Sprintf("unknown workspace_context action: %s", action)}}, nil
	}
}

// loadWorkspaceInfo reads .ledit/workspace.json into a minimal structure
func loadWorkspaceInfo() (workspaceinfo.WorkspaceFile, error) {
	var ws workspaceinfo.WorkspaceFile
	data, err := os.ReadFile(".ledit/workspace.json")
	if err != nil {
		return ws, err
	}
	if err := json.Unmarshal(data, &ws); err != nil {
		return ws, err
	}
	return ws, nil
}

// buildCompactTree prints a compact tree using file paths from workspace info
func buildCompactTree(ws workspaceinfo.WorkspaceFile) string {
	var b strings.Builder
	b.WriteString("--- Workspace Tree ---\n")
	// Group by top-level directory
	groups := map[string][]string{}
	for path := range ws.Files {
		parts := strings.Split(path, "/")
		key := "root"
		if len(parts) > 1 {
			key = parts[0]
		}
		groups[key] = append(groups[key], path)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		files := groups[k]
		sort.Strings(files)
		b.WriteString(fmt.Sprintf("%s/ (%d files):\n", k, len(files)))
		limit := len(files)
		if limit > 50 {
			limit = 50
		}
		for i := 0; i < limit; i++ {
			b.WriteString("  ")
			b.WriteString(files[i])
			b.WriteString("\n")
		}
		if len(files) > limit {
			b.WriteString("  ...\n")
		}
	}
	return b.String()
}

// searchWorkspaceKeywords performs a simple substring search across files, returning file paths and snippets
func searchWorkspaceKeywords(ws workspaceinfo.WorkspaceFile, query string, maxFiles int, maxSnippetsPerFile int) string {
	q := strings.ToLower(query)
	var b strings.Builder
	matches := 0
	b.WriteString(fmt.Sprintf("Search '%s' results:\n", query))
	for path := range ws.Files {
		if matches >= maxFiles {
			break
		}
		// Read small files directly; skip very large files
		info, err := os.Stat(path)
		if err != nil || (info != nil && info.Size() > 2*1024*1024) { // skip >2MB
			continue
		}
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(contentBytes)
		if !strings.Contains(strings.ToLower(content), q) {
			continue
		}
		b.WriteString("- ")
		b.WriteString(path)
		snips := 0
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), q) {
				b.WriteString("\n  ")
				b.WriteString(strings.TrimSpace(line))
				snips++
				if snips >= maxSnippetsPerFile {
					break
				}
			}
		}
		b.WriteString("\n")
		matches++
	}
	if matches == 0 {
		b.WriteString("(no matches)\n")
	}
	return b.String()
}

// buildWorkspaceOverview produces a compact overview using workspaceinfo content
func buildWorkspaceOverview(ws workspaceinfo.WorkspaceFile) string {
	var b strings.Builder
	b.WriteString("--- Workspace Overview ---\n")
	b.WriteString(fmt.Sprintf("Total files indexed: %d\n", len(ws.Files)))
	// List top directories
	dirs := map[string]int{}
	for path := range ws.Files {
		parts := strings.Split(path, "/")
		key := "root"
		if len(parts) > 1 {
			key = parts[0]
		}
		dirs[key]++
	}
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("%s: %d files\n", k, dirs[k]))
	}
	return b.String()
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
