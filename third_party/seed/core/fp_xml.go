package core

import (
	"encoding/json"
	"strings"
)

// extractXMLBlocks extracts tool calls from XML <function=...> and <tool=...> blocks.
func (fp *FallbackParser) extractXMLBlocks(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for {
		openTag := -1
		for _, prefix := range []string{"<function=", "<tool="} {
			p := strings.Index(content[idx:], prefix)
			if p != -1 && openTag == -1 {
				openTag = p + idx
			}
		}
		if openTag == -1 {
			break
		}

		tagName := "function"
		if strings.HasPrefix(content[openTag:], "<tool=") {
			tagName = "tool"
		}
		prefixLen := len("<" + tagName + "=")
		afterPrefix := openTag + prefixLen
		closeAngle := strings.Index(content[afterPrefix:], ">")
		if closeAngle == -1 {
			break
		}
		name := strings.TrimSpace(content[afterPrefix : afterPrefix+closeAngle])
		if name == "" {
			idx = afterPrefix + closeAngle + 1
			continue
		}

		// Search for closing tag: </function=web_search> or </tool=web_search>.
		// Some Qwen-family checkpoints emit the bare form </function> or
		// </tool> without repeating the name — accept both spellings, taking
		// the earliest match. Matching "</tool" alone would false-positive on
		// </tool_call> wrappers, so both spellings are probed exactly.
		searchFrom := afterPrefix + closeAngle + 1
		bareClose := "</" + tagName + ">"
		namedClose := "</" + tagName + "="
		closeBare := strings.Index(content[searchFrom:], bareClose)
		closeNamed := strings.Index(content[searchFrom:], namedClose)
		closeTagIdx := -1
		namedCloser := false
		switch {
		case closeBare != -1 && (closeNamed == -1 || closeBare <= closeNamed):
			closeTagIdx = closeBare
		case closeNamed != -1:
			closeTagIdx = closeNamed
			namedCloser = true
		}
		var bodyStart, bodyEnd, blockEnd int
		if closeTagIdx != -1 {
			bodyStart = searchFrom
			bodyEnd = searchFrom + closeTagIdx // end of body content only
			// blockEnd includes the full closing tag so cleanContent removes it
			if namedCloser {
				// A named closer carries the tool name (</function=name>) —
				// skip past it to the terminating '>'.
				blockEnd = bodyEnd + len(namedClose)
				closer := strings.Index(content[blockEnd:], ">")
				if closer != -1 {
					blockEnd += closer + 1
				}
			} else {
				blockEnd = bodyEnd + len(bareClose)
			}
		} else {
			bodyStart = searchFrom
			bodyEnd = len(content)
			blockEnd = len(content)
		}
		if bodyEnd-bodyStart < 1 {
			idx = blockEnd
			continue
		}
		bodyRaw := strings.TrimSpace(content[bodyStart:bodyEnd])
		if bodyRaw == "" {
			idx = blockEnd
			continue
		}

		// Try XML <parameter> children first; fall back to raw JSON body.
		argsStr := fp.parseXMLParameters(bodyRaw)
		if argsStr == "" {
			argsStr = bodyRaw
		}
		if argsStr == "" {
			idx = blockEnd
			continue
		}
		tc := ToolCall{
			Type:     "function",
			Function: ToolCallFunction{Name: name, Arguments: argsStr},
		}
		// Qwen-family checkpoints wrap the block in <tool_call>…</tool_call>.
		// When the wrapper hugs the block (modulo whitespace), extend the
		// removed interval over it so cleanContent doesn't leave residue.
		blockStart := openTag
		if trimmedStart := strings.TrimRight(content[:openTag], " \t\r\n"); strings.HasSuffix(trimmedStart, "<tool_call>") {
			blockStart = len(trimmedStart) - len("<tool_call>")
		}
		blockFinal := blockEnd
		rest := strings.TrimLeft(content[blockEnd:], " \t\r\n")
		if ws := len(content[blockEnd:]) - len(rest); strings.HasPrefix(rest, "</tool_call>") {
			blockFinal = blockEnd + ws + len("</tool_call>")
		}
		blocks = append(blocks, rawBlock{
			start:  blockStart,
			end:    blockFinal,
			parsed: []ToolCall{tc},
		})
		idx = blockFinal
	}
	return blocks
}

// parseXMLParameters parses XML <parameter ...> children and returns a
// JSON-encoded object string. Returns empty string if no <parameter> elements
// are found. Two name spellings are accepted:
//
//	<parameter name="query">...  (quoted XML attribute)
//	<parameter=query>...         (name as tag suffix — Qwen-family checkpoints)
func (fp *FallbackParser) parseXMLParameters(body string) string {
	params := make(map[string]string)
	idx := 0
	for {
		// Find opening <parameter
		openTag := strings.Index(body[idx:], "<parameter")
		if openTag == -1 {
			break
		}
		openTag += idx

		// Ensure it is exactly "<parameter" (not "<parameterx" etc.)
		tagEnd := openTag + len("<parameter")
		if tagEnd < len(body) {
			ch := body[tagEnd]
			if ch == '<' || ch == '(' || ch == '/' ||
				(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				idx = tagEnd
				continue
			}
		}

		// Find closing >
		attrEnd := strings.Index(body[openTag:], ">")
		if attrEnd == -1 {
			break
		}
		attrEnd += openTag

		// Extract the name from the opening tag: either a quoted/xmlGetAttr
		// attribute or a tag-suffix value (<parameter=name>).
		attrs := body[openTag+10 : attrEnd] // skip "<parameter"
		name := fp.xmlGetAttr(attrs, "name")
		if name == "" {
			name = strings.Trim(strings.TrimSpace(attrs), "=")
		}
		if name == "" || strings.ContainsAny(name, " \t\n\r\"'<>=") {
			idx = attrEnd + 1
			continue
		}

		// Find matching closing tag
		closeTag := strings.Index(body[attrEnd:], "</"+"parameter>")
		var value string
		if closeTag != -1 {
			value = strings.TrimSpace(body[attrEnd+1 : attrEnd+closeTag])
		} else {
			value = strings.TrimSpace(body[attrEnd+1:])
		}
		params[name] = value
		if closeTag != -1 {
			idx = attrEnd + closeTag + len("</"+"parameter>")
		} else {
			break
		}
	}
	if len(params) == 0 {
		return ""
	}
	b, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	return string(b)
}

// xmlGetAttr extracts a named attribute value from an XML-like attribute string.
func (fp *FallbackParser) xmlGetAttr(attrs string, name string) string {
	idx := 0
	for idx < len(attrs) {
		// Skip whitespace
		for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t' || attrs[idx] == '\n' || attrs[idx] == '\r') {
			idx++
		}
		if idx >= len(attrs) {
			break
		}
		// Find attribute name
		start := idx
		for idx < len(attrs) && attrs[idx] != '=' && attrs[idx] != ' ' && attrs[idx] != '\t' {
			idx++
		}
		attrName := attrs[start:idx]
		if attrName != name {
			// Skip this attribute's value so the scan advances past it. If idx
			// already points at the delimiters ('=', or whitespace), the inner
			// loops below leave idx where it is and the outer loop would spin.
			idx = fp.skipAttrValue(attrs, idx)
			continue
		}
		// Expect '='
		if idx >= len(attrs) || attrs[idx] != '=' {
			break
		}
		idx++
		// Skip whitespace
		for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t') {
			idx++
		}
		if idx >= len(attrs) {
			return ""
		}
		// Get value delimiter
		var delim byte
		if attrs[idx] == '"' || attrs[idx] == '\'' {
			delim = attrs[idx]
			idx++
		} else {
			// No quotes, value goes to next space
			end := idx
			for end < len(attrs) && attrs[end] != ' ' && attrs[end] != '\t' && attrs[end] != '>' {
				end++
			}
			return attrs[idx:end]
		}
		// Find closing delimiter
		end := idx
		for end < len(attrs) && attrs[end] != delim {
			end++
		}
		return attrs[idx:end]
	}
	return ""
}

// skipAttrValue consumes an attribute's "=value" suffix starting at attrs[idx]
// and returns the index just past the value. idx must point at '=' or
// whitespace preceding it; both cases leave idx at or before the '='. Used to
// advance past a non-matching attribute so xmlGetAttr's scan cannot stall.
func (fp *FallbackParser) skipAttrValue(attrs string, idx int) int {
	for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t' || attrs[idx] == '\n' || attrs[idx] == '\r') {
		idx++
	}
	if idx >= len(attrs) || attrs[idx] != '=' {
		return idx
	}
	idx++
	for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t') {
		idx++
	}
	if idx >= len(attrs) {
		return idx
	}
	if attrs[idx] == '"' || attrs[idx] == '\'' {
		delim := attrs[idx]
		idx++
		for idx < len(attrs) && attrs[idx] != delim {
			idx++
		}
		// Land one past the closing delimiter (or len, if unterminated).
		return idx + 1
	}
	for idx < len(attrs) && attrs[idx] != ' ' && attrs[idx] != '\t' {
		idx++
	}
	return idx
}
