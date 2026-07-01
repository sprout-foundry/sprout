package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type jsonPatchOperation struct {
	Op    string
	Path  string
	From  string
	Value interface{}
}

const maxStructuredErrorDetails = 8

func handleWriteStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", agenterrors.NewInvalidInputError("unsupported structured format: use json or yaml", nil)
	}

	data, exists := args["data"]
	if !exists {
		return "", agenterrors.NewInvalidInputError("parameter 'data' is required", nil)
	}

	// Convert data to *OrderedMap for ordered serialization.
	// If the caller already provides an *OrderedMap (future-proofing), use it directly.
	// Otherwise, convert map[string]interface{} → OrderedMapFromMap (alphabetical order).
	switch d := data.(type) {
	case *OrderedMap:
		// Already ordered — use as-is.
	case map[string]interface{}:
		data = OrderedMapFromMap(d)
	default:
		// For non-object types (arrays, scalars), pass through — the
		// ordered serializers handle these via their fallback paths.
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to parse schema", err)
		}
		if errs := validateDataAgainstSchema(data, schema, "$"); len(errs) > 0 {
			return "", formatStructuredValidationError("write_structured_file", errs, "")
		}
	}

	content, err := serializeStructuredContent(format, data)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to serialize structured content", err)
	}

	result, err := writeFileContent(ctx, a, path, content, "write_structured_file", true)
	if err != nil {
		return "", agenterrors.NewTool("structured", fmt.Sprintf("failed to write structured file %s", path), err)
	}
	return result, nil
}

func handlePatchStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	opsRaw, ok := args["patch_ops"]
	if !ok {
		// Compatibility path: some models call patch_structured_file with full `data`
		// instead of patch operations. Treat this as a structured write.
		if data, hasData := args["data"]; hasData {
			writeArgs := map[string]interface{}{
				"path": path,
				"data": data,
			}
			if format, hasFormat := args["format"]; hasFormat {
				writeArgs["format"] = format
			}
			if schema, hasSchema := args["schema"]; hasSchema {
				writeArgs["schema"] = schema
			}
			return handleWriteStructuredFile(ctx, a, writeArgs)
		}
		return "", agenterrors.NewInvalidInputError("parameter 'patch_ops' is required (or provide 'data' for full write)", nil)
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", agenterrors.NewInvalidInputError("unsupported structured format: use json or yaml", nil)
	}

	resolvedPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "patch_structured_file", path, err); approved {
			resolvedPath, err = filesystem.SafeResolvePathWithBypass(ctx2, path)
		}
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to resolve file path", err)
		}
	}
	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to read structured file", err)
	}

	doc, err := deserializeStructuredContent(format, string(contentBytes))
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to parse structured content", err)
	}

	ops, err := parsePatchOperations(opsRaw)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to parse patch operations", err)
	}

	applied := 0
	for i, op := range ops {
		doc, err = applyPatchOperation(doc, op)
		if err != nil {
			return "", agenterrors.Wrapf(err, "patch operation failed: tool=patch_structured_file index=%d op=%s path=%s applied=%d/%d",
				i, op.Op, op.Path, applied, len(ops))
		}
		applied++
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to parse schema", err)
		}
		if errs := validateDataAgainstSchema(doc, schema, "$"); len(errs) > 0 {
			context := fmt.Sprintf("applied=%d/%d", applied, len(ops))
			return "", formatStructuredValidationError("patch_structured_file", errs, context)
		}
	}

	updated, err := serializeStructuredContent(format, doc)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to serialize updated content", err)
	}

	result, err := writeFileContent(ctx, a, path, updated, "patch_structured_file", true)
	if err != nil {
		return "", agenterrors.NewTool("structured", fmt.Sprintf("failed to write patched file %s", path), err)
	}
	return result, nil
}

func inferStructuredFormat(path, provided string) string {
	format := strings.ToLower(strings.TrimSpace(provided))
	switch format {
	case "json", "yaml", "yml":
		if format == "yml" {
			return "yaml"
		}
		return format
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}

func serializeStructuredContent(format string, data interface{}) (string, error) {
	switch format {
	case "json":
		return SerializeJSONOrdered(data)
	case "yaml":
		return SerializeYAMLOrdered(data)
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func deserializeStructuredContent(format, content string) (interface{}, error) {
	switch format {
	case "json":
		return ParseJSONOrdered(content)
	case "yaml":
		return ParseYAMLOrdered(content)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

func toSchemaMap(v interface{}) (map[string]interface{}, error) {
	schema, ok := v.(map[string]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("parameter 'schema' must be an object", nil)
	}
	return schema, nil
}

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

	paths := extractValidationPaths(errs)
	pathSummary := strings.Join(limitStrings(paths, maxStructuredErrorDetails), ",")
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

func parsePatchOperations(v interface{}) ([]jsonPatchOperation, error) {
	rawOps, ok := v.([]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("parameter 'patch_ops' must be an array", nil)
	}

	ops := make([]jsonPatchOperation, 0, len(rawOps))
	for i, raw := range rawOps {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("patch_ops[%d] must be an object", i)
		}
		op := strings.ToLower(strings.TrimSpace(fmt.Sprint(obj["op"])))
		path := fmt.Sprint(obj["path"])
		from := ""
		if fromRaw, ok := obj["from"]; ok {
			from = fmt.Sprint(fromRaw)
		}

		if op == "" || path == "" {
			return nil, fmt.Errorf("patch_ops[%d] requires non-empty op and path", i)
		}
		if !slices.Contains([]string{"add", "replace", "remove", "test"}, op) {
			return nil, fmt.Errorf("patch_ops[%d] has unsupported op '%s'", i, op)
		}
		if op == "add" || op == "replace" || op == "test" {
			if _, exists := obj["value"]; !exists {
				return nil, fmt.Errorf("patch_ops[%d] requires value for op '%s'", i, op)
			}
		}
		ops = append(ops, jsonPatchOperation{
			Op:    op,
			Path:  path,
			From:  from,
			Value: obj["value"],
		})
	}

	return ops, nil
}

func applyPatchOperation(doc interface{}, op jsonPatchOperation) (interface{}, error) {
	segments, err := parseJSONPointer(op.Path)
	if err != nil {
		return nil, agenterrors.NewTool("structured", "failed to parse JSON pointer", err)
	}

	switch op.Op {
	case "add":
		return applyMutation(doc, segments, op.Value, "add")
	case "replace":
		return applyMutation(doc, segments, op.Value, "replace")
	case "remove":
		return applyMutation(doc, segments, nil, "remove")
	case "test":
		actual, err := readPointerValue(doc, segments)
		if err != nil {
			return nil, agenterrors.NewTool("structured", "failed to read pointer value", err)
		}
		// Normalize *OrderedMap → map[string]interface{} so that
		// reflect.DeepEqual works when op.Value is a plain map (from JSON
		// tool args) but actual is an *OrderedMap (from ordered deserialization).
		comparableActual := convertFromOrderedValue(actual)
		if !reflect.DeepEqual(comparableActual, op.Value) {
			return nil, fmt.Errorf("patch test failed at %s", op.Path)
		}
		return doc, nil
	default:
		return nil, fmt.Errorf("unsupported patch op: %s", op.Op)
	}
}

func applyMutation(node interface{}, segments []string, value interface{}, op string) (interface{}, error) {
	if len(segments) == 0 {
		switch op {
		case "add", "replace":
			// Convert map[string]interface{} values to *OrderedMap for
			// deterministic key ordering during serialization. Patch values
			// arrive from json.Unmarshal (via tool args) which loses key
			// order — converting ensures consistent output.
			return convertToOrderedValue(value), nil
		case "remove":
			return nil, nil
		default:
			return nil, fmt.Errorf("unsupported op: %s", op)
		}
	}

	token := segments[0]
	if len(segments) == 1 {
		return mutateAtLeaf(node, token, value, op)
	}

	switch typed := node.(type) {
	case *OrderedMap:
		child, exists := typed.Get(token)
		if !exists {
			if op != "add" {
				return nil, fmt.Errorf("path segment '%s' does not exist", token)
			}
			child = NewOrderedMap()
		}
		updatedChild, err := applyMutation(child, segments[1:], value, op)
		if err != nil {
			return nil, agenterrors.NewTool("structured", "failed to apply mutation", err)
		}
		typed.Set(token, updatedChild)
		return typed, nil
	case map[string]interface{}:
		child, exists := typed[token]
		if !exists {
			if op != "add" {
				return nil, fmt.Errorf("path segment '%s' does not exist", token)
			}
			child = map[string]interface{}{}
		}
		updatedChild, err := applyMutation(child, segments[1:], value, op)
		if err != nil {
			return nil, agenterrors.NewTool("structured", "failed to apply mutation", err)
		}
		typed[token] = updatedChild
		return typed, nil
	case []interface{}:
		idx, err := strconv.Atoi(token)
		if err != nil || idx < 0 || idx >= len(typed) {
			return nil, fmt.Errorf("array index out of range at segment '%s'", token)
		}
		updatedChild, err := applyMutation(typed[idx], segments[1:], value, op)
		if err != nil {
			return nil, agenterrors.NewTool("structured", "failed to apply mutation", err)
		}
		typed[idx] = updatedChild
		return typed, nil
	default:
		return nil, fmt.Errorf("cannot traverse into non-container at segment '%s'", token)
	}
}

func mutateAtLeaf(node interface{}, token string, value interface{}, op string) (interface{}, error) {
	// Convert values to ordered form before storing. Patch values arrive from
	// json.Unmarshal (via tool args) as map[string]interface{}, which loses
	// key order. Converting ensures deterministic serialization output.
	orderedValue := convertToOrderedValue(value)

	switch typed := node.(type) {
	case *OrderedMap:
		switch op {
		case "add":
			typed.Set(token, orderedValue)
			return typed, nil
		case "replace":
			if _, exists := typed.Get(token); !exists {
				return nil, fmt.Errorf("cannot replace missing key '%s'", token)
			}
			typed.Set(token, orderedValue)
			return typed, nil
		case "remove":
			if _, exists := typed.Get(token); !exists {
				return nil, fmt.Errorf("cannot remove missing key '%s'", token)
			}
			typed.Delete(token)
			return typed, nil
		}
	case map[string]interface{}:
		switch op {
		case "add":
			typed[token] = value
			return typed, nil
		case "replace":
			if _, exists := typed[token]; !exists {
				return nil, fmt.Errorf("cannot replace missing key '%s'", token)
			}
			typed[token] = orderedValue
			return typed, nil
		case "remove":
			if _, exists := typed[token]; !exists {
				return nil, fmt.Errorf("cannot remove missing key '%s'", token)
			}
			delete(typed, token)
			return typed, nil
		}
	case []interface{}:
		if op == "add" && token == "-" {
			return append(typed, orderedValue), nil
		}
		idx, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid array index '%s'", token)
		}

		switch op {
		case "add":
			if idx < 0 || idx > len(typed) {
				return nil, fmt.Errorf("array insert index out of range: %d", idx)
			}
			typed = append(typed, nil)
			copy(typed[idx+1:], typed[idx:])
			typed[idx] = orderedValue
			return typed, nil
		case "replace":
			if idx < 0 || idx >= len(typed) {
				return nil, fmt.Errorf("array replace index out of range: %d", idx)
			}
			typed[idx] = orderedValue
			return typed, nil
		case "remove":
			if idx < 0 || idx >= len(typed) {
				return nil, fmt.Errorf("array remove index out of range: %d", idx)
			}
			return append(typed[:idx], typed[idx+1:]...), nil
		}
	}

	return nil, fmt.Errorf("cannot apply %s at token '%s'", op, token)
}

func parseJSONPointer(path string) ([]string, error) {
	if path == "" {
		return nil, agenterrors.NewInvalidInputError("patch path cannot be empty", nil)
	}
	if path == "/" {
		return []string{""}, nil
	}
	if path[0] != '/' {
		return nil, fmt.Errorf("invalid patch path '%s': must start with '/'", path)
	}

	raw := strings.Split(path[1:], "/")
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		segments = append(segments, part)
	}
	return segments, nil
}

func readPointerValue(doc interface{}, segments []string) (interface{}, error) {
	current := doc
	for _, segment := range segments {
		switch typed := current.(type) {
		case *OrderedMap:
			val, exists := typed.Get(segment)
			if !exists {
				return nil, fmt.Errorf("path segment '%s' does not exist", segment)
			}
			current = val
		case map[string]interface{}:
			value, exists := typed[segment]
			if !exists {
				return nil, fmt.Errorf("path segment '%s' does not exist", segment)
			}
			current = value
		case []interface{}:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(typed) {
				return nil, fmt.Errorf("array index out of range at segment '%s'", segment)
			}
			current = typed[idx]
		default:
			return nil, fmt.Errorf("cannot traverse non-container at segment '%s'", segment)
		}
	}
	return current, nil
}

func getOptionalString(args map[string]interface{}, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}
