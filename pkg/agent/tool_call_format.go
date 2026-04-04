// Tool call formatting: display-friendly representations of tool calls
// for logging, progress output, and CLI status reporting.
package agent

import (
	"fmt"
	"log"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// Maximum display length for tool call arguments before truncation
const maxToolArgDisplayLength = 50

// formatTruncateString truncates a string to the maximum display length and adds ellipsis if needed,
// then wraps it in quotes for unambiguous display
func formatTruncateString(s string) string {
	if len(s) > maxToolArgDisplayLength {
		s = s[:maxToolArgDisplayLength-3] + "..."
	}
	return fmt.Sprintf("%q", s)
}

func formatToolCall(toolCall api.ToolCall) string {
	// Format: [tool_name]
	// Example: [read_file] "path/to/file.go"
	args, _, err := parseToolArgumentsWithRepair(toolCall.Function.Arguments)
	if err != nil {
		log.Printf("Warning: Failed to parse tool arguments for tool '%s': %v", toolCall.Function.Name, err)
		return fmt.Sprintf("[%s]", toolCall.Function.Name)
	}

	// Extract meaningful arguments for display
	var parts []string
	parts = append(parts, toolCall.Function.Name)

	// Add common parameters consistently with quoting.
	if path, ok := args["path"].(string); ok && path != "" {
		parts = append(parts, formatTruncateString(path))
	} else if filePath, ok := args["file_path"].(string); ok && filePath != "" {
		parts = append(parts, formatTruncateString(filePath))
	}
	if url, ok := args["url"].(string); ok && url != "" {
		parts = append(parts, formatTruncateString(url))
	}
	if imagePath, ok := args["image_path"].(string); ok && imagePath != "" {
		parts = append(parts, formatTruncateString(imagePath))
	}
	if query, ok := args["query"].(string); ok && query != "" {
		parts = append(parts, formatTruncateString(query))
	}
	if command, ok := args["command"].(string); ok && command != "" {
		parts = append(parts, formatTruncateString(command))
	}
	if operation, ok := args["operation"].(string); ok && operation != "" {
		parts = append(parts, formatTruncateString(operation))
	}
	if content, ok := args["content"].(string); ok && len(content) > 0 {
		parts = append(parts, fmt.Sprintf("(%d bytes)", len(content)))
	}
	if pattern, ok := args["pattern"].(string); ok && pattern != "" {
		parts = append(parts, formatTruncateString(pattern))
	}
	if todoSummary := summarizeTodoWriteArgs(args); todoSummary != "" {
		parts = append(parts, todoSummary)
	}

	result := fmt.Sprintf("[%s]", strings.Join(parts, " "))
	return result
}

func summarizeTodoWriteArgs(args map[string]interface{}) string {
	todosRaw, ok := args["todos"].([]interface{})
	if !ok || len(todosRaw) == 0 {
		return ""
	}

	var pending, inProgress, completed, cancelled int
	for _, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			continue
		}
		status, _ := todoMap["status"].(string)
		switch status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}

	return fmt.Sprintf("todos=%d [ ]=%d [~]=%d [x]=%d [-]=%d", len(todosRaw), pending, inProgress, completed, cancelled)
}
