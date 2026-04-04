// Trace recording: captures tool execution data into the trace session
// for observability, replay, and post-hoc analysis.
package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/trace"
)

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
