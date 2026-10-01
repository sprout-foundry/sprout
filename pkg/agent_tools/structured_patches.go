package tools

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Patch operation helpers (used by patch_structured_file)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Patch operations on generic interface{} (loses YAML key order)
// ---------------------------------------------------------------------------

func applyPatchOperation(doc interface{}, op jsonPatchOperation) (interface{}, error) {
	segments, err := parseJSONPointer(op.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON pointer: %w", err)
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
			return nil, fmt.Errorf("failed to read pointer value: %w", err)
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
			return nil, fmt.Errorf("failed to apply mutation: %w", err)
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
			return nil, fmt.Errorf("failed to apply mutation: %w", err)
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
