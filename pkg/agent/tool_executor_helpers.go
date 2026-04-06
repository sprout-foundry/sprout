// Tool executor helpers: small utility functions that support the
// tool execution lifecycle (MCP delegation, stop conditions, ID generation,
// numeric normalization).
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// tryExecuteMCPTool attempts to execute an MCP tool name using the agent's MCP manager.
// Returns handled=false when the tool name doesn't correspond to an MCP tool.
func (te *ToolExecutor) tryExecuteMCPTool(toolName string, args map[string]interface{}) (string, error, bool) {
	if te.agent == nil {
		return "", errors.New("agent not initialized"), true
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

// normalizePositiveInt normalizes various numeric types to a positive int.
// Used by both search result normalization and trace argument normalization.
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
