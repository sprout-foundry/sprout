package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
	ui "github.com/alantheprice/ledit/pkg/ui"
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
	filePath, ok := args["file_path"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"read_file requires 'file_path' parameter"},
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
			"file_path": filePath,
			"file_size": len(content),
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

	// For now, return a placeholder result
	// TODO: Implement proper workspace context functionality
	result := fmt.Sprintf("Workspace context action: %s, query: %s", action, query)

	return &Result{
		Success: true,
		Output:  result,
		Metadata: map[string]interface{}{
			"action": action,
			"query":  query,
		},
	}, nil
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
	filePath, ok := args["file_path"].(string)
	if !ok {
		return &Result{
			Success: false,
			Errors:  []string{"edit_file_section requires 'file_path' parameter"},
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
			"file_path":       filePath,
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
