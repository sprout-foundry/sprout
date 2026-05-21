package tools

import (
	"fmt"
)

// extractString extracts a string value from args map.
func extractString(args map[string]any, key string) (string, error) {
	val, exists := args[key]
	if !exists || val == nil {
		return "", fmt.Errorf("parameter '%s' is required", key)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter '%s' must be a string, got %T", key, val)
	}
	return s, nil
}

// extractInt extracts an integer value from args map.
// Returns 0, nil if the key is missing (for optional parameters).
func extractInt(args map[string]any, key string) (int, error) {
	val, exists := args[key]
	if !exists || val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("parameter '%s' must be an integer, got %T", key, val)
	}
}

// getBoolArg extracts a boolean value from args map, returning a zero value if not set.
func getBoolArg(args map[string]any, key string) bool {
	val, exists := args[key]
	if !exists || val == nil {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	if s, ok := val.(string); ok {
		return s == "true" || s == "1" || s == "yes"
	}
	if f, ok := val.(float64); ok {
		return f == 1
	}
	if i, ok := val.(int); ok {
		return i == 1
	}
	if i, ok := val.(int64); ok {
		return i == 1
	}
	return false
}

// estimateTokenUsage provides a rough token count based on character length.
// Uses 4 chars/token as a heuristic.
func estimateTokenUsage(s string) int {
	return len(s) / 4
}
