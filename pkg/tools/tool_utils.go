package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolCallArgumentsJSON parses tool call arguments from JSON string
func ParseToolCallArgumentsJSON(arguments string) (map[string]interface{}, error) {
	if strings.TrimSpace(arguments) == "" {
		return make(map[string]interface{}), nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	return args, nil
}
