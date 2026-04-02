package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/trace"
)

func constrainToolResultForModel(toolName string, args map[string]interface{}, result string) string {
	if toolName == "analyze_image_content" {
		return compactAnalyzeImageResultForModel(result)
	}

	if toolName != "fetch_url" {
		return result
	}

	maxChars := defaultFetchURLResultMaxChars
	if raw := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

	if len(result) <= maxChars {
		return result
	}

	headLen := maxChars * 70 / 100
	tailLen := maxChars - headLen
	if tailLen <= 0 {
		tailLen = maxChars / 2
		headLen = maxChars - tailLen
	}

	omitted := len(result) - (headLen + tailLen)
	if omitted < 0 {
		omitted = 0
	}

	archivePath, archiveErr := saveFetchURLOutputToFile(args, result)
	notice := buildFetchURLTruncationNotice(omitted, archivePath, archiveErr)
	return result[:headLen] + notice + result[len(result)-tailLen:]
}

func buildFetchURLTruncationNotice(omitted int, archivePath string, archiveErr error) string {
	if archivePath == "" {
		if archiveErr != nil {
			return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Failed to save full output: %v]\n\n", omitted, archiveErr)
		}
		return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output path unavailable.]\n\n", omitted)
	}
	return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output saved to %s]\n\n", omitted, archivePath)
}

func saveFetchURLOutputToFile(args map[string]interface{}, output string) (string, error) {
	dir := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_ARCHIVE_DIR"))
	if dir == "" {
		dir = defaultFetchURLArchiveDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("fetch_url_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	header := ""
	if args != nil {
		if rawURL, ok := args["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
			header = fmt.Sprintf("URL: %s\nFetched-At: %s\n\n", strings.TrimSpace(rawURL), time.Now().Format(time.RFC3339))
		}
	}

	fullOutput := output
	if header != "" {
		fullOutput = header + output
	}

	if err := os.WriteFile(path, []byte(fullOutput), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func compactAnalyzeImageResultForModel(result string) string {
	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return result
	}

	var b strings.Builder
	b.WriteString("analyze_image_content result:\n")
	b.WriteString(fmt.Sprintf("- success: %t\n", parsed.Success))
	if parsed.InputPath != "" {
		b.WriteString(fmt.Sprintf("- input_path: %s\n", parsed.InputPath))
	}
	if parsed.InputType != "" {
		b.WriteString(fmt.Sprintf("- input_type: %s\n", parsed.InputType))
	}
	b.WriteString(fmt.Sprintf("- ocr_attempted: %t\n", parsed.OCRAttempted))

	if parsed.ErrorCode != "" {
		b.WriteString(fmt.Sprintf("- error_code: %s\n", parsed.ErrorCode))
	}
	if parsed.ErrorMessage != "" {
		b.WriteString(fmt.Sprintf("- error_message: %s\n", parsed.ErrorMessage))
	}

	excerpt := strings.TrimSpace(parsed.ExtractedText)
	if excerpt == "" && parsed.Analysis != nil {
		excerpt = strings.TrimSpace(parsed.Analysis.Description)
	}
	if excerpt != "" {
		originalChars := parsed.OriginalChars
		if originalChars == 0 {
			originalChars = len(excerpt)
		}
		returnedChars := parsed.ReturnedChars
		if returnedChars == 0 {
			returnedChars = len(excerpt)
		}
		b.WriteString(fmt.Sprintf("- extracted_chars: %d\n", originalChars))
		b.WriteString(fmt.Sprintf("- returned_chars: %d\n", returnedChars))
	}
	if parsed.OutputTruncated {
		b.WriteString("- tool_output_truncated: true\n")
	}
	if parsed.FullOutputPath != "" {
		b.WriteString(fmt.Sprintf("- full_output_path: %s\n", parsed.FullOutputPath))
	}
	if parsed.Analysis != nil {
		if len(parsed.Analysis.Elements) > 0 {
			b.WriteString(fmt.Sprintf("- detected_elements: %d\n", len(parsed.Analysis.Elements)))
		}
		if len(parsed.Analysis.Issues) > 0 {
			b.WriteString(fmt.Sprintf("- issues: %s\n", strings.Join(parsed.Analysis.Issues, "; ")))
		}
		if len(parsed.Analysis.Suggestions) > 0 {
			b.WriteString(fmt.Sprintf("- suggestions: %s\n", strings.Join(parsed.Analysis.Suggestions, "; ")))
		}
	}
	if excerpt != "" {
		b.WriteString("- extracted_text_excerpt:\n")
		b.WriteString(limitAnalyzeImageExcerpt(excerpt, defaultAnalyzeImageResultExcerptChars))
	}

	return strings.TrimSpace(b.String())
}

func limitAnalyzeImageExcerpt(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}

	suffix := fmt.Sprintf("\n[EXCERPT TRUNCATED: kept first %d of %d chars]", maxChars, len(text))
	keep := maxChars - len(suffix)
	if keep < 0 {
		keep = maxChars
		suffix = ""
	}
	return strings.TrimSpace(text[:keep]) + suffix
}

// recordToolExecutionWithIndex records tool execution data to the trace session
func (te *ToolExecutor) recordToolExecutionWithIndex(toolName string, rawArgs string, args map[string]interface{}, fullResult, modelResult string, err error, toolIndex int) {
	if te.agent == nil || te.agent.traceSession == nil {
		return // Trace session not enabled
	}

	// Type assert to trace session interface
	type traceSessionInterface interface {
		GetRunID() string
		RecordToolCall(record interface{}) error
	}

	traceSession, ok := te.agent.traceSession.(traceSessionInterface)
	if !ok {
		te.agent.debugLog("DEBUG: traceSession is not a valid trace session, skipping tool call recording\n")
		return
	}

	// Categorize the error
	errorCategory, errorMessage := te.categorizeError(toolName, err)

	// Create normalized arguments
	argsNormalized := te.normalizeArguments(args)

	// Build ToolCallRecord
	toolCallRecord := trace.ToolCallRecord{
		RunID:          traceSession.GetRunID(),
		TurnIndex:      te.agent.currentIteration,
		ToolIndex:      toolIndex,
		ToolName:       toolName,
		Args:           args,
		ArgsNormalized: argsNormalized,
		Success:        err == nil,
		FullResult:     fullResult,
		ModelResult:    modelResult,
		ErrorCategory:  errorCategory,
		ErrorMessage:   errorMessage,
		MachineLabels:  []string{},
		Timestamp:      time.Now().Format(time.RFC3339),
	}

	// Record the tool call
	if err := traceSession.RecordToolCall(toolCallRecord); err != nil {
		te.agent.debugLog("DEBUG: Failed to record tool call: %v\n", err)
	}
}

// normalizeArguments normalizes arguments for consistent representation in traces
func (te *ToolExecutor) normalizeArguments(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return nil
	}

	normalized := make(map[string]interface{})
	for key, value := range args {
		// Stringify the key for consistency
		stringKey := fmt.Sprintf("%v", key)

		// Normalize numeric values to positive integers where applicable
		switch v := value.(type) {
		case int, int8, int16, int32, int64:
			if normalizedInt := normalizePositiveInt(v); normalizedInt > 0 {
				normalized[stringKey] = normalizedInt
			} else {
				normalized[stringKey] = v
			}
		case uint, uint8, uint16, uint32, uint64:
			if normalizedInt := normalizePositiveInt(v); normalizedInt > 0 {
				normalized[stringKey] = normalizedInt
			} else {
				normalized[stringKey] = v
			}
		case float32, float64:
			// Convert floats to int if they're whole numbers
			var floatValue float64
			if f32, ok := value.(float32); ok {
				floatValue = float64(f32)
			} else {
				floatValue = value.(float64)
			}
			if floatValue == float64(int(floatValue)) {
				if normalizedInt := normalizePositiveInt(int(floatValue)); normalizedInt > 0 {
					normalized[stringKey] = normalizedInt
				} else {
					normalized[stringKey] = int(floatValue)
				}
			} else {
				normalized[stringKey] = floatValue
			}
		default:
			normalized[stringKey] = value
		}
	}
	return normalized
}

// categorizeError categorizes errors for trace recording
func (te *ToolExecutor) categorizeError(toolName string, err error) (string, string) {
	if err == nil {
		return "", ""
	}

	errorMsg := err.Error()

	// Check for unknown tool
	if strings.Contains(errorMsg, "unknown tool") || strings.Contains(errorMsg, "tool not found") {
		return "unknown_tool", errorMsg
	}

	// Check for timeout
	if strings.Contains(errorMsg, "timed out") || strings.Contains(errorMsg, "timeout") {
		return "timeout", errorMsg
	}

	// Check for validation errors (argument parsing, schema validation)
	if strings.Contains(errorMsg, "parsing arguments") || strings.Contains(errorMsg, "invalid arguments") ||
		strings.Contains(errorMsg, "validation") || strings.Contains(errorMsg, "schema") {
		return "validation", errorMsg
	}

	// Check for circuit breaker
	if strings.Contains(errorMsg, "circuit breaker") {
		return "execution_error", errorMsg
	}

	// Default to execution error
	return "execution_error", errorMsg
}

// tryExecuteMCPTool attempts to execute an MCP tool name using the agent's MCP manager.
// Returns handled=false when the tool name doesn't correspond to an MCP tool.
func (te *ToolExecutor) tryExecuteMCPTool(toolName string, args map[string]interface{}) (string, error, bool) {
	if te.agent == nil {
		return "", fmt.Errorf("agent not initialized"), true
	}

	if strings.HasPrefix(toolName, "mcp_") {
		result, err := te.agent.executeMCPTool(toolName, args)
		return result, err, true
	}

	return "", nil, false
}

// shouldStopExecution checks if execution should stop after a tool
func (te *ToolExecutor) shouldStopExecution(toolName, result string) bool {
	// Stop on ask_user to wait for response
	if toolName == "ask_user" {
		return true
	}

	// Stop on critical errors
	if strings.Contains(result, "CRITICAL ERROR") ||
		strings.Contains(result, "FATAL ERROR") {
		return true
	}

	return false
}

// checkCircuitBreaker checks if an action should be blocked
func (te *ToolExecutor) checkCircuitBreaker(toolName string, args map[string]interface{}) bool {
	if te.agent.circuitBreaker == nil {
		return false
	}

	key := te.generateActionKey(toolName, args)

	// Copy action value outside the lock to reduce critical section hold time
	action := func() *CircuitBreakerAction {
		te.agent.circuitBreaker.mu.RLock()
		defer te.agent.circuitBreaker.mu.RUnlock()
		return te.agent.circuitBreaker.Actions[key]
	}()

	if action == nil {
		return false
	}

	// Higher threshold for troubleshooting operations
	threshold := 3

	// Increase threshold for common troubleshooting operations
	switch toolName {
	case "read_file":
		// Reading files is often repeated during troubleshooting
		threshold = 5
		// But be more aggressive for ZAI to prevent loops
		if te.agent.GetProvider() == "zai" {
			threshold = 3
		}
	case "shell_command":
		// Shell commands are frequently repeated during troubleshooting and debugging
		threshold = 8
	case "edit_file":
		// Editing the same file multiple times might be needed for complex fixes
		threshold = 4
	}

	// Block if attempted too many times
	return action.Count >= threshold
}

// updateCircuitBreaker updates the circuit breaker state
// The caller expects this function to be thread-safe with respect to the circuitBreaker map.
func (te *ToolExecutor) updateCircuitBreaker(toolName string, args map[string]interface{}) {
	if te.agent.circuitBreaker == nil {
		return
	}

	key := te.generateActionKey(toolName, args)
	te.agent.circuitBreaker.mu.Lock()
	defer te.agent.circuitBreaker.mu.Unlock()

	action, exists := te.agent.circuitBreaker.Actions[key]
	if !exists {
		action = &CircuitBreakerAction{
			ActionType: toolName,
			Target:     key,
			Count:      0,
		}
		te.agent.circuitBreaker.Actions[key] = action
	}

	action.Count++
	action.LastUsed = getCurrentTime()

	// Clean up old entries (older than 5 minutes) to prevent memory leaks
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// cleanupOldCircuitBreakerEntriesLocked removes entries older than 5 minutes
// Precondition: caller must hold te.agent.circuitBreaker.mu.Lock()
func (te *ToolExecutor) cleanupOldCircuitBreakerEntriesLocked() {
	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300 // 5 minutes in seconds

	for key, action := range te.agent.circuitBreaker.Actions {
		if action.LastUsed < fiveMinutesAgo {
			delete(te.agent.circuitBreaker.Actions, key)
		}
	}
}

// cleanupOldCircuitBreakerEntries removes entries older than 5 minutes
// This function handles locking internally and is safe to call from anywhere.
func (te *ToolExecutor) cleanupOldCircuitBreakerEntries() {
	if te.agent.circuitBreaker == nil {
		return
	}

	te.agent.circuitBreaker.mu.Lock()
	defer te.agent.circuitBreaker.mu.Unlock()
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// generateActionKey creates a unique key for an action
func (te *ToolExecutor) generateActionKey(toolName string, args map[string]interface{}) string {
	// Create a deterministic key from tool name and arguments
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("%s:%s", toolName, string(argsJSON))
}

// GenerateToolCallID creates a unique tool call ID if one is missing
func (te *ToolExecutor) GenerateToolCallID(toolName string) string {
	// Use a monotonic counter to guarantee uniqueness even under parallel execution
	te.idCounterMu.Lock()
	te.idCounter++
	seq := te.idCounter
	te.idCounterMu.Unlock()

	timestamp := getCurrentTime()
	sanitizedName := strings.ReplaceAll(toolName, "_", "")
	return fmt.Sprintf("call_%s_%d_%d", sanitizedName, timestamp, seq)
}

// getCurrentTime returns the current time (abstracted for testing)
func getCurrentTime() int64 {
	return time.Now().Unix()
}

// formatToolCall formats a tool call for display before execution
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

func (te *ToolExecutor) emitTodoChecklistUpdate(before, after []tools.TodoItem) {
	if te.agent == nil {
		return
	}

	type todoKey struct {
		ID      string
		Content string
	}
	getKey := func(t tools.TodoItem) todoKey {
		return todoKey{
			ID:      strings.TrimSpace(t.ID),
			Content: strings.TrimSpace(t.Content),
		}
	}
	statusBefore := make(map[todoKey]string, len(before))
	for _, t := range before {
		statusBefore[getKey(t)] = t.Status
	}

	var pending, inProgress, completed, cancelled int
	changed := make([]string, 0, len(after))

	for _, t := range after {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}

		key := getKey(t)
		prevStatus, existed := statusBefore[key]
		if !existed || prevStatus != t.Status {
			statusSymbol := todoStatusSymbol(t.Status)
			label := strings.TrimSpace(t.Content)
			if label == "" {
				label = "<untitled>"
			}
			if existed {
				changed = append(changed, fmt.Sprintf("%s %s (%s -> %s)", statusSymbol, label, prevStatus, t.Status))
			} else {
				changed = append(changed, fmt.Sprintf("%s %s (new)", statusSymbol, label))
			}
		}
	}

	// Publish structured todo update event for WebUI
	var todoItems []map[string]interface{}
	for _, t := range after {
		todoItems = append(todoItems, map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		})
	}
	te.agent.PublishTodoUpdate(todoItems)

	// In streaming mode, skip text output — the WebUI receives structured
	// todo_update events and does not need the inline text trace.
	if !te.agent.IsStreamingEnabled() {
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("[edit] Todo update: %d total | [ ] %d pending | [~] %d in progress | [x] %d completed | [-] %d cancelled",
			len(after), pending, inProgress, completed, cancelled))

		if len(changed) == 0 {
			te.agent.PrintLine("   No checklist changes detected.")
			te.agent.PrintLine("")
			return
		}

		maxLines := 8
		for i, line := range changed {
			if i >= maxLines {
				te.agent.PrintLine(fmt.Sprintf("   ... and %d more changes", len(changed)-maxLines))
				break
			}
			te.agent.PrintLine("   " + line)
		}
		te.agent.PrintLine("")
	}
}

func todoStatusSymbol(status string) string {
	switch status {
	case "pending":
		return "[ ]"
	case "in_progress":
		return "[~]"
	case "completed":
		return "[x]"
	case "cancelled":
		return "[-]"
	default:
		return "[?]"
	}
}
