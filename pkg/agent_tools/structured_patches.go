package tools

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
// Patch operations on yaml.Node (preserves key insertion order)
// ---------------------------------------------------------------------------

// applyPatchOperationNode applies a JSON Patch operation directly against a
// *yaml.Node tree, preserving key insertion order from the on-disk file.
func applyPatchOperationNode(node *yaml.Node, op jsonPatchOperation) (*yaml.Node, error) {
	segments, err := parseJSONPointer(op.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON pointer: %w", err)
	}

	switch op.Op {
	case "add":
		return applyMutationNode(node, segments, mapToYamlNode(op.Value), "add")
	case "replace":
		return applyMutationNode(node, segments, mapToYamlNode(op.Value), "replace")
	case "remove":
		return applyMutationNode(node, segments, nil, "remove")
	case "test":
		// Navigate to the target node at the path, then compare its decoded value.
		current := resolveYAMLRoot(node)
		// Special case: path "/" means the root itself
		if len(segments) == 1 && segments[0] == "" {
			actual := nodeToValue(current)
			if !reflect.DeepEqual(actual, op.Value) {
				return nil, fmt.Errorf("patch test failed at %s", op.Path)
			}
			return node, nil
		}
		for _, seg := range segments {
			switch current.Kind {
			case yaml.MappingNode:
				child := findMapValue(current, seg)
				if child == nil {
					return nil, fmt.Errorf("path segment '%s' does not exist", seg)
				}
				current = child
			case yaml.SequenceNode:
				idx, err := strconv.Atoi(seg)
				if err != nil || idx < 0 || idx >= len(current.Content) {
					return nil, fmt.Errorf("array index out of range at segment '%s'", seg)
				}
				current = current.Content[idx]
			default:
				return nil, fmt.Errorf("cannot traverse into non-container at segment '%s'", seg)
			}
		}
		actual := nodeToValue(current)
		if !reflect.DeepEqual(actual, op.Value) {
			return nil, fmt.Errorf("patch test failed at %s", op.Path)
		}
		return node, nil
	default:
		return nil, fmt.Errorf("unsupported patch op: %s", op.Op)
	}
}

// resolveYAMLRoot returns the actual root content node. If the node is a
// DocumentNode, return Content[0]; otherwise return the node itself.
func resolveYAMLRoot(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

// nodeToValue decodes a yaml.Node back to interface{} for test comparisons.
func nodeToValue(node *yaml.Node) interface{} {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) > 0 {
			return nodeToValue(node.Content[0])
		}
		return nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!null":
			return nil
		case "!!bool":
			return node.Value == "true"
		case "!!int":
			v, err := strconv.ParseInt(node.Value, 10, 64)
			if err != nil {
				return node.Value
			}
			return v
		case "!!float":
			v, err := strconv.ParseFloat(node.Value, 64)
			if err != nil {
				return node.Value
			}
			return v
		case "!!str":
			return node.Value
		default:
			return node.Value
		}
	case yaml.SequenceNode:
		out := make([]interface{}, len(node.Content))
		for i, c := range node.Content {
			out[i] = nodeToValue(c)
		}
		return out
	case yaml.MappingNode:
		out := make(map[string]interface{})
		for i := 0; i+1 < len(node.Content); i += 2 {
			out[node.Content[i].Value] = nodeToValue(node.Content[i+1])
		}
		return out
	default:
		return node.Value
	}
}

// applyMutationNode navigates the yaml.Node tree to the target location and
// applies the mutation (add/replace/remove). Returns the updated root node.
//
// The root may be a DocumentNode wrapping the real root in Content[0], or
// it may be the real root directly.  All mutations mutate nodes in-place
// and return the original root pointer so callers can chain operations.
func applyMutationNode(root *yaml.Node, segments []string, valueNode *yaml.Node, op string) (*yaml.Node, error) {
	// Unwrap DocumentNode to find the real root for navigation.
	current := resolveYAMLRoot(root)

	// Special case: path "/" (segments == [""]) replaces/removes the root.
	if len(segments) == 1 && segments[0] == "" {
		switch op {
		case "add", "replace":
			if root.Kind == yaml.DocumentNode {
				if len(root.Content) == 0 {
					root.Content = []*yaml.Node{valueNode}
				} else {
					root.Content[0] = valueNode
				}
			} else {
				current.Kind = valueNode.Kind
				current.Tag = valueNode.Tag
				current.Value = valueNode.Value
				current.Content = valueNode.Content
				current.Style = valueNode.Style
			}
			return root, nil
		case "remove":
			return root, nil
		}
	}

	if len(segments) == 0 {
		return root, nil
	}

	// Navigate to the parent node (all segments except the last).
	for i := 0; i < len(segments)-1; i++ {
		seg := segments[i]
		switch current.Kind {
		case yaml.MappingNode:
			child := findMapValue(current, seg)
			if child == nil {
				if op != "add" {
					return nil, fmt.Errorf("path segment '%s' does not exist", seg)
				}
				// Create intermediate mapping node for "add".
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				current.Content = append(current.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg},
					child,
				)
			}
			current = child
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(current.Content) {
				return nil, fmt.Errorf("array index out of range at segment '%s'", seg)
			}
			current = current.Content[idx]
		case yaml.ScalarNode:
			return nil, fmt.Errorf("cannot traverse into scalar at segment '%s'", seg)
		default:
			return nil, fmt.Errorf("cannot traverse into non-container at segment '%s'", seg)
		}
	}

	// Apply the mutation at the leaf (last segment).
	lastSeg := segments[len(segments)-1]
	switch current.Kind {
	case yaml.MappingNode:
		if _, err := mutateMapNode(current, lastSeg, valueNode, op); err != nil {
			return nil, err
		}
	case yaml.SequenceNode:
		if _, err := mutateSequenceNode(current, lastSeg, valueNode, op); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("cannot apply %s at non-container node for segment '%s'", op, lastSeg)
	}
	return root, nil
}

// findMapValue returns the value node for a key in a mapping, or nil.
func findMapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// findMapKeyIndex returns the index of the key node (-1 if not found).
func findMapKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// mutateMapNode applies a leaf-level mutation on a mapping node.
func mutateMapNode(m *yaml.Node, key string, valueNode *yaml.Node, op string) (*yaml.Node, error) {
	idx := findMapKeyIndex(m, key)

	switch op {
	case "add":
		if idx >= 0 {
			// Key exists — replace value (add with existing key = replace)
			m.Content[idx+1] = valueNode
		} else {
			// Key doesn't exist — append new key-value pair
			m.Content = append(m.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				valueNode,
			)
		}
		return m, nil
	case "replace":
		if idx < 0 {
			return nil, fmt.Errorf("cannot replace missing key '%s'", key)
		}
		m.Content[idx+1] = valueNode
		return m, nil
	case "remove":
		if idx < 0 {
			return nil, fmt.Errorf("cannot remove missing key '%s'", key)
		}
		// Remove both key and value (2 elements) from Content
		m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
		return m, nil
	}

	return nil, fmt.Errorf("unsupported op on mapping: %s", op)
}

// mutateSequenceNode applies a leaf-level mutation on a sequence node.
func mutateSequenceNode(s *yaml.Node, token string, valueNode *yaml.Node, op string) (*yaml.Node, error) {
	switch op {
	case "add":
		if token == "-" {
			s.Content = append(s.Content, valueNode)
			return s, nil
		}
		idx, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid array index '%s'", token)
		}
		if idx < 0 || idx > len(s.Content) {
			return nil, fmt.Errorf("array insert index out of range: %d", idx)
		}
		// Insert at idx (shift existing elements right)
		s.Content = append(s.Content, nil)
		copy(s.Content[idx+1:], s.Content[idx:])
		s.Content[idx] = valueNode
		return s, nil
	case "replace":
		idx, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid array index '%s'", token)
		}
		if idx < 0 || idx >= len(s.Content) {
			return nil, fmt.Errorf("array replace index out of range: %d", idx)
		}
		s.Content[idx] = valueNode
		return s, nil
	case "remove":
		idx, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid array index '%s'", token)
		}
		if idx < 0 || idx >= len(s.Content) {
			return nil, fmt.Errorf("array remove index out of range: %d", idx)
		}
		s.Content = append(s.Content[:idx], s.Content[idx+1:]...)
		return s, nil
	}

	return nil, fmt.Errorf("unsupported op on sequence: %s", op)
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
