package tools

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Patch operation helpers (used by patch_structured_file)
// ---------------------------------------------------------------------------

// stringField returns obj[key] as a string, or "" when the key is absent or
// holds a non-string. It exists because fmt.Sprint(nil) is "<nil>": a missing
// key would otherwise read as a present, non-empty value.
func stringField(obj map[string]interface{}, key string) string {
	s, _ := obj[key].(string)
	return s
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
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] must be an object", i), nil)
		}
		// Read the string fields as strings: fmt.Sprint(nil) yields "<nil>",
		// which is non-empty and would slip past the empty-path check below.
		op := strings.ToLower(strings.TrimSpace(stringField(obj, "op")))
		path := stringField(obj, "path")
		from := stringField(obj, "from")

		if op == "" || path == "" {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] requires non-empty op and path", i), nil)
		}
		if !slices.Contains([]string{"add", "replace", "remove", "test"}, op) {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] has unsupported op '%s'", i, op), nil)
		}
		if op == "add" || op == "replace" || op == "test" {
			if _, exists := obj["value"]; !exists {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] requires value for op '%s'", i, op), nil)
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

// ---------------------------------------------------------------------------
// Patch operations on generic interface{} (loses YAML key order)
// ---------------------------------------------------------------------------

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
		if !reflect.DeepEqual(actual, op.Value) {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch test failed at %s", op.Path), nil)
		}
		return doc, nil
	default:
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("unsupported patch op: %s", op.Op), nil)
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
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("unsupported op: %s", op), nil)
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
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("path segment '%s' does not exist", token), nil)
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
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("array index out of range at segment '%s'", token), nil)
		}
		updatedChild, err := applyMutation(typed[idx], segments[1:], value, op)
		if err != nil {
			return nil, agenterrors.NewTool("structured", "failed to apply mutation", err)
		}
		typed[idx] = updatedChild
		return typed, nil
	default:
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("cannot traverse into non-container at segment '%s'", token), nil)
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
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("cannot replace missing key '%s'", token), nil)
			}
			typed[token] = value
			return typed, nil
		case "remove":
			if _, exists := typed[token]; !exists {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("cannot remove missing key '%s'", token), nil)
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
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("invalid array index '%s'", token), nil)
		}

		switch op {
		case "add":
			if idx < 0 || idx > len(typed) {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("array insert index out of range: %d", idx), nil)
			}
			typed = append(typed, nil)
			copy(typed[idx+1:], typed[idx:])
			typed[idx] = value
			return typed, nil
		case "replace":
			if idx < 0 || idx >= len(typed) {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("array replace index out of range: %d", idx), nil)
			}
			typed[idx] = value
			return typed, nil
		case "remove":
			if idx < 0 || idx >= len(typed) {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("array remove index out of range: %d", idx), nil)
			}
			return append(typed[:idx], typed[idx+1:]...), nil
		}
	}

	return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("cannot apply %s at token '%s'", op, token), nil)
}

func parseJSONPointer(path string) ([]string, error) {
	if path == "" {
		return nil, agenterrors.NewInvalidInputError("patch path cannot be empty", nil)
	}
	if path == "/" {
		return []string{""}, nil
	}
	if path[0] != '/' {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("invalid patch path '%s': must start with '/'", path), nil)
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
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("path segment '%s' does not exist", segment), nil)
			}
			current = value
		case []interface{}:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(typed) {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("array index out of range at segment '%s'", segment), nil)
			}
			current = typed[idx]
		default:
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("cannot traverse non-container at segment '%s'", segment), nil)
		}
	}
	return current, nil
}
