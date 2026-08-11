package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	lfm2ToolCallStart = "<|tool_call_start|>"
	lfm2ToolCallEnd   = "<|tool_call_end|>"
)

// lfm2ToolCallRe matches a single Pythonic tool call: name(args)
// inside <|tool_call_start|> / <|tool_call_end|> markers.
var lfm2ToolCallRe = regexp.MustCompile(
	`\Q` + lfm2ToolCallStart + `\E\s*\[([\s\S]*?)\]\s*\Q` + lfm2ToolCallEnd + `\E`,
)

// RecoverLFM2ToolCalls extracts Liquid AI LFM2-style tool calls from the
// assistant message content. LFM2 emits Pythonic syntax between special tokens:
//
//	<|tool_call_start|>[read_file(path='/some/file.go')]<|tool_call_end|>
//	<|tool_call_start|>[get_weather(city='Paris')]<|tool_call_end|>
//
// Multiple calls may appear sequentially. Returns the recovered calls, the
// content with markers+payloads stripped, and ok=true when at least one call
// was parsed. When ok is false, content is returned unchanged.
func RecoverLFM2ToolCalls(content string) (calls []ToolCall, remaining string, ok bool) {
	matches := lfm2ToolCallRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, content, false
	}

	for i, m := range matches {
		payload := content[m[2]:m[3]]
		tc, parsed := parseLFM2PythonicCall(payload)
		if !parsed {
			continue
		}
		tc.ID = fmt.Sprintf("call_lfm2_%d", i)
		tc.Type = "function"
		calls = append(calls, tc)
	}

	if len(calls) == 0 {
		return nil, content, false
	}

	remaining = lfm2ToolCallRe.ReplaceAllString(content, "")
	remaining = strings.TrimSpace(remaining)
	return calls, remaining, true
}

// parseLFM2PythonicCall parses a single Pythonic function call like:
//
//	read_file(path='/some/file.go')
//	get_weather(city='Paris', unit='celsius')
//
// into a ToolCall with JSON arguments.
func parseLFM2PythonicCall(payload string) (ToolCall, bool) {
	payload = strings.TrimSpace(payload)

	// Find the opening paren.
	parenIdx := strings.IndexByte(payload, '(')
	if parenIdx <= 0 {
		return ToolCall{}, false
	}
	name := strings.TrimSpace(payload[:parenIdx])
	if !isLikelyToolName(name) {
		return ToolCall{}, false
	}

	// Extract balanced parentheses body.
	body, ok := extractParenBody(payload[parenIdx:])
	if !ok {
		return ToolCall{}, false
	}

	args := parseLFM2Args(body)
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return ToolCall{}, false
	}

	return ToolCall{
		Function: ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}, true
}

// extractParenBody returns the content inside the outermost (…), starting from s[0]=='('.
func extractParenBody(s string) (string, bool) {
	if len(s) == 0 || s[0] != '(' {
		return "", false
	}
	depth := 0
	inStr := false
	strQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == strQuote {
				inStr = false
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = true
			strQuote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// lfm2ArgRe matches a single key=value pair where the value is a quoted string.
// Handles escaped quotes inside the value.
var lfm2ArgRe = regexp.MustCompile(`(\w+)\s*=\s*`)

// parseLFM2Args parses Pythonic keyword arguments like:
// path='/some/file.go', content='hello\nworld', count=3
func parseLFM2Args(body string) map[string]interface{} {
	result := make(map[string]interface{})
	if strings.TrimSpace(body) == "" {
		return result
	}

	idx := 0
	for idx < len(body) {
		// Skip whitespace and commas.
		for idx < len(body) && (body[idx] == ' ' || body[idx] == ',' || body[idx] == '\t' || body[idx] == '\n') {
			idx++
		}
		if idx >= len(body) {
			break
		}

		// Match key=
		loc := lfm2ArgRe.FindStringIndex(body[idx:])
		if loc == nil {
			break
		}
		relStart := idx + loc[0]
		relEnd := idx + loc[1]
		key := body[relStart : idx+loc[1]-1]
		key = strings.TrimSpace(strings.TrimSuffix(key, "="))
		key = strings.TrimSpace(key)

		idx = relEnd

		// Skip whitespace.
		for idx < len(body) && body[idx] == ' ' {
			idx++
		}
		if idx >= len(body) {
			break
		}

		val, nextIdx := parseLFM2Value(body, idx)
		result[key] = val
		idx = nextIdx
	}

	return result
}

// parseLFM2Value parses a single value starting at body[idx]. Returns the
// parsed value (Go type) and the index past the consumed characters.
func parseLFM2Value(body string, idx int) (interface{}, int) {
	if idx >= len(body) {
		return nil, idx
	}

	c := body[idx]

	// Quoted string.
	if c == '\'' || c == '"' {
		return parseLFM2QuotedString(body, idx)
	}

	// Number (int or float).
	rest := body[idx:]
	end := 0
	for end < len(rest) {
		ch := rest[end]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '+' || ch == 'e' || ch == 'E' {
			end++
		} else {
			break
		}
	}
	if end > 0 {
		numStr := rest[:end]
		if !strings.Contains(numStr, ".") {
			var i int
			if _, err := fmt.Sscanf(numStr, "%d", &i); err == nil {
				return i, idx + end
			}
		}
		var f float64
		if _, err := fmt.Sscanf(numStr, "%f", &f); err == nil {
			return f, idx + end
		}
	}

	// Boolean / None.
	rest = body[idx:]
	if strings.HasPrefix(rest, "True") {
		return true, idx + 4
	}
	if strings.HasPrefix(rest, "False") {
		return false, idx + 5
	}
	if strings.HasPrefix(rest, "None") {
		return nil, idx + 4
	}

	// Bareword — treat as string up to comma or end.
	end = 0
	for end < len(rest) && rest[end] != ',' && rest[end] != ')' {
		end++
	}
	return strings.TrimSpace(rest[:end]), idx + end
}

// parseLFM2QuotedString parses a Python single- or double-quoted string,
// handling \n, \t, \\, \', \" escapes. Returns the unescaped value and the
// index past the closing quote.
func parseLFM2QuotedString(body string, idx int) (string, int) {
	if idx >= len(body) {
		return "", idx
	}
	quote := body[idx]
	var sb strings.Builder
	i := idx + 1
	for i < len(body) {
		c := body[i]
		if c == '\\' && i+1 < len(body) {
			next := body[i+1]
			switch next {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\\':
				sb.WriteByte('\\')
			case '\'':
				sb.WriteByte('\'')
			case '"':
				sb.WriteByte('"')
			default:
				sb.WriteByte('\\')
				sb.WriteByte(next)
			}
			i += 2
			continue
		}
		if c == quote {
			return sb.String(), i + 1
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String(), i
}
