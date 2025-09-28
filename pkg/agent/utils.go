package agent

import (
    "fmt"
    "os"
    "strings"
    "time"

    api "github.com/alantheprice/ledit/pkg/agent_api"
)

// debugLog logs a message only if debug mode is enabled
func (a *Agent) debugLog(format string, args ...interface{}) {
    if !a.debug {
        return
    }
    msg := fmt.Sprintf(format, args...)
    // Prefer writing to debug log file if available
    if a.debugLogFile != nil {
        a.debugLogMutex.Lock()
        defer a.debugLogMutex.Unlock()
        timestamp := time.Now().Format("15:04:05.000")
        _, _ = a.debugLogFile.WriteString(fmt.Sprintf("[%s] %s", timestamp, msg))
        return
    }
    // Fallback to stderr
    fmt.Fprint(os.Stderr, msg)
}

// getModelContextLimit returns the maximum context window for a model from the API
func (a *Agent) getModelContextLimit() int {
	limit, err := a.client.GetModelContextLimit()
	if err != nil {
		// Fallback to conservative default if API method fails
		if a.debug {
			a.debugLog("⚠️  Failed to get model context limit: %v, using default\n", err)
		}
		return 32000
	}
	return limit
}

// ToolLog logs tool execution messages that are always visible with blue formatting
func (a *Agent) ToolLog(action, target string) {
	// Use muted gray colors for tool call logging - both very close in darkness
	const darkGray = "\033[90m"                  // Bright black (darker gray) for tool call info
	const slightlyLighterGray = "\033[38;5;246m" // Slightly lighter gray for the executed portion
	const reset = "\033[0m"

	// Format: [4] read file filename.go
	iterInfo := fmt.Sprintf("[%d]", a.currentIteration)

	var message string
	if target != "" {
		// Add newline before tool call to put it on its own line
		// Use darker gray for tool call info and slightly lighter gray for target
		message = fmt.Sprintf("\n%s%s %s%s %s%s%s\n", darkGray, iterInfo, action, reset, slightlyLighterGray, target, reset)
	} else {
		// Add newline before tool call to put it on its own line
		message = fmt.Sprintf("\n%s%s %s%s\n", darkGray, iterInfo, action, reset)
	}

	// Route through streaming callback if streaming is enabled
	if a.streamingEnabled && a.streamingCallback != nil {
		// Send through streaming callback to maintain proper order with narrative text
		a.streamingCallback(message)
	} else {
		// Fallback to direct output for non-streaming mode
		if a.outputMutex != nil {
			a.outputMutex.Lock()
			defer a.outputMutex.Unlock()
		}

		// In CI mode, don't use cursor control sequences
		if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
			fmt.Print(message)
		} else {
			// Clear current line and move to start for interactive terminals
			fmt.Print("\r\033[K")
			fmt.Print(message)
		}
	}
}

// PrintLine prints a line of text to the console content area.
// In interactive streaming mode it routes through the streaming callback so
// the AgentConsole renders it in the content region. Otherwise, it falls back
// to direct stdout while ensuring the current terminal line is cleared.
func (a *Agent) PrintLine(text string) {
    message := text
    if !strings.HasSuffix(message, "\n") {
        message += "\n"
    }

    if a.streamingEnabled && a.streamingCallback != nil {
        a.streamingCallback(message)
        return
    }

    if a.outputMutex != nil {
        a.outputMutex.Lock()
        defer a.outputMutex.Unlock()
    }

    // In CI, avoid cursor control sequences
    if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
        fmt.Print(message)
        return
    }

    // Clear current line, then print the message
    fmt.Print("\r\033[K")
    fmt.Print(message)
}

// estimateContextTokens estimates the token count for messages
func (a *Agent) estimateContextTokens(messages []api.Message) int {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
		totalChars += len(msg.ReasoningContent)
	}
	// Rough estimate: 4 chars per token (conservative)
	return totalChars / 4
}

// formatTokenCount formats token count with thousands/millions separators
func (a *Agent) formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	} else if tokens < 1000000 {
		// Convert to thousands format with one decimal place
		thousands := float64(tokens) / 1000.0
		return fmt.Sprintf("%.1fK", thousands)
	} else {
		// Convert to millions format with two decimal places
		millions := float64(tokens) / 1000000.0
		return fmt.Sprintf("%.2fM", millions)
	}
}

// suggestCorrectToolName suggests the correct tool name for common mistakes
func (a *Agent) suggestCorrectToolName(invalidName string) string {
	// Common tool name mappings
	corrections := map[string]string{
		"exec":    "shell_command",
		"bash":    "shell_command",
		"cmd":     "shell_command",
		"command": "shell_command",
		"run":     "shell_command",
		"execute": "shell_command",
		"read":    "read_file",
		"cat":     "read_file",
		"open":    "read_file",
		"write":   "write_file",
		"save":    "write_file",
		"create":  "write_file",
		"edit":    "edit_file",
		"modify":  "edit_file",
		"change":  "edit_file",
		"replace": "edit_file",
		"todo":    "add_todo",
		"task":    "add_todo",
		"update":  "update_todo_status",
		"status":  "update_todo_status",
		"list":    "list_todos",
		"show":    "list_todos",
	}

	if suggestion, exists := corrections[strings.ToLower(invalidName)]; exists {
		return suggestion
	}

	return ""
}
