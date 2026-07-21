// Tool execution helpers shared across the agent package.
//
// Free-function survivors from the deleted pkg/agent/tool_executor_helpers.go.
// Only these two remained in live use after ToolExecutor was replaced by
// seed's core.ToolRegistry:
//
//   - getCurrentTime: security_circuit_breaker.go updates
//     action.LastUsed with the current Unix timestamp.
//
//   - normalizePositiveInt: tool_handlers_search.go normalises numeric
//     LLM-supplied search arguments (top_k, etc.) that may arrive as
//     int, float, json.Number, or string.
//
// Kept package-private since the callers are also in this package.
package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// getCurrentTime returns the current Unix timestamp (seconds). Abstracted
// so tests can override via getCurrentTime = func() int64 { ... }.
func getCurrentTime() int64 {
	return time.Now().Unix()
}

// normalizePositiveInt normalises various numeric types to a positive int.
// Returns 0 for any non-positive or unparseable value. Used by both
// search-result argument normalization and circuit-breaker argument hashing.
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
