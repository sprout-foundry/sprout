package core

import (
	"encoding/json"
	"strings"
)

// extractToolBlocks extracts tool calls from <tool>...</tool> blocks.
func (fp *FallbackParser) extractToolBlocks(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for {
		openTag := strings.Index(content[idx:], "<tool>")
		if openTag == -1 {
			break
		}
		openTag += idx
		afterPrefix := openTag + 6
		closeTagIdx := strings.Index(content[afterPrefix:], "</tool>")
		var bodyStart, bodyEnd int
		if closeTagIdx != -1 {
			bodyStart = afterPrefix
			bodyEnd = bodyStart + closeTagIdx
		} else {
			bodyStart = afterPrefix
			bodyEnd = len(content)
		}
		body := strings.TrimSpace(content[bodyStart:bodyEnd])
		toolCalls := fp.parseToolCallsJSON(body)
		if len(toolCalls) > 0 {
			blocks = append(blocks, rawBlock{
				start:  openTag,
				end:    bodyEnd + 7, // include </tool> closing tag (7 chars)
				parsed: toolCalls,
			})
		}
		if closeTagIdx != -1 {
			idx = bodyStart + closeTagIdx + 7
		} else {
			break
		}
	}
	return blocks
}

// extractToolUseBlocks extracts tool calls from <tool_use>...</tool_use> blocks.
func (fp *FallbackParser) extractToolUseBlocks(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for {
		openTag := strings.Index(content[idx:], "<tool_use>")
		if openTag == -1 {
			break
		}
		openTag += idx
		afterPrefix := openTag + 10
		closeTagIdx := strings.Index(content[afterPrefix:], "</tool_use>")
		var bodyStart, bodyEnd int
		if closeTagIdx != -1 {
			bodyStart = afterPrefix
			bodyEnd = bodyStart + closeTagIdx
		} else {
			bodyStart = afterPrefix
			bodyEnd = len(content)
		}
		body := strings.TrimSpace(content[bodyStart:bodyEnd])
		toolCalls := fp.parseToolCallsJSON(body)
		if len(toolCalls) > 0 {
			blocks = append(blocks, rawBlock{
				start:  openTag,
				end:    bodyEnd + 11, // include </tool_use> closing tag (11 chars)
				parsed: toolCalls,
			})
		}
		if closeTagIdx != -1 {
			idx = bodyStart + closeTagIdx + 11
		} else {
			break
		}
	}
	return blocks
}

// extractFunctionNamePatterns detects `name: tool_name` followed by balanced
// JSON arguments. This handles patterns like:
//
//	name: search
//	arguments: {"query": "hello"}
//
// or variations with different key names (e.g., "function:", "tool:", "name:").
func (fp *FallbackParser) extractFunctionNamePatterns(content string) []rawBlock {
	var blocks []rawBlock

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		name, ok := fp.extractNameValue(line)
		if !ok {
			continue
		}

		if fp.knownToolNames != nil && !fp.knownToolNames(name) {
			continue
		}

		argsJSON, argsEndLine := fp.findArgumentsInLines(lines, i+1)
		if argsJSON == "" {
			continue
		}

		blockStart := fp.lineToOffset(content, i)
		if argsEndLine > len(lines) {
			argsEndLine = len(lines)
		}
		blockEnd := fp.lineToOffset(content, argsEndLine)
		if blockEnd > len(content) {
			blockEnd = len(content)
		}

		tc := ToolCall{
			Type:     "function",
			Function: ToolCallFunction{Name: name, Arguments: argsJSON},
		}
		blocks = append(blocks, rawBlock{
			start:  blockStart,
			end:    blockEnd,
			parsed: []ToolCall{tc},
		})

		i = argsEndLine - 1
	}

	return blocks
}

// extractNameValue checks if a line matches "name: value" pattern and returns
// the value. It handles variations like "name:", "Name:", "NAME:", etc.
func (fp *FallbackParser) extractNameValue(line string) (string, bool) {
	keyPatterns := []string{"name:", "function:", "tool:", "function_name:", "tool_name:"}
	lowerLine := strings.ToLower(line)
	for _, pattern := range keyPatterns {
		idx := strings.Index(lowerLine, pattern)
		if idx == -1 {
			continue
		}

		if idx > 0 {
			prev := line[idx-1]
			if prev != ' ' && prev != '\t' && prev != '\r' {
				continue
			}
		}

		valueStart := idx + len(pattern)
		if valueStart >= len(line) {
			continue
		}

		for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
			valueStart++
		}

		if valueStart >= len(line) {
			continue
		}

		value := strings.TrimSpace(line[valueStart:])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}

		if value != "" {
			return value, true
		}
	}

	return "", false
}

// findArgumentsInLines searches for JSON arguments in lines starting from
// startIndex. It looks for patterns like "arguments: {...}" or just "{...}".
// Returns the JSON string and the line number AFTER the last consumed line.
func (fp *FallbackParser) findArgumentsInLines(lines []string, startIndex int) (string, int) {
	if startIndex >= len(lines) {
		return "", 0
	}

	maxLines := startIndex + 10
	if maxLines > len(lines) {
		maxLines = len(lines)
	}

	for i := startIndex; i < maxLines; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		argsKeyPatterns := []string{"arguments:", "args:", "input:", "parameters:", "params:"}
		lowerLine := strings.ToLower(line)
		origLine := lines[i]
		leadingSpace := len(origLine) - len(line)
		for _, pattern := range argsKeyPatterns {
			idx := strings.Index(lowerLine, pattern)
			if idx == -1 {
				continue
			}

			if idx > 0 {
				prev := lowerLine[idx-1]
				if prev != ' ' && prev != '\t' && prev != '\r' {
					continue
				}
			}

			valueStart := idx + len(pattern)
			if valueStart >= len(line) {
				continue
			}

			for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
				valueStart++
			}

			if valueStart >= len(line) {
				continue
			}

			value := line[valueStart:]

			if len(value) > 0 && (value[0] == '{' || value[0] == '[') {
				if json.Valid([]byte(value)) {
					return value, i + 1
				}

				if len(value) > 1 && value[0] == '{' {
					unescaped := unescapeJSONString(value)
					if unescaped != "" && json.Valid([]byte(unescaped)) {
						return unescaped, i + 1
					}
				}

				jsonStr, endLine := fp.collectJSONLines(lines, i, leadingSpace+valueStart)
				if jsonStr != "" {
					return jsonStr, endLine
				}
				return "", 0
			}

			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
				unquoted := value[1 : len(value)-1]
				if json.Valid([]byte(unquoted)) {
					return unquoted, i + 1
				}
				unescaped := strings.ReplaceAll(unquoted, `\"`, `"`)
				if json.Valid([]byte(unescaped)) {
					return unescaped, i + 1
				}
			}
		}

		if len(line) > 0 && (line[0] == '{' || line[0] == '[') {
			if json.Valid([]byte(line)) {
				return line, i + 1
			}
			jsonStr, endLine := fp.collectJSONLines(lines, i, leadingSpace)
			if jsonStr != "" {
				return jsonStr, endLine
			}
			return "", 0
		}
	}

	return "", 0
}
