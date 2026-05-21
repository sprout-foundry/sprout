package tools

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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

// JSON helpers (package-private, not exported).
type jsonEncoder struct {
	enc *jsonEnc
}

func newJSONEncoder(buf *bytes.Buffer) *jsonEncoder {
	return &jsonEncoder{enc: newJSONEnc(buf)}
}

func (e *jsonEncoder) Encode(v interface{}) error {
	return e.enc.Encode(v)
}

type jsonDecoder struct {
	data []byte
}

func newJSONDecoder(data []byte) *jsonDecoder {
	return &jsonDecoder{data: data}
}

func (d *jsonDecoder) Decode(v interface{}) error {
	return doJSONUnmarshal(d.data, v)
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

// ---------------------------------------------------------------------------
// Schema validation helpers
// ---------------------------------------------------------------------------

// toSchemaMap converts a raw interface{} to a map for schema validation.
func toSchemaMap(v interface{}) (map[string]interface{}, error) {
	schema, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("parameter 'schema' must be an object")
	}
	return schema, nil
}

// validateDataAgainstSchema validates data against a JSON Schema subset.
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

func formatStructuredValidationError(toolName string, errs []string, context string) error {
	if len(errs) == 0 {
		return fmt.Errorf("schema validation failed: no error details provided")
	}

	paths := extractValidationPaths(errs)
	pathSummary := strings.Join(limitStrings(paths, maxStructuredErrorDetails), ", ")
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
