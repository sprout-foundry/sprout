package agent

import (
	"context"
	"encoding/json"
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
			a.debugLog("[WARN] Failed to get model context limit: %v, using default\n", err)
		}
		return 32000
	}
	return limit
}

// PrintLine prints a line of text to the console content area synchronously.
// It delegates to the internal renderer that handles streaming vs CLI output.
func (a *Agent) PrintLine(text string) {
	if a == nil {
		return
	}
	a.printLineInternal(text)
}

// PrintLineAsync enqueues a line for asynchronous output. Background
// goroutines (rate-limit handlers, streaming workers, etc.) should prefer this
// helper to avoid blocking on the UI mutex. If the queue is saturated, we fall
// back to bounded waiting and finally synchronous printing to avoid goroutine
// leaks while still preserving message ordering as much as possible.
func (a *Agent) PrintLineAsync(text string) {
	if a == nil {
		return
	}

	a.ensureAsyncOutputWorker()

	select {
	case a.asyncOutput <- text:
		return
	default:
	}

	// Queue is saturated; attempt a bounded wait before falling back to
	// synchronous printing while preserving ordering. Block for a short
	// interval to give the worker a chance to drain, then emit directly in
	// the current goroutine to avoid reordering.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	select {
	case a.asyncOutput <- text:
	case <-ctx.Done():
		// Timeout reached, fall back to synchronous printing
		a.printLineInternal(text)
	}
}

func (a *Agent) ensureAsyncOutputWorker() {
	a.asyncOutputOnce.Do(func() {
		// Generous buffer to absorb bursts without blocking.
		size := a.asyncBufferSize
		if size <= 0 {
			size = asyncOutputBufferSize
		}
		a.asyncOutput = make(chan string, size)
		go func() {
			for msg := range a.asyncOutput {
				a.printLineInternal(msg)
			}
		}()
	})
}

// printLineInternal contains the core printing logic used by both synchronous
// and asynchronous pathways.
func (a *Agent) printLineInternal(text string) {
	a.printLineInternalLocked(text, true)
}

func (a *Agent) printLineInternalLocked(text string, manageLock bool) {
	message := text
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	// Route through OutputRouter (single source: publishes event + writes terminal)
	if a.outputRouter != nil {
		a.outputRouter.RouteAgentMessage("info", message, nil)
		return
	}

	// Fallback for when router isn't initialized yet
	a.PublishAgentMessage("info", message, nil)

	// Terminal output: if streamingCallback is set, route through it
	if a.streamingEnabled && a.streamingCallback != nil {
		a.streamingCallback(message)
		return
	}

	if manageLock && a.outputMutex != nil {
		a.outputMutex.Lock()
		defer a.outputMutex.Unlock()
	}

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
		"exec":                        "shell_command",
		"bash":                        "shell_command",
		"cmd":                         "shell_command",
		"command":                     "shell_command",
		"run":                         "shell_command",
		"run_command":                 "shell_command",
		"execute":                     "shell_command",
		"read":                        "read_file",
		"cat":                         "read_file",
		"open":                        "read_file",
		"open_file":                   "read_file",
		"write":                       "write_file",
		"save":                        "write_file",
		"create":                      "write_file",
		"edit":                        "edit_file",
		"modify":                      "edit_file",
		"change":                      "edit_file",
		"replace":                     "edit_file",
		"str_replace":                 "edit_file",
		"str_replace_based_edit_tool": "edit_file",
		"write_json":                  "write_structured_file",
		"write_yaml":                  "write_structured_file",
		"write_filejson":              "write_structured_file",
		"structured_write":            "write_structured_file",
		"json_patch":                  "patch_structured_file",
		"patch_json":                  "patch_structured_file",
		"todo":                        "TodoWrite",
		"task":                        "TodoWrite",
		"todos":                       "TodoWrite",
		"tasks":                       "TodoWrite",
		"TodoRead":                    "TodoRead",
		"list_todos":                  "TodoRead",
		"search":                      "search_files",
		"find":                        "search_files",
		"grep":                        "search_files",
		"list":                        "TodoRead",
		"show":                        "TodoRead",
		"web":                         "web_search",
		"google":                      "web_search",
		"bing":                        "web_search",
		"search_web":                  "web_search",
		"fetch":                       "fetch_url",
		"download":                    "fetch_url",
		"url":                         "fetch_url",
		"fetch_urljson":               "fetch_url",
		"screenshot":                  "analyze_ui_screenshot",
		"ui":                          "analyze_ui_screenshot",
		"image":                       "analyze_image_content",
		"vision":                      "analyze_image_content",
		"history":                     "view_history",
		"logs":                        "view_history",
		"rollback":                    "rollback_changes",
		"revert":                      "rollback_changes",
	}

	if suggestion, exists := corrections[strings.ToLower(invalidName)]; exists {
		return suggestion
	}

	return ""
}

// LogToolCall appends a JSON line describing a tool call to a local file for quick debugging.
// File: ./tool_calls.log (in the current working directory)
func (a *Agent) LogToolCall(tc api.ToolCall, phase string) {
	// Only log when explicitly enabled or in debug mode
	if os.Getenv("LEDIT_LOG_TOOL_CALLS") == "" && !a.debug {
		return
	}
	entry := map[string]interface{}{
		"ts":        time.Now().Format(time.RFC3339Nano),
		"provider":  a.GetProvider(),
		"model":     a.GetModel(),
		"phase":     phase, // e.g., "received", "executing"
		"id":        tc.ID,
		"type":      tc.Type,
		"name":      tc.Function.Name,
		"arguments": tc.Function.Arguments,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}

	f, err := os.OpenFile("tool_calls.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	// best-effort write; ignore errors on close
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// ToolLog formats and prints a tool call message immediately for user visibility.
// Routes through OutputRouter for single-sourced event+terminal output.
// Format: [4 - 30%] read file filename.go
func (a *Agent) ToolLog(action string, target string) {
	if a == nil {
		return
	}

	// Route through OutputRouter (single source: publishes event + writes terminal)
	if a.outputRouter != nil {
		a.outputRouter.RouteToolLog(action, target)
		return
	}

	// Fallback for when router isn't initialized yet
	message := fmt.Sprintf("%s %s", action, target)
	a.PublishAgentMessage("tool_log", message, nil)
	fmt.Print(message + "\n")
}
