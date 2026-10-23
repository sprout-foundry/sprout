package agent

// tool_structured_validation.go — the schema-validation layer for the
// write_structured_file / patch_structured_file tools: validateDataAgainstSchema and its helpers (formatStructured
// ValidationError, extractValidationPaths, limitStrings, isNumberValue,
// isIntegerValue, the maxStructuredErrorDetails cap). Split out of
// tool_handlers_structured.go.
import (
	"fmt"
	"reflect"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const maxStructuredErrorDetails = 8

func validateDataAgainstSchema(data interface{}, schema map[string]interface{}, path string) []string {
	if schema == nil {
		return nil
	}

	var errs []string
	if typeRaw, ok := schema["type"]; ok {
		typeName, _ := typeRaw.(string)
		switch typeName {
		case "object":
			// Support both *OrderedMap and map[string]interface{} for
			// schema validation. The *OrderedMap case must come first
			// because Go type switches match the first applicable case.
			switch obj := data.(type) {
			case *OrderedMap:
				if reqRaw, ok := schema["required"]; ok {
					required, ok := reqRaw.([]interface{})
					if ok {
						for _, entry := range required {
							key := fmt.Sprint(entry)
							if _, exists := obj.Get(key); !exists {
								errs = append(errs, fmt.Sprintf("%s.%s: required field missing", path, key))
							}
						}
					}
				}
				props, _ := schema["properties"].(map[string]interface{})
				for _, pair := range obj.InOrder() {
					propRaw, exists := props[pair.Key]
					if !exists {
						if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
							errs = append(errs, fmt.Sprintf("%s.%s: additional property not allowed", path, pair.Key))
						}
						continue
					}
					propSchema, ok := propRaw.(map[string]interface{})
					if !ok {
						continue
					}
					errs = append(errs, validateDataAgainstSchema(pair.Value, propSchema, path+"."+pair.Key)...)
				}
			case map[string]interface{}:
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
			default:
				return []string{fmt.Sprintf("%s: expected object", path)}
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
			if !tools.IsNumberValue(data) {
				errs = append(errs, fmt.Sprintf("%s: expected number", path))
			}
		case "integer":
			if !tools.IsIntegerValue(data) {
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
			// Normalize *OrderedMap → map[string]interface{} so that
			// reflect.DeepEqual works when data is *OrderedMap but enum
			// values are map[string]interface{} (from JSON tool args).
			comparableData := convertFromOrderedValue(data)
			match := false
			for _, candidate := range enumVals {
				if reflect.DeepEqual(candidate, comparableData) {
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
		return agenterrors.NewInvalidInputError("schema validation failed: no error details provided", nil)
	}

	paths := tools.ExtractValidationPaths(errs)
	pathSummary := strings.Join(tools.LimitStrings(paths, maxStructuredErrorDetails), ",")
	if pathSummary == "" {
		pathSummary = "unknown"
	}

	details := strings.Join(tools.LimitStrings(errs, maxStructuredErrorDetails), " | ")
	if len(errs) > maxStructuredErrorDetails {
		details += fmt.Sprintf(" | ...(%d more)", len(errs)-maxStructuredErrorDetails)
	}

	if context == "" {
		return agenterrors.NewValidation(fmt.Sprintf("schema validation failed: tool=%s error_count=%d failed_paths=%s details=%s", toolName, len(errs), pathSummary, details), nil)
	}

	return agenterrors.NewValidation(fmt.Sprintf("schema validation failed: tool=%s %s error_count=%d failed_paths=%s details=%s", toolName, context, len(errs), pathSummary, details), nil)
}
