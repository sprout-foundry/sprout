package core

import (
	"encoding/json"
	"strings"
)

// extractNamedToolBlocks extracts tool calls from named block patterns like:
//
//	search {
//	  "query": "hello"
//	}
func (fp *FallbackParser) extractNamedToolBlocks(content string) []rawBlock {
	stripped := fp.stripCodeFences(content)
	// Precompute matched bracket pairs once (O(N)) instead of calling
	// matchBrace per identifier candidate, which was O(N^2) when many '{'
	// candidates were unmatched.
	matches := fp.computeBraceMatches(stripped)
	var blocks []rawBlock

	idx := 0
	for idx < len(stripped) {
		// Skip to a valid identifier start (letter or underscore)
		if !isIdentStart(stripped[idx]) {
			idx++
			continue
		}

		// Extract the full identifier
		identStart := idx
		for idx < len(stripped) && isIdentChar(stripped[idx]) {
			idx++
		}
		identEnd := idx
		name := stripped[identStart:identEnd]

		// Check if this is a known tool (if filter is set)
		if fp.knownToolNames != nil && !fp.knownToolNames(name) {
			continue
		}

		// After the identifier, skip whitespace
		for idx < len(stripped) && (stripped[idx] == ' ' || stripped[idx] == '\t' || stripped[idx] == '\n' || stripped[idx] == '\r') {
			idx++
		}

		// Expect '{' immediately after whitespace
		if idx >= len(stripped) || stripped[idx] != '{' {
			continue
		}

		braceStart := idx
		braceEnd, ok := matches[braceStart]
		if !ok {
			continue
		}

		// Extract the full JSON object including braces
		argsStr := strings.TrimSpace(stripped[braceStart : braceEnd+1])

		// Validate that the content is valid JSON, with repair for common
		// model output issues (trailing commas, unquoted keys).
		repaired := repairBlockJSON(argsStr)
		if !json.Valid([]byte(repaired)) {
			continue
		}
		argsStr = repaired

		// Create the tool call
		tc := ToolCall{
			Type:     "function",
			Function: ToolCallFunction{Name: name, Arguments: argsStr},
		}

		// The block spans from the identifier start to the end of the closing brace
		blockEnd := braceEnd + 1
		blocks = append(blocks, rawBlock{
			start:  identStart,
			end:    blockEnd,
			parsed: []ToolCall{tc},
		})

		// Continue scanning after this block
		idx = blockEnd
	}
	return blocks
}

// isIdentStart returns true if the byte can start a Go-like identifier
// (letter or underscore).
func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// isIdentChar returns true if the byte can appear in a Go-like identifier
// (letter, digit, underscore, or hyphen).
func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9') || b == '-'
}

// dedupeBlocks removes duplicate tool calls (same name+arguments) from blocks.
func dedupeBlocks(blocks []rawBlock) []rawBlock {
	seen := make(map[string]bool)
	added := make(map[int]bool)
	var result []rawBlock
	for bi, b := range blocks {
		if added[bi] {
			continue
		}
		blockHasNew := false
		for _, tc := range b.parsed {
			key := tc.Function.Name + "\x00" + tc.Function.Arguments
			if !seen[key] {
				seen[key] = true
				blockHasNew = true
			}
		}
		if blockHasNew {
			added[bi] = true
			result = append(result, b)
		}
	}
	return result
}

// repairBlockJSON attempts to repair common JSON issues in extracted blocks:
// 1. Removes trailing commas before } or ]
// 2. Quotes unquoted keys (e.g., {expression: "val"} → {"expression":"val"})
// Returns the original string if it's already valid JSON. Returns the original
// unchanged if repair fails (caller should check validity).
func repairBlockJSON(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}

	// Strategy 1: Remove trailing commas before } or ]
	fixed := strings.ReplaceAll(s, ",}", "}")
	fixed = strings.ReplaceAll(fixed, ",]", "]")
	if json.Valid([]byte(fixed)) {
		return fixed
	}

	// Strategy 2: Quote unquoted keys
	// Match patterns like `{key:` or `, key:` and quote the key.
	result := strings.Builder{}
	i := 0
	for i < len(fixed) {
		// Look for unquoted key patterns: after { or , optionally with whitespace,
		// followed by an identifier, then :
		ch := fixed[i]
		if ch == '{' || ch == ',' {
			result.WriteByte(ch)
			i++
			// Skip whitespace
			skip := i
			for skip < len(fixed) && (fixed[skip] == ' ' || fixed[skip] == '\t' || fixed[skip] == '\n' || fixed[skip] == '\r') {
				skip++
			}
			// Copy the whitespace
			for i < skip {
				result.WriteByte(fixed[i])
				i++
			}
			// Check if we have an unquoted key (identifier not starting with ")
			if i < len(fixed) && fixed[i] != '"' && fixed[i] != '}' && fixed[i] != ']' {
				// Collect identifier characters
				kStart := i
				for i < len(fixed) && isIdentChar(fixed[i]) {
					i++
				}
				// Skip whitespace after identifier
				skip2 := i
				for skip2 < len(fixed) && (fixed[skip2] == ' ' || fixed[skip2] == '\t') {
					skip2++
				}
				// Check if followed by ':'
				if skip2 < len(fixed) && fixed[skip2] == ':' {
					// Quote the key
					result.WriteByte('"')
					for kStart < i {
						result.WriteByte(fixed[kStart])
						kStart++
					}
					result.WriteByte('"')
					// Copy whitespace before ':'
					for i < skip2 {
						result.WriteByte(fixed[i])
						i++
					}
					// Copy ':'
					result.WriteByte(fixed[i])
					i++
					continue
				}
				// Not a key-value pair, output as-is
				for kStart < i {
					result.WriteByte(fixed[kStart])
					kStart++
				}
				continue
			}
			continue
		}
		result.WriteByte(ch)
		i++
	}

	if json.Valid([]byte(result.String())) {
		return result.String()
	}

	// Could not repair
	return s
}
