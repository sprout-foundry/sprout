package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// mistralToolCallsMarker is the sentinel Mistral-family models emit inside the
// assistant message content when a provider/endpoint doesn't translate their
// native tool calls into the OpenAI-style structured tool_calls field.
const mistralToolCallsMarker = "[TOOL_CALLS]"

// RecoverMistralToolCalls recovers tool calls from the Mistral-family text
// format, where the model emits `[TOOL_CALLS]…` inside the message content
// instead of the structured tool_calls field. It returns the recovered calls
// and the content with the marker and its payload stripped. ok is false when no
// marker is present or nothing parseable follows it (content is returned
// unchanged in that case).
//
// Two payload shapes are handled:
//
//	[TOOL_CALLS][{"name":"f","arguments":{…}}, …]   (JSON array — Mistral native)
//	[TOOL_CALLS]f{…}g{…}                             (name + JSON object, repeated)
func RecoverMistralToolCalls(content string) (calls []ToolCall, remaining string, ok bool) {
	before, payload, found := strings.Cut(content, mistralToolCallsMarker)
	if !found {
		return nil, content, false
	}
	before = strings.TrimSpace(before)
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, content, false
	}

	if strings.HasPrefix(payload, "[") {
		calls = parseMistralArray(payload)
	} else {
		calls = parseMistralNameObjects(payload)
	}
	if len(calls) == 0 {
		return nil, content, false
	}
	for i := range calls {
		calls[i].ID = fmt.Sprintf("call_mistral_%d", i)
		calls[i].Type = "function"
	}
	return calls, before, true
}

// parseMistralArray parses the native `[{"name":…,"arguments":…}, …]` shape.
func parseMistralArray(payload string) []ToolCall {
	var raw []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	// Decode the leading JSON array, tolerating trailing text after it.
	if err := json.NewDecoder(strings.NewReader(payload)).Decode(&raw); err != nil {
		return nil
	}
	var calls []ToolCall
	for _, r := range raw {
		if r.Name == "" {
			continue
		}
		calls = append(calls, ToolCall{
			Function: ToolCallFunction{Name: r.Name, Arguments: argsToString(r.Arguments)},
		})
	}
	return calls
}

// parseMistralNameObjects parses the `name{json}name2{json}` shape, where one or
// more calls are concatenated as a tool name followed by its JSON arguments.
func parseMistralNameObjects(payload string) []ToolCall {
	var calls []ToolCall
	s := payload
	for {
		s = strings.TrimSpace(s)
		brace := strings.IndexByte(s, '{')
		if brace < 0 {
			break
		}
		name := strings.Trim(s[:brace], " ,\n\t\r")
		if !isLikelyToolName(name) {
			break
		}
		obj, end := extractJSONObject(s[brace:])
		if obj == "" {
			break
		}
		calls = append(calls, ToolCall{
			Function: ToolCallFunction{Name: name, Arguments: obj},
		})
		s = s[brace+end:]
	}
	return calls
}

// argsToString normalizes a raw arguments value to the JSON string downstream
// tool execution expects. Objects/arrays are used as-is; a double-encoded JSON
// string is unwrapped; empty becomes "{}".
func argsToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// extractJSONObject returns the balanced-brace JSON object starting at s[0]=='{'
// and the index just past it, or ("", 0) if s doesn't start with a valid object.
func extractJSONObject(s string) (string, int) {
	if len(s) == 0 || s[0] != '{' {
		return "", 0
	}
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				obj := s[:i+1]
				if json.Valid([]byte(obj)) {
					return obj, i + 1
				}
				return "", 0
			}
		}
	}
	return "", 0
}

// isLikelyToolName guards parseMistralNameObjects against treating arbitrary
// prose that contains a brace as a tool call: a real tool name is a short
// identifier (letters, digits, _ and -).
func isLikelyToolName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
