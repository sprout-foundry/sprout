package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// Precompiled regex patterns for performance
var (
	codeBlockRegex       = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\\s*(.*?)```")
	xmlFunctionRegex     = regexp.MustCompile(`(?s)<function=([^>]+)>(.*?)</function>`)
	xmlOpenWrapperRegex  = regexp.MustCompile(`(?s)<tool_call>\s*$`)
	xmlCloseWrapperRegex = regexp.MustCompile(`(?s)^\s*</tool_call>`)
	functionNameRegex    = regexp.MustCompile(`name:\s*(\w[\w\.-]*)`) // allow tool names with dots/dashes
)

// FallbackParser handles parsing tool calls from content when they should have been structured tool_calls
type FallbackParser struct {
	agent *Agent
}

// FallbackParseResult captures the tool calls that were parsed from content along with cleaned content
type FallbackParseResult struct {
	ToolCalls      []api.ToolCall
	CleanedContent string
}

type extractedBlock struct {
	calls []api.ToolCall
	start int
	end   int
}

type jsonSegment struct {
	start int
	end   int
	raw   string
}

// NewFallbackParser creates a new fallback parser
func NewFallbackParser(agent *Agent) *FallbackParser {
	return &FallbackParser{
		agent: agent,
	}
}

// Parse attempts to extract tool calls from content and returns both the calls and cleaned content
func (fp *FallbackParser) Parse(content string) *FallbackParseResult {
	if fp.agent.debug {
		fp.agent.debugLog("🔍 FallbackParser: Attempting to parse tool calls from content\n")
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	if !fp.containsToolCallPatterns(trimmed) {
		if fp.agent.debug {
			fp.agent.debugLog("🔍 FallbackParser: No tool call patterns detected\n")
		}
		return nil
	}

	blocks := fp.collectBlocks(trimmed)
	if len(blocks) == 0 {
		if fp.agent.debug {
			fp.agent.debugLog("🔍 FallbackParser: No valid tool calls found in content\n")
		}
		return nil
	}

	blocks = fp.mergeBlocks(blocks)

	var toolCalls []api.ToolCall
	for _, block := range blocks {
		for _, call := range block.calls {
			if call.Function.Name == "" {
				continue
			}
			toolCalls = append(toolCalls, fp.ensureToolCallDefaults(call))
		}
	}

	toolCalls = fp.dedupeToolCalls(toolCalls)
	if len(toolCalls) == 0 {
		if fp.agent.debug {
			fp.agent.debugLog("🔍 FallbackParser: No valid tool calls after normalization\n")
		}
		return nil
	}

	cleaned := fp.removeBlocksFromContent(trimmed, blocks)

	return &FallbackParseResult{
		ToolCalls:      toolCalls,
		CleanedContent: cleaned,
	}
}

// ShouldUseFallback checks if fallback parsing should be attempted
func (fp *FallbackParser) ShouldUseFallback(content string, hasStructuredToolCalls bool) bool {
	return !hasStructuredToolCalls && fp.containsToolCallPatterns(content)
}

func (fp *FallbackParser) collectBlocks(content string) []extractedBlock {
	var blocks []extractedBlock

	blocks = append(blocks, fp.parseJSONBlocks(content)...)
	blocks = append(blocks, fp.parseXMLBlocks(content)...)
	blocks = append(blocks, fp.parseFunctionBlocks(content)...)

	return blocks
}

func (fp *FallbackParser) parseJSONBlocks(content string) []extractedBlock {
	var blocks []extractedBlock

	codeBlockMatches := codeBlockRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range codeBlockMatches {
		if len(match) < 4 {
			continue
		}
		blockStart, blockEnd := match[0], match[1]
		innerStart, innerEnd := match[2], match[3]
		inner := content[innerStart:innerEnd]
		if !fp.containsToolCallPatterns(inner) {
			continue
		}
		calls := fp.parseToolCallsFromJSON(inner)
		if len(calls) == 0 {
			continue
		}
		blocks = append(blocks, extractedBlock{
			calls: calls,
			start: blockStart,
			end:   blockEnd,
		})
	}

	segments := fp.extractJSONSegments(content)
	for _, segment := range segments {
		if !fp.containsToolCallPatterns(segment.raw) {
			continue
		}
		if fp.segmentCovered(segment.start, segment.end, blocks) {
			continue
		}
		calls := fp.parseToolCallsFromJSON(segment.raw)
		if len(calls) == 0 {
			continue
		}
		blocks = append(blocks, extractedBlock{
			calls: calls,
			start: segment.start,
			end:   segment.end,
		})
	}

	return blocks
}

func (fp *FallbackParser) parseXMLBlocks(content string) []extractedBlock {
	var blocks []extractedBlock

	matches := xmlFunctionRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		blockStart, blockEnd := match[0], match[1]
		nameStart, nameEnd := match[2], match[3]
		innerStart, innerEnd := match[4], match[5]

		if loc := xmlOpenWrapperRegex.FindStringIndex(content[:blockStart]); loc != nil {
			blockStart = loc[0]
		}

		if loc := xmlCloseWrapperRegex.FindStringIndex(content[blockEnd:]); loc != nil {
			blockEnd += loc[1]
		}

		name := strings.TrimSpace(content[nameStart:nameEnd])
		inner := content[innerStart:innerEnd]
		args := fp.parseXMLParameters(inner)
		call := fp.createToolCallFromArgs(name, args)
		if call.Function.Name == "" {
			continue
		}
		blocks = append(blocks, extractedBlock{
			calls: []api.ToolCall{call},
			start: blockStart,
			end:   blockEnd,
		})
	}

	return blocks
}

func (fp *FallbackParser) parseFunctionBlocks(content string) []extractedBlock {
	var blocks []extractedBlock

	matches := functionNameRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		nameStart, nameEnd := match[2], match[3]
		name := strings.TrimSpace(content[nameStart:nameEnd])
		jsonSegment, ok := fp.findJSONSegment(content, match[1])
		if !ok {
			continue
		}
		args := fp.parseJSONArguments(jsonSegment.raw)
		call := fp.createToolCallFromArgs(name, args)
		if call.Function.Name == "" {
			continue
		}
		blocks = append(blocks, extractedBlock{
			calls: []api.ToolCall{call},
			start: match[0],
			end:   jsonSegment.end,
		})
	}

	return blocks
}

func (fp *FallbackParser) parseToolCallsFromJSON(raw string) []api.ToolCall {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	type toolCallWrapper struct {
		ToolCalls []json.RawMessage `json:"tool_calls"`
		Message   struct {
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	}

	var wrapper toolCallWrapper
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil {
		var calls []api.ToolCall
		for _, item := range wrapper.ToolCalls {
			if call, ok := fp.convertRawToolCall(item); ok {
				calls = append(calls, call)
			}
		}
		for _, item := range wrapper.Message.ToolCalls {
			if call, ok := fp.convertRawToolCall(item); ok {
				calls = append(calls, call)
			}
		}
		if len(calls) > 0 {
			return calls
		}
	} else if fp.agent != nil && fp.agent.debug {
		fp.agent.debugLog("FallbackParser: JSON wrapper parse error: %v (snippet: %s)\n", err, truncateForLog(raw))
	}

	var array []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &array); err == nil {
		var calls []api.ToolCall
		for _, item := range array {
			if call, ok := fp.convertRawToolCall(item); ok {
				calls = append(calls, call)
			}
		}
		if len(calls) > 0 {
			return calls
		}
	} else if fp.agent != nil && fp.agent.debug {
		fp.agent.debugLog("FallbackParser: JSON array parse error: %v (snippet: %s)\n", err, truncateForLog(raw))
	}

	if call, ok := fp.convertRawToolCall(json.RawMessage([]byte(raw))); ok {
		return []api.ToolCall{call}
	}

	return nil
}

func (fp *FallbackParser) convertRawToolCall(raw json.RawMessage) (api.ToolCall, bool) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		if fp.agent != nil && fp.agent.debug {
			fp.agent.debugLog("FallbackParser: tool call map parse error: %v (snippet: %s)\n", err, truncateForLog(string(raw)))
		}
		return api.ToolCall{}, false
	}

	name := fp.extractToolName(data)
	if name == "" {
		return api.ToolCall{}, false
	}

	argsRaw := fp.extractArguments(data)
	arguments := fp.normalizeArguments(argsRaw)

	call := api.ToolCall{}
	if idRaw, ok := data["id"]; ok {
		var id string
		if err := json.Unmarshal(idRaw, &id); err == nil && id != "" {
			call.ID = id
		}
	}
	if typeRaw, ok := data["type"]; ok {
		var typ string
		if err := json.Unmarshal(typeRaw, &typ); err == nil && typ != "" {
			call.Type = typ
		}
	}

	call.Function.Name = name
	call.Function.Arguments = arguments

	return call, true
}

func (fp *FallbackParser) extractToolName(data map[string]json.RawMessage) string {
	if fnRaw, ok := data["function"]; ok {
		var fn struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(fnRaw, &fn); err == nil && fn.Name != "" {
			return fn.Name
		} else if err != nil && fp.agent != nil && fp.agent.debug {
			fp.agent.debugLog("FallbackParser: function.name parse error: %v (snippet: %s)\n", err, truncateForLog(string(fnRaw)))
		}
	}

	if fcRaw, ok := data["function_call"]; ok {
		var fc struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(fcRaw, &fc); err == nil && fc.Name != "" {
			return fc.Name
		} else if err != nil && fp.agent != nil && fp.agent.debug {
			fp.agent.debugLog("FallbackParser: function_call.name parse error: %v (snippet: %s)\n", err, truncateForLog(string(fcRaw)))
		}
	}

	for _, key := range []string{"name", "tool", "function"} {
		if raw, ok := data[key]; ok {
			var name string
			if err := json.Unmarshal(raw, &name); err == nil && name != "" {
				return name
			}
		}
	}

	return ""
}

func (fp *FallbackParser) extractArguments(data map[string]json.RawMessage) json.RawMessage {
	if fnRaw, ok := data["function"]; ok {
		var fn struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(fnRaw, &fn); err == nil && len(fn.Arguments) > 0 {
			return fn.Arguments
		} else if err != nil && fp.agent != nil && fp.agent.debug {
			fp.agent.debugLog("FallbackParser: function.arguments parse error: %v (snippet: %s)\n", err, truncateForLog(string(fnRaw)))
		}
	}

	if fcRaw, ok := data["function_call"]; ok {
		var fc struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(fcRaw, &fc); err == nil && len(fc.Arguments) > 0 {
			return fc.Arguments
		} else if err != nil && fp.agent != nil && fp.agent.debug {
			fp.agent.debugLog("FallbackParser: function_call.arguments parse error: %v (snippet: %s)\n", err, truncateForLog(string(fcRaw)))
		}
	}

	for _, key := range []string{"arguments", "args", "input"} {
		if raw, ok := data[key]; ok {
			return raw
		}
	}

	return json.RawMessage("{}")
}

func (fp *FallbackParser) normalizeArguments(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "{}"
	}

	if trimmed[0] == '"' {
		var asString string
		if err := json.Unmarshal(trimmed, &asString); err == nil {
			inner := strings.TrimSpace(asString)
			if inner == "" {
				return "{}"
			}
			if json.Valid([]byte(inner)) {
				return inner
			}
			encoded, err := json.Marshal(inner)
			if err == nil {
				return string(encoded)
			}
		}
	}

	if json.Valid(trimmed) {
		return string(trimmed)
	}

	var asInterface interface{}
	if err := json.Unmarshal(trimmed, &asInterface); err == nil {
		normalized, err := json.Marshal(asInterface)
		if err == nil {
			return string(normalized)
		}
	}

	encoded, err := json.Marshal(string(trimmed))
	if err == nil {
		return string(encoded)
	}

	return "{}"
}

func (fp *FallbackParser) parseXMLParameters(content string) map[string]interface{} {
	args := make(map[string]interface{})
	paramRegex := regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)
	matches := paramRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		value := strings.TrimSpace(match[2])
		if name == "" {
			continue
		}

		var jsonValue interface{}
		if err := json.Unmarshal([]byte(value), &jsonValue); err == nil {
			args[name] = jsonValue
		} else {
			args[name] = value
		}
	}

	if len(args) == 0 {
		var jsonArgs map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &jsonArgs); err == nil {
			return jsonArgs
		}
	}

	return args
}

func (fp *FallbackParser) createToolCallFromArgs(name string, args map[string]interface{}) api.ToolCall {
	if strings.TrimSpace(name) == "" {
		return api.ToolCall{}
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		argsJSON = []byte("{}")
	}

	call := api.ToolCall{}
	call.Function.Name = strings.TrimSpace(name)
	call.Function.Arguments = string(argsJSON)
	return call
}

func (fp *FallbackParser) ensureToolCallDefaults(call api.ToolCall) api.ToolCall {
	if strings.TrimSpace(call.Function.Name) == "" {
		return call
	}

	if call.ID == "" {
		call.ID = fp.generateToolCallID(call.Function.Name)
	}
	if call.Type == "" {
		call.Type = "function"
	}
	if strings.TrimSpace(call.Function.Arguments) == "" {
		call.Function.Arguments = "{}"
	}

	return call
}

func (fp *FallbackParser) generateToolCallID(name string) string {
	sanitized := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(strings.ToLower(name), "_")
	sanitized = strings.Trim(sanitized, "_")
	if sanitized == "" {
		sanitized = "tool"
	}
	return fmt.Sprintf("fallback_%s_%d", sanitized, time.Now().UnixNano())
}

func (fp *FallbackParser) dedupeToolCalls(calls []api.ToolCall) []api.ToolCall {
	if len(calls) <= 1 {
		return calls
	}
	seen := make(map[string]struct{})
	result := make([]api.ToolCall, 0, len(calls))
	for _, call := range calls {
		key := call.Function.Name + "::" + call.Function.Arguments
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, call)
	}
	return result
}

func (fp *FallbackParser) removeBlocksFromContent(content string, blocks []extractedBlock) string {
	if len(blocks) == 0 {
		return strings.TrimSpace(content)
	}

	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].start == blocks[j].start {
			return blocks[i].end < blocks[j].end
		}
		return blocks[i].start < blocks[j].start
	})

	var builder strings.Builder
	prev := 0
	for _, block := range blocks {
		if block.start < prev {
			block.start = prev
		}
		if block.start > len(content) {
			break
		}
		if block.end > len(content) {
			block.end = len(content)
		}
		builder.WriteString(content[prev:block.start])
		prev = block.end
	}

	if prev < len(content) {
		builder.WriteString(content[prev:])
	}

	cleaned := strings.TrimSpace(builder.String())
	lines := strings.Split(cleaned, "\n")
	compact := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmedLine := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmedLine) == "" {
			continue
		}
		compact = append(compact, trimmedLine)
	}

	cleaned = strings.TrimSpace(strings.Join(compact, "\n"))

	return cleaned
}

func (fp *FallbackParser) containsToolCallPatterns(content string) bool {
	patterns := []string{
		`"tool_calls"`,
		`"function"`,
		`"function_call"`,
		`"arguments"`,
		`"name"`,
		`name:`,
		`arguments:`,
		`<function=`,
		`<tool_call`,
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}

func (fp *FallbackParser) mergeBlocks(blocks []extractedBlock) []extractedBlock {
	if len(blocks) <= 1 {
		return blocks
	}

	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].start == blocks[j].start {
			return blocks[i].end < blocks[j].end
		}
		return blocks[i].start < blocks[j].start
	})

	merged := []extractedBlock{blocks[0]}
	for _, block := range blocks[1:] {
		last := &merged[len(merged)-1]
		if block.start <= last.end {
			if block.end > last.end {
				last.end = block.end
			}
			last.calls = append(last.calls, block.calls...)
			continue
		}
		merged = append(merged, block)
	}

	return merged
}

func (fp *FallbackParser) segmentCovered(start, end int, blocks []extractedBlock) bool {
	for _, block := range blocks {
		if start >= block.start && end <= block.end {
			return true
		}
	}
	return false
}

func (fp *FallbackParser) extractJSONSegments(content string) []jsonSegment {
	var segments []jsonSegment
	inString := false
	escape := false
	depth := 0
	segmentStart := -1

	for i := 0; i < len(content); i++ {
		ch := content[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if ch == '{' || ch == '[' {
			if depth == 0 {
				segmentStart = i
			}
			depth++
		} else if ch == '}' || ch == ']' {
			depth--
			if depth == 0 && segmentStart != -1 {
				segmentEnd := i + 1
				raw := content[segmentStart:segmentEnd]
				segments = append(segments, jsonSegment{
					start: segmentStart,
					end:   segmentEnd,
					raw:   raw,
				})
				segmentStart = -1
			}
		}
	}

	return segments
}

func (fp *FallbackParser) findJSONSegment(content string, start int) (jsonSegment, bool) {
	for i := start; i < len(content); i++ {
		ch := content[i]
		if ch == '{' || ch == '[' {
			segment, ok := fp.readBalancedJSON(content, i)
			if ok {
				return segment, true
			}
		}
	}
	return jsonSegment{}, false
}

func (fp *FallbackParser) readBalancedJSON(content string, start int) (jsonSegment, bool) {
	inString := false
	escape := false
	depth := 0

	for i := start; i < len(content); i++ {
		ch := content[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if ch == '{' || ch == '[' {
			depth++
		} else if ch == '}' || ch == ']' {
			depth--
			if depth == 0 {
				segmentEnd := i + 1
				raw := content[start:segmentEnd]
				if json.Valid([]byte(raw)) {
					return jsonSegment{start: start, end: segmentEnd, raw: raw}, true
				}
				return jsonSegment{}, false
			}
		}
	}

	return jsonSegment{}, false
}

func (fp *FallbackParser) parseJSONArguments(raw string) map[string]interface{} {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err == nil {
		return args
	} else if fp.agent != nil && fp.agent.debug {
		fp.agent.debugLog("FallbackParser: arguments parse error: %v (snippet: %s)\n", err, truncateForLog(raw))
	}
	return map[string]interface{}{}
}

// truncateForLog returns a safe, max-length snippet for debug logging
func truncateForLog(s string) string {
	const max = 120
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
