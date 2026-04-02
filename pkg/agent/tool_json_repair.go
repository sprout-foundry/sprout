package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var toolFailureDataURLPattern = regexp.MustCompile(`data:[^;\s]+;base64,[A-Za-z0-9+/=]+`)
var toolFailureBase64RunPattern = regexp.MustCompile(`[A-Za-z0-9+/=]{512,}`)
var jsonTrailingCommaPattern = regexp.MustCompile(`,(\s*[}\]])`)

func sanitizeToolFailureMessage(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return "unknown tool error"
	}

	msg = toolFailureDataURLPattern.ReplaceAllStringFunc(msg, func(m string) string {
		mime := "application/octet-stream"
		if semi := strings.Index(m, ";"); semi > len("data:") {
			mime = m[len("data:"):semi]
		}
		return "data:" + mime + ";base64,[REDACTED]"
	})

	msg = toolFailureBase64RunPattern.ReplaceAllString(msg, "[BASE64_REDACTED]")

	if len(msg) > maxToolFailureMessageChars {
		msg = msg[:maxToolFailureMessageChars] + "... (truncated)"
	}
	return msg
}

func parseToolArgumentsWithRepair(raw string) (map[string]interface{}, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false, fmt.Errorf("empty arguments")
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err == nil {
		return args, false, nil
	}

	candidates := repairJSONArgumentCandidates(raw)
	for _, candidate := range candidates {
		if candidate == raw {
			continue
		}
		var repaired map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &repaired); err == nil {
			return repaired, true, nil
		}
	}

	return nil, false, fmt.Errorf("invalid JSON arguments")
}

func repairJSONArgumentCandidates(raw string) []string {
	seen := make(map[string]struct{})
	candidates := make([]string, 0, 12)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}

	add(raw)
	withoutFence := stripMarkdownCodeFence(raw)
	add(withoutFence)

	for _, base := range []string{raw, withoutFence} {
		add(extractFirstBalancedJSONObject(base))
		add(extractOuterJSONObject(base))
	}

	initial := append([]string(nil), candidates...)
	for _, c := range initial {
		noCommas := removeJSONTrailingCommas(c)
		add(noCommas)
		add(closeJSONDelimiters(noCommas))
		add(closeJSONDelimiters(c))
	}

	return candidates
}

func stripMarkdownCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return trimmed
	}
	if !strings.HasPrefix(lines[0], "```") {
		return trimmed
	}
	end := len(lines)
	if strings.TrimSpace(lines[end-1]) == "```" {
		end--
	}
	if end <= 1 {
		return trimmed
	}
	return strings.Join(lines[1:end], "\n")
}

func extractOuterJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

func extractFirstBalancedJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escape := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

func removeJSONTrailingCommas(raw string) string {
	return jsonTrailingCommaPattern.ReplaceAllString(raw, "$1")
}

func closeJSONDelimiters(raw string) string {
	stack := make([]byte, 0, 16)
	inString := false
	escape := false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return raw
			}
			stack = stack[:len(stack)-1]
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return raw
			}
			stack = stack[:len(stack)-1]
		}
	}

	if len(stack) == 0 {
		return raw
	}

	var b strings.Builder
	b.WriteString(raw)
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	return b.String()
}
