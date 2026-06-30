package tools

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// SP-082-1: yaml.Node helpers for preserving key insertion order
// ---------------------------------------------------------------------------

// parseToYamlNode parses JSON or YAML content into a *yaml.Node, preserving
// the original key insertion order from the source text. yaml.v3's parser
// retains the order of map keys as they appear in the input, unlike
// map[string]interface{} which loses all ordering.
func parseToYamlNode(format, content string) (*yaml.Node, error) {
	var node yaml.Node
	switch format {
	case "json":
		// yaml.v3 can parse JSON into yaml.Node, preserving key order.
		if err := yaml.Unmarshal([]byte(content), &node); err != nil {
			return nil, fmt.Errorf("failed to parse json into yaml.Node: %w", err)
		}
	case "yaml":
		if err := yaml.Unmarshal([]byte(content), &node); err != nil {
			return nil, fmt.Errorf("failed to parse yaml into yaml.Node: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported structured format %q: use json or yaml", format)
	}
	return &node, nil
}

// serializeYamlNode serializes a *yaml.Node back to JSON or YAML, preserving
// the key order captured during parsing.
func serializeYamlNode(format string, node *yaml.Node) (string, error) {
	switch format {
	case "json":
		var buf bytes.Buffer
		if err := nodeToJSON(&buf, node); err != nil {
			return "", fmt.Errorf("failed to serialize yaml.Node to json: %w", err)
		}
		return strings.TrimRight(buf.String(), "\n"), nil
	case "yaml":
		// Wrap in a DocumentNode for clean output only if the node isn't
		// already one. parseToYamlNode returns a DocumentNode, so we need
		// to avoid double-wrapping.
		target := node
		if target.Kind != yaml.DocumentNode {
			target = &yaml.Node{
				Kind:    yaml.DocumentNode,
				Content: []*yaml.Node{node},
			}
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(target); err != nil {
			return "", fmt.Errorf("failed to serialize yaml.Node to yaml: %w", err)
		}
		return strings.TrimRight(buf.String(), "\n"), nil
	default:
		return "", fmt.Errorf("unsupported structured format %q: use json or yaml", format)
	}
}

// writeJSONString writes a JSON-escaped quoted string to buf.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if ch < 0x20 {
				fmt.Fprintf(buf, "\\u%04x", ch)
			} else {
				buf.WriteByte(ch)
			}
		}
	}
	buf.WriteByte('"')
}

// nodeToJSON writes a yaml.Node as indented JSON to buf, preserving key order.
func nodeToJSON(buf *bytes.Buffer, node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) > 0 {
			return nodeToJSON(buf, node.Content[0])
		}
		buf.WriteString("null")
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!null", "!!bool", "!!float", "!!int":
			buf.WriteString(node.Value)
		case "!!str":
			writeJSONString(buf, node.Value)
		default:
			// Fallback: treat as quoted string
			writeJSONString(buf, node.Value)
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, item := range node.Content {
			buf.WriteString("  ")
			if err := nodeToJSON(buf, item); err != nil {
				return err
			}
			if i < len(node.Content)-1 {
				buf.WriteString(",")
			}
			buf.WriteString("\n")
		}
		buf.WriteString("]")
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			buf.WriteString("  ")
			writeJSONString(buf, keyNode.Value)
			buf.WriteString(": ")
			if err := nodeToJSON(buf, valNode); err != nil {
				return err
			}
			if i < len(node.Content)-2 {
				buf.WriteString(",")
			}
			buf.WriteString("\n")
		}
		buf.WriteString("}")
	default:
		return fmt.Errorf("unsupported yaml.Node kind: %d", node.Kind)
	}
	return nil
}

// mapToYamlNode converts a map[string]interface{} (with nested maps/slices)
// into a *yaml.Node, preserving key insertion order from map iteration.
func mapToYamlNode(v interface{}) *yaml.Node {
	switch typed := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: typed}
	case bool:
		bv := "false"
		if typed {
			bv = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: bv}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}
	case int8:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}
	case int16:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}
	case int32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(typed, 10)}
	case uint:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}
	case uint8:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}
	case uint16:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}
	case uint32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(uint64(typed), 10)}
	case uint64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(typed, 10)}
	case float64:
		if typed == float64(int64(typed)) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(typed), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(typed, 'f', -1, 64)}
	case float32:
		fv := float64(typed)
		if fv == float64(int64(fv)) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(fv), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(fv, 'f', -1, 32)}
	case []interface{}:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			seq.Content = append(seq.Content, mapToYamlNode(item))
		}
		return seq
	case map[string]interface{}:
		mp := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for k, val := range typed {
			mp.Content = append(mp.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				mapToYamlNode(val),
			)
		}
		return mp
	default:
		// Fallback: convert to string representation
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(typed)}
	}
}

// stripJSONStyle recursively clears Style flags on all nodes.  When
// yaml.Unmarshal() parses JSON into a *yaml.Node, it sets Style=32
// (yaml.JSONStyle) on every node.  If those nodes are later encoded as
// YAML, the JSONStyle persists and produces JSON-style output (e.g.
// `"key": value` instead of `key: value`).  This function resets all
// styles to zero so the encoder emits native YAML syntax.
func stripJSONStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		stripJSONStyle(child)
	}
}

// serializeWithOrder parses the raw JSON args, extracts the "data" sub-object,
// and serializes it using yaml.Node to preserve key insertion order. Returns
// the serialized content for the given format (json or yaml).
func serializeWithOrder(rawArgsJSON, format string) (string, error) {
	node, err := parseToYamlNode("json", rawArgsJSON)
	if err != nil {
		return "", fmt.Errorf("failed to parse raw args JSON: %w", err)
	}
	dataNode := findKeyInMapping(node, "data")
	if dataNode == nil {
		return "", fmt.Errorf("could not find 'data' key in raw args JSON")
	}
	// When serializing to YAML, strip JSONStyle flags that yaml.Unmarshal
	// sets when parsing JSON source.  Without this the encoder emits JSON
	// syntax (`"key": value`) instead of native YAML (`key: value`).
	if format == "yaml" {
		stripJSONStyle(dataNode)
	}
	return serializeYamlNode(format, dataNode)
}

// findKeyInMapping looks up a key in the top-level mapping of a yaml.Node
// document, returning the value node or nil.
func findKeyInMapping(doc *yaml.Node, key string) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
