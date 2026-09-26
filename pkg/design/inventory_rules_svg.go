package design

// inventory_rules_svg.go — the SVG attribute-parsing helpers used by the
// token-usage validator: svgTokenUsageAttrs and the SVG value-offset /
// tag-span helpers (elementSpan, findCloseTagOffset, indexByteFrom,
// findSVGAttrValueOffset, isSVGNameByte, isSVGSpace). Split out of
// inventory_rules.go.

import (
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strings"
)

// svgTokenUsageAttrs walks an SVG document and collects the literal
// fill/stroke/font-family attributes it carries, resolving each one's byte
// offset and the byte span of the element that owns it so the finding can carry
// a line and the backing check can scope a comment to the element. A malformed
// document yields no attributes: its well-formedness failure is the validator's
// business (ruleSVGWellformed), not this advisory pack's.
func svgTokenUsageAttrs(content []byte) []tokenUsageAttr {
	dec := xml.NewDecoder(bytes.NewReader(content))
	dec.Strict = true

	var out []tokenUsageAttr
	searchFrom := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		beg, end := elementSpan(content, se, searchFrom)
		if beg >= 0 {
			searchFrom = beg + 1
		}
		for _, a := range se.Attr {
			attr := strings.ToLower(a.Name.Local)
			var literal bool
			switch attr {
			case "fill", "stroke":
				literal = isColorLiteral(a.Value)
			case "font-family":
				literal = fontFamilyIsLiteral(a.Value)
			default:
				continue
			}
			if !literal {
				continue
			}
			off := findSVGAttrValueOffset(content, a.Name.Local, a.Value, 0)
			out = append(out, tokenUsageAttr{
				attr:    attr,
				value:   a.Value,
				offset:  off,
				elemBeg: beg,
				elemEnd: end,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].offset < out[j].offset })
	return out
}

// elementSpan returns the [start, end) byte span of the markup of the element
// se: from its opening `<name` through the matching close tag (or the end of
// its self-closing tag, or the end of the document for an unclosed element).
// The span lets the token-usage check scope a comment to the element it sits
// in. Opening tags are located at or after searchFrom so a repeated tag name
// resolves to the next occurrence, which keeps the walk aligned with the
// decoder's document order. A failed match returns (-1, -1) and the caller
// falls back to line-only association.
func elementSpan(content []byte, se xml.StartElement, searchFrom int) (int, int) {
	open := findTagOffset(content, se.Name.Local, searchFrom)
	if open < 0 {
		// Retry from the top: a tag nested earlier in the document may still
		// be the decoder's next start element when the walk has advanced past
		// it (for example after a parent's close tag was consumed first).
		open = findTagOffset(content, se.Name.Local, 0)
	}
	if open < 0 {
		return -1, -1
	}
	tagEnd := indexByteFrom(content, open, '>')
	if tagEnd < 0 {
		return open, len(content)
	}
	// Self-closing element: the span is just the tag.
	if tagEnd > open && content[tagEnd-1] == '/' {
		return open, tagEnd + 1
	}
	close := findCloseTagOffset(content, se.Name.Local, tagEnd)
	if close < 0 {
		return open, tagEnd + 1
	}
	return open, close
}

// findCloseTagOffset locates the matching `</name>` at or after fromOff and
// returns the offset just past its `>`; -1 when absent.
func findCloseTagOffset(content []byte, name string, fromOff int) int {
	needle := []byte("</" + name + ">")
	if idx := bytes.Index(content[fromOff:], needle); idx >= 0 {
		return fromOff + idx + len(needle)
	}
	return -1
}

// indexByteFrom returns the offset of the first b at or after fromOff, or -1.
func indexByteFrom(content []byte, fromOff int, b byte) int {
	if fromOff < 0 {
		fromOff = 0
	}
	if idx := bytes.IndexByte(content[fromOff:], b); idx >= 0 {
		return fromOff + idx
	}
	return -1
}

// findSVGAttrValueOffset locates the byte offset of `attr="value"` (or the
// single-quoted form, with optional whitespace around `=`) at or after fromOff,
// returning the offset of the value's first byte, or -1 when the pair is absent.
// It is the token-usage pack's own locator rather than the shared
// findAttrValueOffset helper: the pack needs best-effort line support on
// ordinary styling attributes, and isolating the search here keeps the
// established SVG validator untouched.
func findSVGAttrValueOffset(content []byte, attr, value string, fromOff int) int {
	if fromOff < 0 || fromOff > len(content) {
		fromOff = 0
	}
	attrBytes := []byte(attr)
	for base := fromOff; base < len(content); {
		idx := bytes.Index(content[base:], attrBytes)
		if idx < 0 {
			return -1
		}
		start := base + idx
		j := start + len(attrBytes)
		// The attribute name must end at a word boundary (`fill` is not part
		// of `fill-opacity` or `xfill`).
		if j < len(content) && isSVGNameByte(content[j]) {
			base = start + 1
			continue
		}
		// Optional whitespace, `=`, optional whitespace, then the opening quote.
		for j < len(content) && isSVGSpace(content[j]) {
			j++
		}
		if j >= len(content) || content[j] != '=' {
			base = start + 1
			continue
		}
		j++
		for j < len(content) && isSVGSpace(content[j]) {
			j++
		}
		if j >= len(content) || (content[j] != '"' && content[j] != '\'') {
			base = start + 1
			continue
		}
		quote := content[j]
		valueStart := j + 1
		valueEnd := valueStart + len(value)
		if valueEnd <= len(content) && string(content[valueStart:valueEnd]) == value {
			next := valueEnd
			if next < len(content) && content[next] == quote {
				return valueStart
			}
		}
		base = start + 1
	}
	return -1
}

// isSVGNameByte reports whether b may appear in an XML attribute name.
func isSVGNameByte(b byte) bool {
	return b == '-' || b == '_' || b == ':' || b == '.' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isSVGSpace reports whether b is XML whitespace.
func isSVGSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// ---------------------------------------------------------------------------
// Screen inventory — orphan wireframes
// ---------------------------------------------------------------------------
