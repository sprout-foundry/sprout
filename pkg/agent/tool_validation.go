package agent

import (
	"encoding/json"
	"fmt"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// validateParameters validates and extracts parameters according to tool configuration
func (r *ToolRegistry) validateParameters(tool ToolConfig, args map[string]interface{}, agent *Agent) (map[string]interface{}, error) {
	validated := make(map[string]interface{})

	for _, param := range tool.Parameters {
		value, found := r.extractParameter(param, args)

		if !found && param.Required {
			return nil, agenterrors.NewValidation(fmt.Sprintf("required parameter '%s' missing", param.Name), nil)
		}

		if found {
			// Type validation and conversion
			convertedValue, err := r.convertParameterType(value, param.Type, agent)
			if err != nil {
				return nil, agenterrors.Wrap(err, fmt.Sprintf("parameter '%s'", param.Name))
			}
			validated[param.Name] = convertedValue
		}
	}

	return validated, nil
}

// extractParameter extracts a parameter value, checking alternatives for backward compatibility
func (r *ToolRegistry) extractParameter(param ParameterConfig, args map[string]interface{}) (interface{}, bool) {
	// Try primary name first
	if value, exists := args[param.Name]; exists {
		return value, true
	}

	// Try alternative names for backward compatibility
	for _, alt := range param.Alternatives {
		if value, exists := args[alt]; exists {
			return value, true
		}
	}

	return nil, false
}

// getMapKeys returns all keys from a map as a slice
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapToJSONString converts a map to a pretty-printed JSON string
func (r *ToolRegistry) mapToJSONString(m map[string]interface{}) (string, error) {
	jsonBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to marshal map to JSON")
	}
	return string(jsonBytes), nil
}

// convertParameterType converts a parameter to the expected type
func (r *ToolRegistry) convertParameterType(value interface{}, expectedType string, agent *Agent) (interface{}, error) {
	switch expectedType {
	case "string":
		if str, ok := value.(string); ok {
			return str, nil
		}

		// Handle case where content is passed as a map instead of string
		if mapVal, ok := value.(map[string]interface{}); ok {
			// Try to convert the map to JSON string
			jsonStr, err := r.mapToJSONString(mapVal)
			if err != nil {
				if agent != nil && agent.debug {
					agent.debugLog("Expected string, got map[string]interface {}. Failed to convert to JSON: %v\n", err)
					agent.debugLog("Content as map keys: %v\n", getMapKeys(mapVal))
				}
				return "", agenterrors.Wrap(err, fmt.Sprintf("expected string, got %T (failed to convert map to JSON)", value))
			}

			if agent != nil && agent.debug {
				agent.debugLog("Converted map to JSON string. Length: %d\n", len(jsonStr))
			}
			return jsonStr, nil
		}

		// Debug logging for other type conversion failures
		if agent != nil && agent.debug {
			agent.debugLog("Expected string, got %T. Value: %+v\n", value, value)
		}

		return "", agenterrors.NewValidation(fmt.Sprintf("expected string, got %T", value), nil)

	case "int", "integer":
		if i, ok := value.(int); ok {
			return i, nil
		}
		if f, ok := value.(float64); ok {
			return int(f), nil
		}
		return 0, agenterrors.NewValidation(fmt.Sprintf("expected int, got %T", value), nil)

	case "float64", "number":
		if f, ok := value.(float64); ok {
			return f, nil
		}
		if i, ok := value.(int); ok {
			return float64(i), nil
		}
		return 0.0, agenterrors.NewValidation(fmt.Sprintf("expected float64, got %T", value), nil)

	case "bool", "boolean":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		return false, agenterrors.NewValidation(fmt.Sprintf("expected bool, got %T", value), nil)

	case "array":
		if arr, ok := value.([]interface{}); ok {
			return arr, nil
		}
		return nil, agenterrors.NewValidation(fmt.Sprintf("expected array, got %T", value), nil)

	case "object":
		switch typed := value.(type) {
		case map[string]interface{}:
			return typed, nil
		case []interface{}:
			// Allow top-level arrays for structured content payloads.
			return typed, nil
		default:
			return nil, agenterrors.NewValidation(fmt.Sprintf("expected object, got %T", value), nil)
		}

	default:
		return value, nil // No conversion needed for unknown types
	}
}
