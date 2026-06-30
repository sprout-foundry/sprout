package tools

import (
	"fmt"
	"reflect"
	"strings"
)

// ---------------------------------------------------------------------------
// Schema validation helpers
// ---------------------------------------------------------------------------

// toSchemaMap converts a raw interface{} to a map for schema validation.
func toSchemaMap(v interface{}) (map[string]interface{}, error) {
	schema, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("parameter 'schema' must be an object")
	}
	return schema, nil
}

// validateDataAgainstSchema validates data against a JSON Schema subset.
func validateDataAgainstSchema(data interface{}, schema map[string]interface{}, path string) []string {
	if schema == nil {
		return nil
	}

	var errs []string
	if typeRaw, ok := schema["type"]; ok {
		typeName, _ := typeRaw.(string)
		switch typeName {
		case "object":
			obj, ok := data.(map[string]interface{})
			if !ok {
				return []string{fmt.Sprintf("%s: expected object", path)}
			}
			if reqRaw, ok := schema["required"]; ok {
				required, ok := reqRaw.([]interface{})
				if ok {
					for _, entry := range required {
						key := fmt.Sprint(entry)
						if _, exists := obj[key]; !exists {
							errs = append(errs, fmt.Sprintf("%s.%s: required field missing", path, key))
						}
					}
				}
			}
			props, _ := schema["properties"].(map[string]interface{})
			for key, value := range obj {
				propRaw, exists := props[key]
				if !exists {
					if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
						errs = append(errs, fmt.Sprintf("%s.%s: additional property not allowed", path, key))
					}
					continue
				}
				propSchema, ok := propRaw.(map[string]interface{})
				if !ok {
					continue
				}
				errs = append(errs, validateDataAgainstSchema(value, propSchema, path+"."+key)...)
			}
		case "array":
			arr, ok := data.([]interface{})
			if !ok {
				return []string{fmt.Sprintf("%s: expected array", path)}
			}
			itemSchema, _ := schema["items"].(map[string]interface{})
			for i, value := range arr {
				if itemSchema != nil {
					errs = append(errs, validateDataAgainstSchema(value, itemSchema, fmt.Sprintf("%s[%d]", path, i))...)
				}
			}
		case "string":
			if _, ok := data.(string); !ok {
				errs = append(errs, fmt.Sprintf("%s: expected string", path))
			}
		case "number":
			if !isNumberValue(data) {
				errs = append(errs, fmt.Sprintf("%s: expected number", path))
			}
		case "integer":
			if !isIntegerValue(data) {
				errs = append(errs, fmt.Sprintf("%s: expected integer", path))
			}
		case "boolean":
			if _, ok := data.(bool); !ok {
				errs = append(errs, fmt.Sprintf("%s: expected boolean", path))
			}
		case "null":
			if data != nil {
				errs = append(errs, fmt.Sprintf("%s: expected null", path))
			}
		}
	}

	if enumRaw, ok := schema["enum"]; ok {
		enumVals, ok := enumRaw.([]interface{})
		if ok {
			match := false
			for _, candidate := range enumVals {
				if reflect.DeepEqual(candidate, data) {
					match = true
					break
				}
			}
			if !match {
				errs = append(errs, fmt.Sprintf("%s: value not in enum", path))
			}
		}
	}

	return errs
}

func formatStructuredValidationError(toolName string, errs []string, context string) error {
	if len(errs) == 0 {
		return fmt.Errorf("schema validation failed: no error details provided")
	}

	paths := extractValidationPaths(errs)
	pathSummary := strings.Join(limitStrings(paths, maxStructuredErrorDetails), ", ")
	if pathSummary == "" {
		pathSummary = "unknown"
	}

	details := strings.Join(limitStrings(errs, maxStructuredErrorDetails), " | ")
	if len(errs) > maxStructuredErrorDetails {
		details += fmt.Sprintf(" | ...(%d more)", len(errs)-maxStructuredErrorDetails)
	}

	if context == "" {
		return fmt.Errorf("schema validation failed: tool=%s error_count=%d failed_paths=%s details=%s", toolName, len(errs), pathSummary, details)
	}

	return fmt.Errorf("schema validation failed: tool=%s %s error_count=%d failed_paths=%s details=%s", toolName, context, len(errs), pathSummary, details)
}

func extractValidationPaths(errs []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(errs))
	for _, errText := range errs {
		text := strings.TrimSpace(errText)
		if text == "" {
			continue
		}

		path := text
		if idx := strings.Index(path, ":"); idx > 0 {
			path = strings.TrimSpace(path[:idx])
		}

		if !strings.HasPrefix(path, "$") {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func limitStrings(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return values
	}
	return values[:max]
}

func isNumberValue(v interface{}) bool {
	switch v.(type) {
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func isIntegerValue(v interface{}) bool {
	switch value := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return float64(int64(value)) == value
	default:
		return false
	}
}
