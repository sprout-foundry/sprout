package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractJSONFences extracts tool calls from ```json code fences in content.
func (fp *FallbackParser) extractJSONFences(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for idx < len(content) {
		fenceStart := strings.Index(content[idx:], "```")
		if fenceStart == -1 {
			break
		}
		fenceStart += idx
		afterFence := fenceStart + 3
		rest := content[afterFence:]

		// Determine where actual content starts (skip language tag + newline)
		contentOffset := 0
		if nl := strings.IndexAny(rest, "\n\r"); nl != -1 {
			lang := strings.TrimSpace(rest[:nl])
			if lang != "" && lang != "json" {
				// Skip non-json fences
				ci := strings.Index(rest[nl+1:], "```")
				if ci == -1 {
					break
				}
				idx = afterFence + nl + 1 + ci
				continue
			}
			contentOffset = nl + 1
		}

		// Find the closing ```
		closeIdxInRest := strings.Index(rest, "```")
		if closeIdxInRest == -1 {
			closeIdxInRest = len(rest)
		}

		// blockContent excludes the language tag
		blockContent := rest[contentOffset:closeIdxInRest]

		toolCalls := fp.parseToolCallsJSON(blockContent)
		if len(toolCalls) > 0 {
			// Block spans from opening fence to end of closing fence (or end of content).
			// closeIdxInRest is relative to rest (content[afterFence:]).
			blockEnd := afterFence + closeIdxInRest
			if closeIdxInRest < len(rest) {
				blockEnd += 3 // include closing fence markers
			}
			blocks = append(blocks, rawBlock{
				start:  fenceStart,
				end:    blockEnd,
				parsed: toolCalls,
			})
		}

		// Continue scanning after the closing fence.
		idx = afterFence + closeIdxInRest + 3
		if idx > len(content) {
			break
		}
	}
	return blocks
}

// parseToolCallsJSON parses JSON content into tool calls.
func (fp *FallbackParser) parseToolCallsJSON(content string) []ToolCall {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return nil
	}

	// Attempt to repair common model output issues (trailing commas) so
	// that all subsequent unmarshal attempts have a chance of succeeding.
	content = repairJSONForParsing(content)

	// Direct array of tool calls
	var tcs []ToolCall
	if err := json.Unmarshal([]byte(content), &tcs); err == nil && len(tcs) > 0 {
		return tcs
	}

	// {tool_calls: [...]}
	var w struct {
		ToolCalls []ToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(content), &w); err == nil && len(w.ToolCalls) > 0 {
		return w.ToolCalls
	}

	// {function_call: {...}}
	var sf struct {
		FunctionCall struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function_call"`
	}
	if err := json.Unmarshal([]byte(content), &sf); err == nil {
		if sf.FunctionCall.Name != "" && sf.FunctionCall.Arguments != "" {
			return []ToolCall{{
				Type:     "function",
				Function: ToolCallFunction{Name: sf.FunctionCall.Name, Arguments: sf.FunctionCall.Arguments},
			}}
		}
	}

	// {tool_use: {...}}
	var tu struct {
		ToolUse struct {
			Name      string `json:"name"`
			Input     any    `json:"input"`
			InputJSON string `json:"input_json"`
		} `json:"tool_use"`
	}
	if err := json.Unmarshal([]byte(content), &tu); err == nil {
		if tu.ToolUse.Name != "" {
			args := fp.anyToString(tu.ToolUse.Input)
			return []ToolCall{{
				Type:     "function",
				Function: ToolCallFunction{Name: tu.ToolUse.Name, Arguments: args},
			}}
		}
	}

	// {input: {...}} with name field
	var ni struct {
		Name  string `json:"name"`
		Input any    `json:"input"`
	}
	if err := json.Unmarshal([]byte(content), &ni); err == nil {
		if ni.Name != "" && ni.Input != nil {
			args := fp.anyToString(ni.Input)
			return []ToolCall{{
				Type:     "function",
				Function: ToolCallFunction{Name: ni.Name, Arguments: args},
			}}
		}
	}

	// {name: "...", arguments: {...}} — top-level name/arguments format
	// commonly emitted by models that don't use a wrapper object.
	var na struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(content), &na); err == nil {
		if na.Name != "" {
			args := fp.anyToString(na.Arguments)
			return []ToolCall{{
				Type:     "function",
				Function: ToolCallFunction{Name: na.Name, Arguments: args},
			}}
		}
	}

	// Single ToolCall object (e.g., {"id": "...", "type": "function", "function": {...}})
	var single ToolCall
	if err := json.Unmarshal([]byte(content), &single); err == nil {
		if single.Function.Name != "" {
			return []ToolCall{single}
		}
	}

	return nil
}

// repairJSONForParsing attempts basic repair of JSON content so that
// json.Unmarshal can succeed on malformed but repairable input.
// Returns the repaired string if repair succeeded, or the original
// unchanged if repair was unnecessary or failed.
func repairJSONForParsing(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	fixed := strings.ReplaceAll(s, ",}", "}")
	fixed = strings.ReplaceAll(fixed, ",]", "]")
	if json.Valid([]byte(fixed)) {
		return fixed
	}
	return s
}

// anyToString converts a value to a JSON argument string.
func (fp *FallbackParser) anyToString(v any) string {
	if v == nil {
		return "{}"
	}
	switch s := v.(type) {
	case string:
		if s == "" {
			return "{}"
		}
		return s
	case map[string]any:
		b, _ := json.Marshal(s)
		return string(b)
	case []any:
		b, _ := json.Marshal(s)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}
