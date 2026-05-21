package tools

import "fmt"

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

// estimateTokenUsage provides a rough token count based on character length.
// Uses 4 chars/token as a heuristic.
func estimateTokenUsage(s string) int {
	return len(s) / 4
}
