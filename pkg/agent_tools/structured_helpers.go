package tools

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Structured file helpers (extracted from pkg/agent/tool_handlers_structured.go
// so both the new ToolHandler path and any legacy code can use them).
// ---------------------------------------------------------------------------

const maxStructuredErrorDetails = 8

type jsonPatchOperation struct {
	Op    string
	Path  string
	From  string
	Value interface{}
}

// inferStructuredFormat determines the format from the file extension or
// the provided format string. Returns "" when the extension is not supported.
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

// serializeStructuredContent serializes data to the given format.
func serializeStructuredContent(format string, data interface{}) (string, error) {
	enc, err := newStructuredEncoder(format)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := enc.Encode(&buf, data); err != nil {
		return "", fmt.Errorf("failed to serialize %s: %w", format, err)
	}
	out := buf.String()
	// YAML already ends with \n; JSON via json.Encoder adds a trailing \n.
	out = strings.TrimRight(out, "\n")
	return out, nil
}

// deserializeStructuredContent deserializes content from the given format.
func deserializeStructuredContent(format, content string) (interface{}, error) {
	dec, err := newStructuredDecoder(format)
	if err != nil {
		return nil, err
	}
	var doc interface{}
	if err := dec.Decode([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("failed to deserialize %s: %w", format, err)
	}
	return normalizeYAMLValue(doc), nil
}

// structuredEncoder writes data in a given format.
type structuredEncoder struct {
	format string
}

func newStructuredEncoder(format string) (*structuredEncoder, error) {
	switch format {
	case "json", "yaml":
		return &structuredEncoder{format: format}, nil
	default:
		return nil, fmt.Errorf("unsupported structured format %q: use json or yaml", format)
	}
}

func (e *structuredEncoder) Encode(buf *bytes.Buffer, data interface{}) error {
	switch e.format {
	case "json":
		enc := newJSONEncoder(buf)
		return enc.Encode(data)
	case "yaml":
		b, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", e.format)
	}
}

// structuredDecoder reads data from a given format.
type structuredDecoder struct {
	format string
}

func newStructuredDecoder(format string) (*structuredDecoder, error) {
	switch format {
	case "json", "yaml":
		return &structuredDecoder{format: format}, nil
	default:
		return nil, fmt.Errorf("unsupported structured format %q: use json or yaml", format)
	}
}

func (d *structuredDecoder) Decode(data []byte, out interface{}) error {
	switch d.format {
	case "json":
		return newJSONDecoder(data).Decode(out)
	case "yaml":
		return yaml.Unmarshal(data, out)
	default:
		return fmt.Errorf("unsupported format: %s", d.format)
	}
}

// NOTE: The jsonEnc / doJSONMarshal / doJSONUnmarshal types bridge the
// encoding/json package so that this file can live in pkg/agent_tools
// without pulling in the full encoding/json dependency in a way that
// conflicts with the structured file handling in pkg/agent.
//
// This mirrors the pattern used by the existing tool_handlers_structured.go
// in the agent package, but adapted for the ToolHandler interface.

// We use a tiny indirection layer here so the handlers in this package can
// call serializeStructuredContent / deserializeStructuredContent without
// importing encoding/json directly.  The functions are implemented in
// structured_json.go (build-tagged for !js) and structured_js.go (js/wasm).

type jsonEnc struct {
	buf *bytes.Buffer
}

func newJSONEnc(buf *bytes.Buffer) *jsonEnc {
	return &jsonEnc{buf: buf}
}

// Encode is an indirection to encoding/json.  Implemented in structured_json.go / structured_js.go.
func (e *jsonEnc) Encode(v interface{}) error {
	return doJSONEncode(e.buf, v)
}

func doJSONEncode(buf *bytes.Buffer, v interface{}) error {
	// Defer to the platform-specific implementation.
	return platformJSONEncode(buf, v)
}

func doJSONUnmarshal(data []byte, v interface{}) error {
	return platformJSONUnmarshal(data, v)
}

// normalizeYAMLValue recursively converts map[interface{}]interface{} maps
// (produced by yaml.Unmarshal) to map[string]interface{}.
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
