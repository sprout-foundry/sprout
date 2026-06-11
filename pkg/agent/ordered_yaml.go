package agent

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ParseYAMLOrdered parses a YAML string into an *OrderedMap, preserving the
// key order from the source text. The YAML content must represent a mapping
// (object) at the top level. Nested mappings are recursively wrapped in
// *OrderedMap so that ordering is maintained at every depth.
//
// This function uses yaml.Node to walk the parsed tree, which preserves the
// original key ordering from the source document.
func ParseYAMLOrdered(content string) (*OrderedMap, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// The top-level node is a DocumentNode; its first Content element is
	// the root value node.
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("YAML content is empty or not a valid document")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("YAML content must be a mapping at the top level, got %s", yamlNodeKindName(root.Kind))
	}

	result, err := yamlMappingToOrderedMap(root)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// yamlMappingToOrderedMap converts a yaml.MappingNode into an *OrderedMap,
// iterating Content pairs (key, value) in document order. Merge keys (<<)
// are resolved first so that subsequent keys can override the merged values.
func yamlMappingToOrderedMap(node *yaml.Node) (*OrderedMap, error) {
	om := NewOrderedMap()

	// First pass: resolve merge keys (<<) to build the base mapping.
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Value == "<<" {
			if err := mergeMappingInto(om, valNode); err != nil {
				return nil, fmt.Errorf("failed to resolve merge key: %w", err)
			}
		}
	}

	// Second pass: process all non-merge keys. These override any values
	// from the merged base.
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Value == "<<" {
			continue
		}

		key := keyNode.Value
		value, err := yamlNodeToValue(valNode)
		if err != nil {
			return nil, fmt.Errorf("failed to parse value for key %q: %w", key, err)
		}

		om.Set(key, value)
	}

	return om, nil
}

// mergeMappingInto resolves a YAML merge key value and merges its entries
// into the target *OrderedMap. The value can be:
//   - an AliasNode pointing to a MappingNode (most common: <<: *defaults)
//   - a MappingNode directly (inline merge)
//   - a SequenceNode of aliases (merge list: <<: [*a, *b], last wins)
func mergeMappingInto(om *OrderedMap, node *yaml.Node) error {
	// Resolve aliases first.
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}

	switch node.Kind {
	case yaml.MappingNode:
		base, err := yamlMappingToOrderedMap(node)
		if err != nil {
			return err
		}
		for _, pair := range base.InOrder() {
			om.Set(pair.Key, pair.Value)
		}
	case yaml.SequenceNode:
		// Sequence of merge targets — iterate in order so that later
		// entries override earlier ones.
		for _, elem := range node.Content {
			if err := mergeMappingInto(om, elem); err != nil {
				return err
			}
		}
	default:
		// Ignore non-mapping merge values silently — matches standard
		// YAML merge key behavior.
	}
	return nil
}

// yamlNodeToValue converts a yaml.Node into a Go value. MappingNodes become
// *OrderedMap, SequenceNodes become []interface{}, and ScalarNodes are decoded
// into their appropriate Go types.
func yamlNodeToValue(node *yaml.Node) (interface{}, error) {
	// Resolve aliases — the Alias field points to the anchor node.
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}

	switch node.Kind {
	case yaml.MappingNode:
		return yamlMappingToOrderedMap(node)
	case yaml.SequenceNode:
		result := make([]interface{}, 0, len(node.Content))
		for i, child := range node.Content {
			val, err := yamlNodeToValue(child)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sequence element %d: %w", i, err)
			}
			result = append(result, val)
		}
		return result, nil
	case yaml.ScalarNode:
		return yamlScalarToValue(node)
	default:
		return nil, fmt.Errorf("unexpected YAML node kind: %s", yamlNodeKindName(node.Kind))
	}
}

// yamlScalarToValue decodes a scalar yaml.Node into the appropriate Go type.
// It uses yaml.Node.Decode to leverage the library's built-in type resolution
// based on the node's resolved tag.
func yamlScalarToValue(node *yaml.Node) (interface{}, error) {
	// Null tag.
	if node.Tag == "!!null" {
		return nil, nil
	}

	// Boolean.
	if node.Tag == "!!bool" {
		var b bool
		if err := node.Decode(&b); err != nil {
			return nil, fmt.Errorf("failed to decode YAML bool: %w", err)
		}
		return b, nil
	}

	// Integer.
	if node.Tag == "!!int" {
		var n int
		if err := node.Decode(&n); err != nil {
			// Try int64 for large values.
			var n64 int64
			if err2 := node.Decode(&n64); err2 != nil {
				return nil, fmt.Errorf("failed to decode YAML int: %w", err)
			}
			return n64, nil
		}
		return n, nil
	}

	// Float.
	if node.Tag == "!!float" {
		var f float64
		if err := node.Decode(&f); err != nil {
			return nil, fmt.Errorf("failed to decode YAML float: %w", err)
		}
		return f, nil
	}

	// String (or anything else — treat as string).
	return node.Value, nil
}

// NormalizeYAMLOrdered recursively normalizes YAML-parsed values into
// *OrderedMap-safe representations. It handles the remaining cases where
// yaml.Unmarshal into interface{} produces map[interface{}]interface{} or
// map[string]interface{} values, converting them to *OrderedMap. This
// replaces the old normalizeYAMLValue function.
//
// When key order is unknown (e.g., from a regular map), keys are sorted
// alphabetically as a deterministic fallback.
func NormalizeYAMLOrdered(v interface{}) interface{} {
	switch typed := v.(type) {
	case *OrderedMap:
		// Already ordered — normalize nested values.
		for _, pair := range typed.InOrder() {
			typed.Set(pair.Key, NormalizeYAMLOrdered(pair.Value))
		}
		return typed
	case map[string]interface{}:
		return OrderedMapFromMap(typed)
	case map[interface{}]interface{}:
		// yaml.Unmarshal sometimes produces this type for mappings with
		// non-string keys. Convert to a string-keyed map first, then to
		// OrderedMap with sorted keys.
		strMap := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			strMap[fmt.Sprint(k)] = NormalizeYAMLOrdered(val)
		}
		return OrderedMapFromMap(strMap)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, elem := range typed {
			out[i] = NormalizeYAMLOrdered(elem)
		}
		return out
	default:
		return v
	}
}

// yamlNodeKindName returns a human-readable name for a yaml.Kind value.
func yamlNodeKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}
