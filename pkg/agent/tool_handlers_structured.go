package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"gopkg.in/yaml.v3"
)

type jsonPatchOperation struct {
	Op    string
	Path  string
	From  string
	Value interface{}
}

func handleWriteStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", fmt.Errorf("unsupported structured format; use json or yaml")
	}

	data, exists := args["data"]
	if !exists {
		return "", fmt.Errorf("parameter 'data' is required")
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", err
		}
		if errs := validateDataAgainstSchema(data, schema, "$"); len(errs) > 0 {
			return "", fmt.Errorf("schema validation failed: %s", strings.Join(errs, "; "))
		}
	}

	content, err := serializeStructuredContent(format, data)
	if err != nil {
		return "", err
	}

	return handleWriteFile(ctx, a, map[string]interface{}{
		"path":    path,
		"content": content,
	})
}

func handlePatchStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	opsRaw, ok := args["patch_ops"]
	if !ok {
		return "", fmt.Errorf("parameter 'patch_ops' is required")
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", fmt.Errorf("unsupported structured format; use json or yaml")
	}

	resolvedPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "patch_structured_file", path, err)
		if ctx2 != ctx {
			resolvedPath, err = filesystem.SafeResolvePathWithBypass(ctx2, path)
		}
		if err != nil {
			return "", err
		}
	}
	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("failed to read structured file: %w", err)
	}

	doc, err := deserializeStructuredContent(format, string(contentBytes))
	if err != nil {
		return "", fmt.Errorf("failed to parse structured content: %w", err)
	}

	ops, err := parsePatchOperations(opsRaw)
	if err != nil {
		return "", err
	}

	for _, op := range ops {
		doc, err = applyPatchOperation(doc, op)
		if err != nil {
			return "", err
		}
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", err
		}
		if errs := validateDataAgainstSchema(doc, schema, "$"); len(errs) > 0 {
			return "", fmt.Errorf("schema validation failed after patch: %s", strings.Join(errs, "; "))
		}
	}

	updated, err := serializeStructuredContent(format, doc)
	if err != nil {
		return "", err
	}

	return handleWriteFile(ctx, a, map[string]interface{}{
		"path":    path,
		"content": updated,
	})
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
		b, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal JSON: %w", err)
		}
		return string(b) + "\n", nil
	case "yaml":
		b, err := yaml.Marshal(data)
		if err != nil {
			return "", fmt.Errorf("failed to marshal YAML: %w", err)
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			b = append(b, '\n')
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func deserializeStructuredContent(format, content string) (interface{}, error) {
	var doc interface{}
	switch format {
	case "json":
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			return nil, err
		}
	case "yaml":
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
	return normalizeYAMLValue(doc), nil
}

func normalizeYAMLValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, value := range typed {
			out[k] = normalizeYAMLValue(value)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, value := range typed {
			out[fmt.Sprint(k)] = normalizeYAMLValue(value)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i := range typed {
			out[i] = normalizeYAMLValue(typed[i])
		}
		return out
	default:
		return typed
	}
}

func toSchemaMap(v interface{}) (map[string]interface{}, error) {
	schema, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("parameter 'schema' must be an object")
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
		return nil, fmt.Errorf("parameter 'patch_ops' must be an array")
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
		return nil, err
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
			return nil, err
		}
		if !reflect.DeepEqual(actual, op.Value) {
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
			return value, nil
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
			return nil, err
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
			return nil, err
		}
		typed[idx] = updatedChild
		return typed, nil
	default:
		return nil, fmt.Errorf("cannot traverse into non-container at segment '%s'", token)
	}
}

func mutateAtLeaf(node interface{}, token string, value interface{}, op string) (interface{}, error) {
	switch typed := node.(type) {
	case map[string]interface{}:
		switch op {
		case "add":
			typed[token] = value
			return typed, nil
		case "replace":
			if _, exists := typed[token]; !exists {
				return nil, fmt.Errorf("cannot replace missing key '%s'", token)
			}
			typed[token] = value
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
			return append(typed, value), nil
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
			typed[idx] = value
			return typed, nil
		case "replace":
			if idx < 0 || idx >= len(typed) {
				return nil, fmt.Errorf("array replace index out of range: %d", idx)
			}
			typed[idx] = value
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
		return nil, fmt.Errorf("patch path cannot be empty")
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
