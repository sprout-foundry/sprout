//go:build darwin && arm64 && cgo

package localmodel

// local_provider_toolcalls.go — the LocalProvider tool-call parsing +
// tool-prompt formatting: the local / Qwen tool-call parsers, the tool-call
// body + inline-param / simple-ident helpers, and the tools-prompt formatters
// (formatToolsPrompt, formatQwenToolsPrompt). Split out of local_provider.go.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// parseLocalToolCalls parses tool calls from model output using the
// architecture-appropriate parser. Returns content with tool calls
// stripped, and the parsed tool calls in OpenAI format.
func parseLocalToolCalls(arch, text string) (string, []api.ToolCall) {
	switch arch {
	case "lfm2":
		calls, remaining, ok := api.RecoverLFM2ToolCalls(text)
		if !ok {
			return text, nil
		}
		return remaining, calls
	case gemmaArch:
		return parseGemmaToolCalls(text)
	default:
		return parseQwenToolCalls(text)
	}
}

// parseQwenToolCalls extracts Qwen-style tool calls from model output.
//
// The format is XML-ish and models are inconsistent about whitespace: a call
// may be spread over several lines or emitted entirely on one line, and the
// closing </parameter>/</function>/</tool_call> tags may share a line with a
// parameter value. So this scans the raw text rather than going line by line —
// a line-oriented parser drops parameters whenever a value shares a line with
// a closing tag, and misses one-line calls entirely.
func parseQwenToolCalls(text string) (string, []api.ToolCall) {
	if !strings.Contains(text, "<tool_call>") && !strings.Contains(text, "<function=") {
		return text, nil
	}

	var calls []api.ToolCall
	var content strings.Builder
	rest := text

	for {
		start := strings.Index(rest, "<tool_call>")
		if start < 0 {
			break
		}
		content.WriteString(rest[:start])
		rest = rest[start+len("<tool_call>"):]

		body := rest
		if end := strings.Index(rest, "</tool_call>"); end >= 0 {
			body = rest[:end]
			rest = rest[end+len("</tool_call>"):]
		} else {
			rest = "" // unterminated: treat the remainder as the call body
		}
		if call, ok := parseToolCallBody(body, len(calls)); ok {
			calls = append(calls, call)
		}
	}

	// Some models emit <function=...> without the surrounding <tool_call>.
	if len(calls) == 0 && strings.Contains(rest, "<function=") {
		if call, ok := parseToolCallBody(rest, 0); ok {
			calls = append(calls, call)
			rest = ""
		}
	}
	content.WriteString(rest)

	return strings.TrimSpace(content.String()), calls
}

// parseToolCallBody parses a single "<function=name>...<parameter=k>v..." body.
func parseToolCallBody(body string, idx int) (api.ToolCall, bool) {
	fnStart := strings.Index(body, "<function=")
	if fnStart < 0 {
		return api.ToolCall{}, false
	}
	r := body[fnStart+len("<function="):]
	gt := strings.Index(r, ">")
	if gt < 0 {
		return api.ToolCall{}, false
	}
	name := strings.TrimSpace(r[:gt])
	if name == "" {
		return api.ToolCall{}, false
	}

	args := make(map[string]interface{})
	p := r[gt+1:]
	for {
		ps := strings.Index(p, "<parameter=")
		if ps < 0 {
			break
		}
		p = p[ps+len("<parameter="):]
		pe := strings.Index(p, ">")
		if pe < 0 {
			break
		}
		key := strings.TrimSpace(p[:pe])
		p = p[pe+1:]

		// The value runs to </parameter>, or to the next <parameter= when the
		// model forgets to close, or to </function>, or to the end.
		valEnd := len(p)
		for _, marker := range []string{"</parameter>", "<parameter=", "</function>"} {
			if i := strings.Index(p, marker); i >= 0 && i < valEnd {
				valEnd = i
			}
		}
		val := strings.TrimSpace(p[:valEnd])
		if key != "" {
			var parsed interface{}
			if json.Unmarshal([]byte(val), &parsed) == nil {
				args[key] = parsed
			} else {
				args[key] = val
			}
		}
		p = p[valEnd:]
	}

	// Gemma-family models frequently emit parameters as <key=value> instead of
	// the Qwen <parameter=key>value</parameter> form. Fall back to that shape
	// only when no standard parameters were found, so this can't misparse a
	// well-formed call.
	if len(args) == 0 {
		parseInlineParams(body[fnStart:], args)
	}

	argsJSON, _ := json.Marshal(args)
	return api.ToolCall{
		ID:   fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx),
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}, true
}

// parseInlineParams extracts <key=value> parameters, where the value ends at
// </key> when present and at the closing > otherwise. The leading
// <function=...> tag is skipped by the caller's slicing.
func parseInlineParams(body string, args map[string]interface{}) {
	p := body
	if i := strings.Index(p, ">"); i >= 0 {
		p = p[i+1:] // skip past <function=name>
	}
	for {
		lt := strings.Index(p, "<")
		if lt < 0 {
			return
		}
		p = p[lt+1:]
		eq := strings.Index(p, "=")
		gt := strings.Index(p, ">")
		if eq < 0 || (gt >= 0 && gt < eq) {
			continue // not a key=value tag
		}
		key := strings.TrimSpace(p[:eq])
		if key == "" || !isSimpleIdent(key) {
			continue
		}
		rest := p[eq+1:]

		end := len(rest)
		if close := strings.Index(rest, "</"+key+">"); close >= 0 {
			end = close
		} else if g := strings.Index(rest, ">"); g >= 0 {
			end = g
		}
		val := strings.TrimSpace(rest[:end])
		if _, seen := args[key]; !seen && val != "" {
			args[key] = val
		}
		p = rest[end:]
	}
}

// isSimpleIdent reports whether s looks like a parameter name rather than
// arbitrary markup, so stray angle brackets in prose aren't treated as params.
func isSimpleIdent(s string) bool {
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return len(s) > 0
}

// formatToolsPrompt builds the tool-calling system prompt for the given
// architecture. Returns empty string for architectures that handle tools
// via the chat template itself (e.g. LFM2 embeds tools in the system prompt).
func formatToolsPrompt(arch string, tools []api.Tool) string {
	switch arch {
	case "lfm2":
		// LFM2 tools are injected into the system prompt as JSON.
		// The chat template's {% if tools %} block handles formatting.
		// We prepend the tool list as part of the system message.
		var toolJSONs []string
		for _, tool := range tools {
			j, _ := json.Marshal(tool)
			toolJSONs = append(toolJSONs, string(j))
		}
		return "<|im_start|>system\nList of tools: [" + strings.Join(toolJSONs, ", ") + "]<|im_end|>\n"
	default:
		return formatQwenToolsPrompt(tools)
	}
}

func formatQwenToolsPrompt(tools []api.Tool) string {
	var sb strings.Builder
	sb.WriteString("<|im_start|>system\n# Tools\n\nYou have access to the following functions:\n\n<tools>")
	for _, tool := range tools {
		j, _ := json.Marshal(tool)
		sb.WriteString("\n")
		sb.Write(j)
	}
	sb.WriteString("\n</tools>")
	sb.WriteString("\n\nIf you choose to call a function ONLY reply in the following format with NO suffix:\n\n")
	sb.WriteString("<tool_call>\n<function=example_function_name>\n<parameter=example_parameter_1>\nvalue_1\n</parameter>\n<parameter=example_parameter_2>\nThis is the value for the second parameter\nthat can span\nmultiple lines\n</parameter>\n</function>\n</tool_call>\n\n")
	sb.WriteString("<IMPORTANT>\nReminder:\n- Function calls MUST follow the specified format: an inner <function=...></function> block must be nested within <tool_call></tool_call> XML tags\n- Required parameters MUST be specified\n- You may provide optional reasoning for your function call in natural language BEFORE the function call, but NOT after\n- If there is no function call available, answer the question like normal with your current knowledge and do not tell the user about function calls\n</IMPORTANT>")
	sb.WriteString("<|im_end|>\n")
	return sb.String()
}
