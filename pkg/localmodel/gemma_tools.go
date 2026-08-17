//go:build darwin && arm64 && cgo

package localmodel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Gemma-4 native tool-calling format, per the model's own
// chat_template.jinja (Google Gemma canonical template, 2026-07-09):
//
//	declarations: <|tool>declaration:name{...}<tool|>      (system turn)
//	calls:         <|tool_call>call:name{key:value}<tool_call|>
//	responses:     <|tool_response>response:name{value:<|"|>…<|"|>}<tool_response|>
//
// String values are quoted with the atomic <|"|> token; object keys are
// bare; booleans/numbers are literal. Stock (untuned) gemma-4 models were
// trained on exactly this — the Qwen XML format broke them (doubled calls,
// mangled parameters, path doubling). Tuned variants are being retrained
// onto this same format, so it is the only format wired here.

const gemmaArch = "gemma4_text"

// gemmaQuote wraps s in the atomic string-quote token pair.
func gemmaQuote(s string) string { return `<|"|>` + s + `<|"|>` }

// gemmaFormatValue renders v in native argument syntax (escape_keys=false:
// nested object keys stay bare, matching format_argument in the template).
func gemmaFormatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return gemmaQuote(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'g', -1, 64)
	case int:
		return strconv.Itoa(val)
	case map[string]interface{}:
		return gemmaFormatMapping(val)
	case []interface{}:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = gemmaFormatValue(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case nil:
		return "null"
	default:
		j, _ := json.Marshal(val)
		return gemmaQuote(string(j))
	}
}

func gemmaFormatMapping(m map[string]interface{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // template dictsort
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ":" + gemmaFormatValue(m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// gemmaToolDeclarations renders the <|tool>…<tool|> block for the system
// turn, one declaration per tool, sorted by name (template dictsort over
// properties; tools render in the order given).
func gemmaToolDeclarations(tools []api.Tool) string {
	var sb strings.Builder
	for _, tool := range tools {
		sb.WriteString("<|tool>declaration:")
		sb.WriteString(tool.Function.Name)
		sb.WriteString("{description:")
		sb.WriteString(gemmaQuote(tool.Function.Description))
		if params := gemmaParameters(tool.Function.Parameters); params != "" {
			sb.WriteString(",parameters:")
			sb.WriteString(params)
		}
		sb.WriteString("}<tool|>")
	}
	return sb.String()
}

// gemmaParameters renders a JSON-schema parameters object in native
// declaration syntax: properties (dictsort) first, then required, then type
// — mirroring format_function_declaration.
func gemmaParameters(params interface{}) string {
	if params == nil {
		return ""
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("{")
	if props, ok := m["properties"].(map[string]interface{}); ok && len(props) > 0 {
		sb.WriteString("properties:{")
		sb.WriteString(gemmaProperties(props))
		sb.WriteString("}")
	}
	if req, ok := m["required"].([]interface{}); ok && len(req) > 0 {
		if sb.Len() > 1 {
			sb.WriteString(",")
		}
		sb.WriteString("required:[")
		for i, r := range req {
			if i > 0 {
				sb.WriteString(",")
			}
			if s, ok := r.(string); ok {
				sb.WriteString(gemmaQuote(s))
			}
		}
		sb.WriteString("]")
	}
	if t, ok := m["type"].(string); ok {
		if sb.Len() > 1 {
			sb.WriteString(",")
		}
		sb.WriteString("type:")
		sb.WriteString(gemmaQuote(strings.ToUpper(t)))
	}
	sb.WriteString("}")
	return sb.String()
}

// gemmaProperties renders each property as key:{description,type} with the
// template's field order (description, enum/items, type).
func gemmaProperties(props map[string]interface{}) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		prop, _ := props[k].(map[string]interface{})
		var sb strings.Builder
		sb.WriteString(k)
		sb.WriteString(":{")
		if d, ok := prop["description"].(string); ok && d != "" {
			sb.WriteString("description:")
			sb.WriteString(gemmaQuote(d))
		}
		if t, ok := prop["type"].(string); ok {
			if sb.Len() > len(k)+2 {
				sb.WriteString(",")
			}
			sb.WriteString("type:")
			sb.WriteString(gemmaQuote(strings.ToUpper(t)))
		}
		sb.WriteString("}")
		parts[i] = sb.String()
	}
	return strings.Join(parts, ",")
}

// gemmaFormatAssistantToolCalls renders prior assistant tool_calls for
// conversation replay. Multiple calls concatenate with no separator, per
// the template's tool_calls loop.
func gemmaFormatAssistantToolCalls(toolCalls []api.ToolCall) string {
	var sb strings.Builder
	for _, tc := range toolCalls {
		args := map[string]interface{}{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		sb.WriteString("<|tool_call>call:")
		sb.WriteString(tc.Function.Name)
		sb.WriteString(gemmaFormatMapping(args))
		sb.WriteString("<tool_call|>")
	}
	return sb.String()
}

// gemmaFormatToolResponse renders a tool result block (continues the
// model's open turn — no turn markers around it).
func gemmaFormatToolResponse(name, content string) string {
	return "<|tool_response>response:" + name + "{value:" + gemmaQuote(content) + "}<tool_response|>"
}

// gemmaStripThinking removes thought-channel spans the model may emit
// (<|channel>thought\n…<channel|>), mirroring the template's strip_thinking
// macro applied to assistant content.
func gemmaStripThinking(text string) string {
	if !strings.Contains(text, "<|channel>") {
		return text
	}
	var sb strings.Builder
	rest := text
	for {
		i := strings.Index(rest, "<|channel>thought")
		if i < 0 {
			sb.WriteString(rest)
			return sb.String()
		}
		sb.WriteString(rest[:i])
		rest = rest[i:]
		end := strings.Index(rest, "<channel|>")
		if end < 0 {
			return sb.String() // unterminated thought: drop the rest
		}
		rest = rest[end+len("<channel|>"):]
	}
}

// parseGemmaToolCalls extracts native <|tool_call> blocks from model
// output. Returns visible content (thinking stripped, text before the
// first call) and parsed calls. Stock models occasionally hallucinate a
// wrong opener (<|tool_response> instead of <|tool_call>) ahead of an
// otherwise well-formed call:NAME{…} body — recognize the call body
// wherever a plausible opener appears, not just the exact token.
func parseGemmaToolCalls(text string) (string, []api.ToolCall) {
	if !strings.Contains(text, "<|tool_call>") && !strings.Contains(text, "<|channel>thought") &&
		!strings.Contains(text, "call:") {
		return text, nil
	}
	content := gemmaStripThinking(text)
	var calls []api.ToolCall
	rest := content
	for {
		openerStart, bodyStart := gemmaFindCallStart(rest)
		if openerStart < 0 {
			break
		}
		if len(calls) == 0 {
			content = content[:openerStart]
		}
		rest = rest[bodyStart:]

		call, remainder, ok := parseGemmaOneCall(rest)
		if !ok {
			break
		}
		calls = append(calls, call)
		rest = remainder
	}
	if len(calls) == 0 {
		return strings.TrimSpace(content), nil
	}
	return strings.TrimSpace(content), calls
}

// gemmaFindCallStart locates the next plausible call opener: a known
// opener token (<|tool_call>, or the hallucinated <|tool_response>)
// directly followed by "call:". Returns the opener's start index (for
// content slicing) and the index of the call body (for parsing).
func gemmaFindCallStart(rest string) (int, int) {
	for _, opener := range []string{"<|tool_call>", "<|tool_response>"} {
		idx := strings.Index(rest, opener)
		if idx >= 0 && strings.HasPrefix(rest[idx+len(opener):], "call:") {
			return idx, idx + len(opener)
		}
	}
	return -1, -1
}

// parseGemmaOneCall parses "call:NAME{ARGS}" up to and including the
// closing <tool_call|>. Returns the parsed call and the text after it.
func parseGemmaOneCall(rest string) (api.ToolCall, string, bool) {
	if !strings.HasPrefix(rest, "call:") {
		return api.ToolCall{}, rest, false
	}
	rest = rest[len("call:"):]
	brace := strings.Index(rest, "{")
	if brace < 0 {
		return api.ToolCall{}, rest, false
	}
	name := strings.TrimSpace(rest[:brace])
	if name == "" {
		return api.ToolCall{}, rest, false
	}
	argsStr, after, ok := gemmaScanValue(rest[brace:], 0)
	if !ok {
		return api.ToolCall{}, rest, false
	}
	after = strings.TrimPrefix(after, "<tool_call|>")

	args, ok := gemmaParseArgs(argsStr)
	if !ok {
		args = map[string]interface{}{}
	}
	argsJSON, _ := json.Marshal(args)
	return api.ToolCall{
		ID:   fmt.Sprintf("call_gemma_%d", time.Now().UnixNano()),
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}, after, true
}

// gemmaParseArgs parses the native argument mapping ("{k:v,k2:v2}") into a
// Go map. Values keep their types: quoted strings, bools, numbers, nested
// mappings and arrays.
func gemmaParseArgs(s string) (map[string]interface{}, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, false
	}
	s = s[1 : len(s)-1]
	out := map[string]interface{}{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, ", \t\n")
		colon := strings.Index(s, ":")
		if colon < 0 {
			return nil, false
		}
		key := strings.TrimSpace(s[:colon])
		s = s[colon+1:]
		valStr, after, ok := gemmaScanValue(s, 0)
		if !ok {
			return nil, false
		}
		val, ok := gemmaParseValue(valStr)
		if !ok {
			return nil, false
		}
		out[key] = val
		s = after
	}
	return out, len(out) > 0
}

// gemmaParseValue converts one native-syntax value to a Go value.
func gemmaParseValue(s string) (interface{}, bool) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, `<|"|>`):
		end := strings.Index(s[5:], `<|"|>`)
		if end < 0 {
			return nil, false
		}
		return s[5 : 5+end], true
	case s == "true":
		return true, true
	case s == "false":
		return false, true
	case s == "null":
		return nil, true
	case strings.HasPrefix(s, "{"), strings.HasPrefix(s, "["):
		// Nested structures: re-render through JSON to keep them typed.
		var v interface{}
		if err := json.Unmarshal([]byte(gemmaToJSON(s)), &v); err != nil {
			return s, true // fall back to raw string
		}
		return v, true
	default:
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n, true
		}
		return s, true // bare string fallback
	}
}

// gemmaToJSON converts native syntax to JSON text (quote bare keys and
// <|"|>-quoted strings).
func gemmaToJSON(s string) string {
	var sb strings.Builder
	i := 0
	expectKey := false
	for i < len(s) {
		c := s[i]
		switch {
		case c == '{':
			sb.WriteByte('{')
			expectKey = true
			i++
		case c == '[':
			sb.WriteByte('[')
			i++
		case c == '}' || c == ']':
			sb.WriteByte(c)
			i++
		case c == ',':
			sb.WriteByte(',')
			// After a comma inside an object, a key follows.
			expectKey = strings.LastIndexByte(sb.String(), '{') > strings.LastIndexByte(sb.String(), '[')
			i++
		case strings.HasPrefix(s[i:], `<|"|>`):
			j := i + 5
			for j < len(s) && !strings.HasPrefix(s[j:], `<|"|>`) {
				j++
			}
			sb.WriteString(strconv.Quote(s[i+5 : j]))
			i = j + 5
		case c == ':':
			sb.WriteByte(':')
			expectKey = false
			i++
		case c == ' ' || c == '\t' || c == '\n':
			i++
		default:
			if expectKey {
				j := i
				for j < len(s) && s[j] != ':' && s[j] != ',' && s[j] != '}' {
					j++
				}
				sb.WriteString(strconv.Quote(strings.TrimSpace(s[i:j])))
				i = j
			} else {
				j := i
				for j < len(s) && s[j] != ',' && s[j] != '}' && s[j] != ']' {
					j++
				}
				tok := strings.TrimSpace(s[i:j])
				if tok == "true" || tok == "false" || tok == "null" {
					sb.WriteString(tok)
				} else if _, err := strconv.ParseFloat(tok, 64); err == nil {
					sb.WriteString(tok)
				} else {
					sb.WriteString(strconv.Quote(tok))
				}
				i = j
			}
		}
	}
	return sb.String()
}

// gemmaScanValue extracts the substring of one complete value starting at
// s plus the remainder after it. Handles quoted strings, nested {} / []
// structures, and bare literals (true/false/null/numbers) which end at the
// next ',' or '}' at the caller's nesting level.
func gemmaScanValue(s string, depth int) (string, string, bool) {
	switch {
	case strings.HasPrefix(s, `<|"|>`):
		end := strings.Index(s[5:], `<|"|>`)
		if end < 0 {
			return "", s, false
		}
		return s[:5+end+5], s[5+end+5:], true
	case strings.HasPrefix(s, "{"), strings.HasPrefix(s, "["):
		open := s[0]
		closeCh := byte('}')
		if open == '[' {
			closeCh = ']'
		}
		d := depth + 1
		i := 1
		for i < len(s) {
			if strings.HasPrefix(s[i:], `<|"|>`) {
				end := strings.Index(s[i+5:], `<|"|>`)
				if end < 0 {
					return "", s, false
				}
				i += 5 + end + 5
				continue
			}
			switch s[i] {
			case open:
				d++
			case closeCh:
				d--
				if d == 0 {
					return s[:i+1], s[i+1:], true
				}
			}
			i++
		}
		return "", s, false
	case strings.HasPrefix(s, "true"):
		return "true", s[4:], true
	case strings.HasPrefix(s, "false"):
		return "false", s[5:], true
	case strings.HasPrefix(s, "null"):
		return "null", s[4:], true
	default:
		// Bare number or unquoted token: runs to the next delimiter.
		j := 0
		for j < len(s) && s[j] != ',' && s[j] != '}' && s[j] != ']' {
			j++
		}
		if j == 0 {
			return "", s, false
		}
		return s[:j], s[j:], true
	}
}
