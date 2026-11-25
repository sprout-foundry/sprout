package core

import (
	"encoding/json"
	"strings"
)

// extractAntmlBlocks extracts tool calls from Anthropic-namespaced markup.
//
// Some checkpoints (observed: qwen3.8-27b via NInfer) narrate a tool call as
// text using "antml:"-prefixed tags, with the tool name carried in a
// <parameter name="TOOL"> attribute rather than a <function=/<tool= opener:
//
//	<antml:parameter name="edit_file">
//	<antml:parameter name="new_str" string="true">…</antml:parameter>
//	<antml:parameter name="path" string="true">a.js</antml:parameter>
//	</antml:parameter>
//
// The block is often wrapped in <antml:function_calls>…</antml:function_calls>
// and each call in <antml:invoke name="TOOL">…</antml:invoke>. Both the bare
// parameter-block form and the invoke form are handled here.
func (fp *FallbackParser) extractAntmlBlocks(content string) []rawBlock {
	var blocks []rawBlock
	blocks = append(blocks, fp.extractAntmlInvokeBlocks(content)...)
	blocks = append(blocks, fp.extractAntmlParameterBlocks(content)...)
	return blocks
}

// antmlOpenTag is the opening tag prefix for a namespaced parameter element.
const antmlOpenTag = "<antml:parameter"

// antmlCloseTag is the closing tag for a namespaced parameter element.
const antmlCloseTag = "</antml:parameter>"

// extractAntmlInvokeBlocks handles the <antml:invoke name="TOOL">…</antml:invoke>
// form, whose children are <antml:parameter name="ARG">…</antml:parameter>.
func (fp *FallbackParser) extractAntmlInvokeBlocks(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for {
		openTag := strings.Index(content[idx:], "<antml:invoke")
		if openTag == -1 {
			break
		}
		openTag += idx

		// The name attribute lives on the invoke tag itself.
		attrEnd := strings.Index(content[openTag:], ">")
		if attrEnd == -1 {
			break
		}
		attrEnd += openTag
		name := fp.xmlGetAttr(content[openTag+len("<antml:invoke"):attrEnd], "name")
		if name == "" {
			idx = attrEnd + 1
			continue
		}

		bodyStart := attrEnd + 1
		closeIdx := strings.Index(content[bodyStart:], "</antml:invoke>")
		var bodyEnd, blockEnd int
		if closeIdx != -1 {
			bodyEnd = bodyStart + closeIdx
			blockEnd = bodyEnd + len("</antml:invoke>")
		} else {
			bodyEnd = len(content)
			blockEnd = len(content)
		}

		body := content[bodyStart:bodyEnd]
		argsStr := fp.parseAntmlParameters(body)
		if argsStr == "" {
			idx = blockEnd
			continue
		}
		tc := ToolCall{
			Type:     "function",
			Function: ToolCallFunction{Name: name, Arguments: argsStr},
		}
		blocks = append(blocks, rawBlock{
			start:  openTag,
			end:    blockEnd,
			parsed: []ToolCall{tc},
		})
		idx = blockEnd
	}
	return blocks
}

// extractAntmlParameterBlocks handles the bare form where the outermost
// <antml:parameter name="TOOL"> carries the tool name and its children are the
// arguments. Nested parameter elements are consumed as a unit so an inner
// <antml:parameter name="ARG"> is never mistaken for a top-level tool call.
func (fp *FallbackParser) extractAntmlParameterBlocks(content string) []rawBlock {
	var blocks []rawBlock
	idx := 0
	for {
		openTag := strings.Index(content[idx:], antmlOpenTag)
		if openTag == -1 {
			break
		}
		openTag += idx

		// Reject a longer sibling tag (e.g. "<antml:parameters") — the byte
		// after the tag name must not continue the identifier.
		tagEnd := openTag + len(antmlOpenTag)
		if tagEnd < len(content) && isIdentChar(content[tagEnd]) {
			idx = tagEnd
			continue
		}

		attrEnd := strings.Index(content[openTag:], ">")
		if attrEnd == -1 {
			break
		}
		attrEnd += openTag
		name := fp.xmlGetAttr(content[openTag+len(antmlOpenTag):attrEnd], "name")
		if name == "" {
			idx = attrEnd + 1
			continue
		}

		// The element body ends at the LAST close tag before the next
		// top-level element open (or EOF). The outermost element is always the
		// last one in a run of closes, so a value that itself contains a
		// tag-shaped literal cannot truncate the element.
		bodyStart := attrEnd + 1
		blockEnd := fp.antmlElementEnd(content, bodyStart)
		if blockEnd == -1 {
			// Unterminated: take the rest of the content as the body.
			blockEnd = len(content)
		}
		bodyEnd := blockEnd
		if strings.HasSuffix(content[:blockEnd], antmlCloseTag) {
			bodyEnd = blockEnd - len(antmlCloseTag)
		}

		body := content[bodyStart:bodyEnd]
		argsStr := fp.parseAntmlParameters(body)
		if argsStr == "" {
			// No nested parameters: treat the element's text as a raw JSON body
			// (some checkpoints put a JSON object directly inside).
			trimmed := strings.TrimSpace(body)
			if json.Valid([]byte(trimmed)) {
				argsStr = trimmed
			}
		}
		if argsStr == "" {
			idx = blockEnd
			continue
		}
		tc := ToolCall{
			Type:     "function",
			Function: ToolCallFunction{Name: name, Arguments: argsStr},
		}
		blocks = append(blocks, rawBlock{
			start:  openTag,
			end:    blockEnd,
			parsed: []ToolCall{tc},
		})
		idx = blockEnd
	}
	return blocks
}

// antmlElementEnd returns the index just past the closing </antml:parameter>
// that matches an element whose body starts at bodyStart, honoring nesting.
// Returns -1 when no matching close exists.
//
// Nesting is counted so an inner <antml:parameter name="ARG">…</antml:parameter>
// child does not terminate its parent. A value that itself contains a balanced
// tag-shaped literal pair (a code payload quoting this exact markup) will
// over-count and end the element early — a pathological input the real
// checkpoint output does not produce, and one that still recovers a call.
func (fp *FallbackParser) antmlElementEnd(content string, bodyStart int) int {
	depth := 1
	pos := bodyStart
	for pos < len(content) {
		nextOpen := strings.Index(content[pos:], antmlOpenTag)
		nextClose := strings.Index(content[pos:], antmlCloseTag)
		if nextClose == -1 {
			return -1
		}
		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			pos += nextOpen + len(antmlOpenTag)
			continue
		}
		depth--
		pos += nextClose
		if depth == 0 {
			return pos + len(antmlCloseTag)
		}
		pos += len(antmlCloseTag)
	}
	return -1
}

// parseAntmlParameters parses nested <antml:parameter name="ARG">…</antml:parameter>
// children and returns a JSON-encoded object string, or "" when none are found.
// Values are taken verbatim (not XML-unescaped) so code payloads survive intact.
func (fp *FallbackParser) parseAntmlParameters(body string) string {
	params := make(map[string]string)
	idx := 0
	for {
		openTag := strings.Index(body[idx:], antmlOpenTag)
		if openTag == -1 {
			break
		}
		openTag += idx

		tagEnd := openTag + len(antmlOpenTag)
		if tagEnd < len(body) && isIdentChar(body[tagEnd]) {
			idx = tagEnd
			continue
		}

		attrEnd := strings.Index(body[openTag:], ">")
		if attrEnd == -1 {
			break
		}
		attrEnd += openTag
		name := fp.xmlGetAttr(body[openTag+len(antmlOpenTag):attrEnd], "name")
		if name == "" {
			idx = attrEnd + 1
			continue
		}

		valueStart := attrEnd + 1
		closeIdx := strings.Index(body[valueStart:], antmlCloseTag)
		var value string
		if closeIdx != -1 {
			value = body[valueStart : valueStart+closeIdx]
			idx = valueStart + closeIdx + len(antmlCloseTag)
		} else {
			value = body[valueStart:]
			idx = len(body)
		}
		params[name] = value
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
