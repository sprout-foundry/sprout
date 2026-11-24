package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// FallbackParserOptions configures the parser.
type FallbackParserOptions struct {
	// KnownToolNames returns true if the given name is a registered tool.
	// When nil, all extracted tool names are accepted.
	KnownToolNames func(string) bool
	// Debug enables verbose logging (printed to stderr).
	Debug bool
}

// FallbackParser extracts tool calls from malformed LLM response content.
type FallbackParser struct {
	debug          bool
	knownToolNames func(string) bool
}

// FallbackParseResult contains extracted tool calls and cleaned content.
type FallbackParseResult struct {
	ToolCalls      []ToolCall
	CleanedContent string
	// UnknownToolNames are the names of calls the content held that are not
	// registered tools (KnownToolNames refused them), in the order found.
	UnknownToolNames []string
}

// NewFallbackParser creates a new FallbackParser with the given options.
func NewFallbackParser(opts FallbackParserOptions) *FallbackParser {
	return &FallbackParser{
		debug:          opts.Debug,
		knownToolNames: opts.KnownToolNames,
	}
}

// ShouldUseFallback returns true when structured tool_calls are missing
// and the content contains patterns suggestive of tool calls.
//
// It uses a three-tier confidence model:
//
//   - Tier 1: Strong patterns (code fences, XML tags, quoted JSON keys)
//     trigger immediately with a single match.
//   - Tier 2a: Weak patterns (bare keywords like "name:" or "arguments:")
//     require at least two independent matches to avoid false positives on
//     normal conversational text.
//   - Tier 2b: One weak pattern plus a JSON structure marker ({" or [")
//     triggers a single weak match, catching patterns like:
//     "name: search\n{\"query\": \"hello\"}"
func (fp *FallbackParser) ShouldUseFallback(content string, hasStructuredToolCalls bool) bool {
	if hasStructuredToolCalls {
		return false
	}
	return fp.containsToolCallPatterns(content)
}

// Parse extracts tool calls from malformed LLM response content.
func (fp *FallbackParser) Parse(content string) *FallbackParseResult {
	if !fp.containsToolCallPatterns(content) {
		return &FallbackParseResult{
			ToolCalls:      nil,
			CleanedContent: content,
		}
	}
	blocks := fp.extractAll(content)
	if len(blocks) == 0 {
		return &FallbackParseResult{
			ToolCalls:      nil,
			CleanedContent: content,
		}
	}
	merged := fp.mergeAndDedupe(blocks)
	toolCalls, unknown := fp.normalizeAll(merged)
	cleaned := fp.cleanContent(content, merged)
	return &FallbackParseResult{
		ToolCalls:        toolCalls,
		CleanedContent:   cleaned,
		UnknownToolNames: unknown,
	}
}

// ---------------------------------------------------------------------------
// ShouldUseFallback helpers
// ---------------------------------------------------------------------------

// strongPatternMarkers are high-confidence indicators of tool-call content.
// A single match is sufficient to trigger fallback parsing.
var strongPatternMarkers = []string{
	// Code fences
	"```json",
	"```",
	// XML tags
	"<function=",
	"<tool=",
	"<tool>",
	"<tool_use>",
	// Anthropic-namespaced tags. Some checkpoints (observed: qwen3.8-27b via
	// NInfer) emit tool calls as "<antml:parameter name=\"tool\">…" with the
	// "antml:" prefix on every tag, and no <function=/<tool= opener. These are
	// unambiguous tool-call markup, so a single match triggers Tier 1.
	"<antml:parameter",
	"<antml:function_calls",
	"<antml:invoke",
	// call: prefix (e.g., "call:calculator{...}")
	"call:",
	// JSON keys — quoted so they only match actual JSON key occurrences
	// (e.g., {"tool_calls": ...}), not bare English prose.
	// JSON keys are high-confidence because normal prose rarely has these
	// exact quoted-key patterns.
	`"tool_calls"`,
	`"function_call"`,
	`"tool_use"`,
	`"arguments"`,
	`"function":`,
}

// Note: "arguments" (unquoted) is here as a weak marker — it catches bare
// occurrences in prose like "the arguments for this position". The quoted
// version is in strongPatternMarkers and triggers Tier 1 for actual JSON keys.
var weakPatternMarkers = []string{
	"name:",
	"function:",
	"tool:",
	"function_name:",
	"tool_name:",
	"arguments",
	"args:",
	"input:",
	"parameters:",
	"params:",
}

// jsonStructureMarkers indicate the presence of JSON data in the content.
// Alone they are not enough to trigger fallback, but combined with a weak
// keyword they suggest tool-call-like content.
var jsonStructureMarkers = []string{
	`{"`,
	`["`,
}

// weakPatternThreshold is the minimum number of distinct weak markers that
// must match before we consider the content tool-call-like.
const weakPatternThreshold = 2

func (fp *FallbackParser) containsToolCallPatterns(content string) bool {
	if len(strings.TrimSpace(content)) == 0 {
		return false
	}
	lowerContent := strings.ToLower(content)

	// Tier 1: strong patterns — single match is enough
	for _, m := range strongPatternMarkers {
		if strings.Contains(lowerContent, m) {
			return true
		}
	}

	// Count weak marker matches and JSON structure matches.
	weakMatches := 0
	hasJSONStructure := false
	for _, m := range weakPatternMarkers {
		if strings.Contains(lowerContent, m) {
			weakMatches++
		}
	}
	for _, m := range jsonStructureMarkers {
		if strings.Contains(lowerContent, m) {
			hasJSONStructure = true
			break
		}
	}

	// Tier 2a: multiple weak markers (e.g., "name:" + "arguments")
	if weakMatches >= weakPatternThreshold {
		return true
	}

	// Tier 2b: one weak marker + JSON structure (e.g., "name:" + '{"query":...}')
	if weakMatches >= 1 && hasJSONStructure {
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// rawBlock: a region of content potentially containing tool calls.
// ---------------------------------------------------------------------------

type rawBlock struct {
	start  int
	end    int
	parsed []ToolCall
}

// ---------------------------------------------------------------------------
// Extraction strategies
// ---------------------------------------------------------------------------

func (fp *FallbackParser) extractAll(content string) []rawBlock {
	var blocks []rawBlock
	blocks = append(blocks, fp.extractJSONFences(content)...)
	blocks = append(blocks, fp.extractXMLBlocks(content)...)
	blocks = append(blocks, fp.extractAntmlBlocks(content)...)
	blocks = append(blocks, fp.extractToolBlocks(content)...)
	blocks = append(blocks, fp.extractToolUseBlocks(content)...)
	blocks = append(blocks, fp.extractFunctionNamePatterns(content)...)
	blocks = append(blocks, fp.extractBareJSON(content)...)
	blocks = append(blocks, fp.extractNamedToolBlocks(content)...)
	return blocks
}

// stripCodeFences returns content with fenced blocks replaced by spaces.
func (fp *FallbackParser) stripCodeFences(content string) string {
	buf := make([]byte, len(content))
	copy(buf, content)
	inFence := false
	for i := 0; i < len(content); i++ {
		if !inFence && i+2 < len(content) &&
			content[i] == '`' && content[i+1] == '`' && content[i+2] == '`' {
			inFence = true
			for j := i; j < i+3 && j < len(buf); j++ {
				buf[j] = ' '
			}
			i += 2
			continue
		}
		if inFence && i+2 < len(content) &&
			content[i] == '`' && content[i+1] == '`' && content[i+2] == '`' {
			inFence = false
			for j := i; j < i+3 && j < len(buf); j++ {
				buf[j] = ' '
			}
			i += 2
			continue
		}
		if inFence {
			buf[i] = ' '
		}
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// mergeAndDedupe: orchestration — sort, merge overlapping, dedupe
// ---------------------------------------------------------------------------

func (fp *FallbackParser) mergeAndDedupe(blocks []rawBlock) []rawBlock {
	if len(blocks) <= 1 {
		return blocks
	}
	// Sort by start position.
	sorted := make([]rawBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].start < sorted[j].start
	})
	// Merge overlapping blocks.
	var merged []rawBlock
	current := 0
	for current < len(sorted) {
		end := sorted[current].end
		parsed := make([]ToolCall, 0, len(sorted[current].parsed))
		parsed = append(parsed, sorted[current].parsed...)
		next := current + 1
		for next < len(sorted) && sorted[next].start < end {
			if sorted[next].end > end {
				end = sorted[next].end
			}
			parsed = append(parsed, sorted[next].parsed...)
			next++
		}
		merged = append(merged, rawBlock{
			start:  sorted[current].start,
			end:    end,
			parsed: parsed,
		})
		current = next
	}
	// Deduplicate by name+args.
	return dedupeBlocks(merged)
}

// ---------------------------------------------------------------------------
// normalize: validate, canonicalize, assign synthetic IDs
// ---------------------------------------------------------------------------

func (fp *FallbackParser) normalize(blocks []rawBlock) []ToolCall {
	toolCalls, _ := fp.normalizeAll(blocks)
	return toolCalls
}

// normalizeAll is normalize, also returning the names of calls it refused
// because they are not registered tools.
func (fp *FallbackParser) normalizeAll(blocks []rawBlock) ([]ToolCall, []string) {
	var toolCalls []ToolCall
	var unknown []string
	seen := make(map[string]bool)
	seenUnknown := make(map[string]bool)
	for _, b := range blocks {
		for _, tc := range b.parsed {
			if tc.Function.Name == "" {
				continue
			}
			if fp.knownToolNames != nil && !fp.knownToolNames(tc.Function.Name) {
				if !seenUnknown[tc.Function.Name] {
					seenUnknown[tc.Function.Name] = true
					unknown = append(unknown, tc.Function.Name)
				}
				continue
			}
			trimmed := strings.TrimSpace(tc.Function.Arguments)
			trimmed = tryRepairJSON(trimmed)
			if trimmed == "" || !isValidJSON(trimmed) {
				continue
			}
			// Canonicalize arguments to compact JSON so that semantically
			// identical arguments with different whitespace are treated as
			// the same for deduplication and downstream consumption.
			canonicalArgs := canonicalizeJSON(trimmed)
			key := tc.Function.Name + "\x00" + canonicalArgs
			if seen[key] {
				continue
			}
			seen[key] = true
			if tc.ID == "" {
				tc.ID = syntheticID(tc.Function.Name)
			}
			// Always normalize Type to "function" — even if the model
			// returned a different type (e.g., "tool", "code"), we force
			// the canonical value so downstream consumers don't have to
			// handle multiple type strings.
			tc.Type = "function"
			tc.Function.Arguments = canonicalArgs
			toolCalls = append(toolCalls, tc)
		}
	}
	return toolCalls, unknown
}

func syntheticID(name string) string {
	return fmt.Sprintf("fallback_%s_%d", name, time.Now().UnixNano())
}

func isValidJSON(s string) bool {
	return json.Valid([]byte(strings.TrimSpace(s)))
}

// tryRepairJSON attempts to repair common JSON issues in argument strings:
//  1. If already valid JSON, returns as-is.
//  2. Removes trailing commas before } or ] (e.g., {"location": "NYC",} → {"location":"NYC"}).
//  3. If repair succeeds, returns the fixed string. Otherwise returns the original
//     unchanged (caller's isValidJSON check will then reject it).
func tryRepairJSON(s string) string {
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

// canonicalizeJSON re-marshals valid JSON into compact canonical form.
// This normalizes whitespace, key ordering is preserved by Go's map
// iteration (which is fine for deduplication as long as the input is
// already a string). Returns the original string if re-marshaling fails.
//
// Uses json.Decoder.UseNumber() to preserve numeric precision for large
// integers that would otherwise be truncated to float64.
func canonicalizeJSON(s string) string {
	var parsed any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return s
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return s
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// cleanContent: remove extracted blocks from original content
// ---------------------------------------------------------------------------

func (fp *FallbackParser) cleanContent(content string, blocks []rawBlock) string {
	if len(blocks) == 0 {
		return content
	}
	// Sort ascending by start position for interval merging.
	sorted := make([]rawBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].start < sorted[j].start
	})
	// Merge overlapping intervals (interval stabbing problem).
	var intervals []struct{ start, end int }
	for _, b := range sorted {
		if len(intervals) > 0 && b.start < intervals[len(intervals)-1].end {
			// Merge with previous interval
			if b.end > intervals[len(intervals)-1].end {
				intervals[len(intervals)-1].end = b.end
			}
		} else {
			intervals = append(intervals, struct{ start, end int }{b.start, b.end})
		}
	}
	// Build result by copying only the segments between removed intervals.
	var result strings.Builder
	pos := 0
	for _, iv := range intervals {
		if iv.start > pos {
			result.WriteString(content[pos:iv.start])
		}
		pos = iv.end
	}
	if pos < len(content) {
		result.WriteString(content[pos:])
	}
	// Normalize whitespace: collapse into single spaces.
	cleaned := strings.Join(strings.Fields(result.String()), " ")
	return strings.TrimSpace(cleaned)
}

// ---------------------------------------------------------------------------
// Helpers used by extraction strategies
// ---------------------------------------------------------------------------

// unescapeJSONString handles the case where a JSON string value is wrapped
// in outer quotes with escaped inner quotes, like:
//
//	{"key": "val"}  (from the raw text: "{\"key\": \"val\"}")
//
// It strips the outer quotes and unescapes \" to ".
func unescapeJSONString(s string) string {
	if len(s) < 2 {
		return ""
	}

	// Check if the string looks like a quoted JSON: starts with { or [
	// and ends with } or ] (possibly preceded by ")
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 {
		return ""
	}

	// Case 1: trimmed starts and ends with the same quote
	if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') ||
		(trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
		inner := trimmed[1 : len(trimmed)-1]
		// Unescape \" to "
		unescaped := strings.ReplaceAll(inner, `\"`, `"`)
		return unescaped
	}

	// Case 2: starts with { or [ but contains \" patterns suggesting
	// it's a quoted JSON string that lost its outer quotes
	if (trimmed[0] == '{' || trimmed[0] == '[') && strings.Contains(trimmed, `\"`) {
		// Try to find the matching closing quote at the end
		// The pattern is: {\"key\": \"val\"}
		// We need to unescape \" to " and check if valid JSON
		unescaped := strings.ReplaceAll(trimmed, `\"`, `"`)
		if json.Valid([]byte(unescaped)) {
			return unescaped
		}
	}

	return ""
}
