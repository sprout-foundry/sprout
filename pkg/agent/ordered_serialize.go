package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/errors"
	"gopkg.in/yaml.v3"
)

// SerializeJSONOrdered serializes data to a pretty-printed JSON string with
// 2-space indentation. When data is an *OrderedMap, keys are emitted in
// insertion order. For regular map[string]interface{} and other types, the
// standard json.Marshal behavior is used as a fallback.
//
// HTML escaping is disabled to match the behavior of the existing
// serializeStructuredContent function.
func SerializeJSONOrdered(data interface{}) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	val, err := orderedToJSONValue(data)
	if err != nil {
		return "", err
	}

	if err := enc.Encode(val); err != nil {
		return "", errors.NewTool("ordered", "failed to encode ordered JSON", err)
	}

	// json.Encoder.Encode appends a trailing newline; trim it to match
	// existing behavior.
	return strings.TrimRight(buf.String(), "\n"), nil
}

// orderedToJSONValue converts a Go value into a JSON-serializable form.
// *OrderedMap values are converted to map[string]interface{} preserving
// insertion order using a custom ordered map type that json.Marshal respects.
// Slices are walked recursively.
func orderedToJSONValue(v interface{}) (interface{}, error) {
	switch typed := v.(type) {
	case *OrderedMap:
		return orderedMapToJSON(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, elem := range typed {
			converted, err := orderedToJSONValue(elem)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	case map[string]interface{}:
		// Regular map — sort keys for determinism but don't need ordering.
		// Recursively convert nested values.
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			converted, err := orderedToJSONValue(val)
			if err != nil {
				return nil, err
			}
			out[k] = converted
		}
		return out, nil
	default:
		return v, nil
	}
}

// orderedMapToJSON converts an *OrderedMap into a json-ordered-encoder-
// compatible structure. We build the JSON object string manually to guarantee
// key order. Leaf values are marshaled using an encoder with HTML escaping
// disabled to match the existing serializeStructuredContent behavior.
func orderedMapToJSON(om *OrderedMap) (json.RawMessage, error) {
	if om == nil || om.inner == nil {
		return json.RawMessage("null"), nil
	}

	var buf bytes.Buffer
	buf.WriteByte('{')

	first := true
	for pair := om.inner.Oldest(); pair != nil; pair = pair.Next() {
		if !first {
			buf.WriteByte(',')
		}
		first = false

		// Encode key.
		keyBytes, err := marshalJSONNoEscape(pair.Key)
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to marshal JSON key %q", pair.Key), err)
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')

		// Encode value.
		val, err := orderedToJSONValue(pair.Value)
		if err != nil {
			return nil, err
		}
		valBytes, err := marshalJSONNoEscape(val)
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to marshal JSON value for key %q", pair.Key), err)
		}
		buf.Write(valBytes)
	}

	buf.WriteByte('}')
	return json.RawMessage(buf.String()), nil
}

// marshalJSONNoEscape marshals v to JSON bytes with HTML escaping disabled.
func marshalJSONNoEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder.Encode appends a trailing newline; trim it.
	result := buf.Bytes()
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result, nil
}

// SerializeYAMLOrdered serializes data to a YAML string. When data is an
// *OrderedMap, keys are emitted in insertion order by constructing a yaml.Node
// tree. For regular map[string]interface{} and other types, the standard
// yaml.Marshal is used as a fallback.
//
// A trailing newline is always included to match existing YAML behavior.
func SerializeYAMLOrdered(data interface{}) (string, error) {
	node, err := orderedToYAMLNode(data)
	if err != nil {
		return "", err
	}

	// If the value was handled by fallback (returned nil node), use standard
	// yaml.Marshal.
	if node == nil {
		b, err := yaml.Marshal(data)
		if err != nil {
			return "", errors.NewTool("ordered", "failed to marshal YAML", err)
		}
		result := string(b)
		if len(result) == 0 || result[len(result)-1] != '\n' {
			result += "\n"
		}
		return result, nil
	}

	// Wrap in a DocumentNode for clean output.
	docNode := &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{node},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(docNode); err != nil {
		return "", errors.NewTool("ordered", "failed to encode YAML", err)
	}

	result := buf.String()
	if len(result) == 0 || result[len(result)-1] != '\n' {
		result += "\n"
	}
	return result, nil
}

// orderedToYAMLNode converts a Go value into a *yaml.Node tree. Returns nil
// when the value should be handled by the standard yaml.Marshal fallback.
func orderedToYAMLNode(v interface{}) (*yaml.Node, error) {
	switch typed := v.(type) {
	case *OrderedMap:
		return orderedMapToYAMLNode(typed)
	case []interface{}:
		return sliceToYAMLNode(typed)
	case map[string]interface{}:
		// For regular maps, also build an ordered node tree (sorted keys) for
		// consistent output.
		return mapToYAMLNode(typed)
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: typed}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(typed)}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(typed)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(typed, 10)}, nil
	case float64:
		return floatToYAMLNode(typed), nil
	default:
		// For any other type, let the standard yaml.Marshal handle it.
		return nil, nil
	}
}

// orderedMapToYAMLNode converts an *OrderedMap into a yaml.MappingNode with
// Content pairs in insertion order.
func orderedMapToYAMLNode(om *OrderedMap) (*yaml.Node, error) {
	if om == nil || om.inner == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}

	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	for pair := om.inner.Oldest(); pair != nil; pair = pair.Next() {
		// Key node.
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: pair.Key,
		}
		node.Content = append(node.Content, keyNode)

		// Value node.
		valNode, err := orderedToYAMLNode(pair.Value)
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to convert value for key %q", pair.Key), err)
		}
		if valNode == nil {
			// Fallback: marshal the value and embed it as a scalar.
			b, mErr := yaml.Marshal(pair.Value)
			if mErr != nil {
				return nil, errors.NewTool("ordered", fmt.Sprintf("failed to marshal value for key %q", pair.Key), mErr)
			}
			// Trim trailing newlines from yaml.Marshal output.
			s := strings.TrimRight(string(b), "\n")
			valNode = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: s,
			}
		}
		node.Content = append(node.Content, valNode)
	}

	return node, nil
}

// sliceToYAMLNode converts a []interface{} into a yaml.SequenceNode.
func sliceToYAMLNode(items []interface{}) (*yaml.Node, error) {
	node := &yaml.Node{
		Kind: yaml.SequenceNode,
	}

	for i, item := range items {
		child, err := orderedToYAMLNode(item)
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to convert sequence element %d", i), err)
		}
		if child == nil {
			// Fallback: marshal and embed.
			b, mErr := yaml.Marshal(item)
			if mErr != nil {
				return nil, errors.NewTool("ordered", fmt.Sprintf("failed to marshal sequence element %d", i), mErr)
			}
			s := strings.TrimRight(string(b), "\n")
			child = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: s,
			}
		}
		node.Content = append(node.Content, child)
	}

	return node, nil
}

// mapToYAMLNode converts a regular map[string]interface{} into a
// yaml.MappingNode with keys sorted alphabetically for deterministic output.
func mapToYAMLNode(m map[string]interface{}) (*yaml.Node, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	for _, key := range keys {
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: key,
		}
		node.Content = append(node.Content, keyNode)

		valNode, err := orderedToYAMLNode(m[key])
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to convert value for key %q", key), err)
		}
		if valNode == nil {
			b, mErr := yaml.Marshal(m[key])
			if mErr != nil {
				return nil, errors.NewTool("ordered", fmt.Sprintf("failed to marshal value for key %q", key), mErr)
			}
			s := strings.TrimRight(string(b), "\n")
			valNode = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: s,
			}
		}
		node.Content = append(node.Content, valNode)
	}

	return node, nil
}

// floatToYAMLNode creates a ScalarNode for a float64 value.
func floatToYAMLNode(f float64) *yaml.Node {
	if math.IsInf(f, 1) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}
	}
	if math.IsInf(f, -1) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-.inf"}
	}
	if math.IsNaN(f) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}
	}
	// Use strconv.FormatFloat with 'g' format for clean output.
	s := strconv.FormatFloat(f, 'g', -1, 64)
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}
}
