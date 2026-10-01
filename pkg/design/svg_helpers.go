package design

// svg_helpers.go — the low-level SVG parsing helpers: the svgWalk /
// refLoc / dataNavLoc / symbolLoc / dataURI / xmlMap types and the
// walkSVG / parseViewBox / frameMatches / offset / data-URI location
// functions, split out of svg.go.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
)

// svgWalk holds the structural facts the wireframe checks extract from one
// SVG document via a single encoding/xml pass.
type svgWalk struct {
	error          error
	errorLine      int
	sawRoot        bool
	rootName       string
	rootIsSVG      bool
	viewBoxRaw     string
	viewBoxW       int
	viewBoxH       int
	viewBoxMissing bool
	viewBoxAllInt  bool
	hasText        bool
	scripts        []refLoc
	resourceRefs   []refLoc
	dataNavs       []dataNavLoc
	symbols        []symbolLoc
} // refLoc is a <script> tag or a resource reference (href/src) found during

// the walk.
type refLoc struct {
	tag   string
	attr  string
	value string
}

// dataNavLoc is an element carrying a data-nav attribute during the walk.
type dataNavLoc struct {
	value string
	id    string
}

// symbolLoc is a <symbol> entry and its id during the walk (used for the
// icon sprite.svg check, SP-140-1 §1f).
type symbolLoc struct {
	id string
}

// walkSVG decodes one SVG document and collects the structural facts the
// wireframe checks need: well-formedness (and the first syntax error's line),
// the root element and its viewBox, <script> tags, resource references,
// data-nav elements, and <text> presence.
func walkSVG(content []byte) svgWalk {
	var w svgWalk
	dec := xml.NewDecoder(bytes.NewReader(content))
	dec.Strict = true

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			var synErr *xml.SyntaxError
			if errors.As(err, &synErr) {
				w.error = err
				w.errorLine = synErr.Line
			} else {
				w.error = err
			}
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		tagName := se.Name.Local

		if !w.sawRoot {
			w.sawRoot = true
			w.rootName = tagName
			if strings.EqualFold(tagName, "svg") {
				w.rootIsSVG = true
			}
			for _, a := range se.Attr {
				if a.Name.Local == "viewBox" {
					w.viewBoxRaw = a.Value
					var ok bool
					_, _, w.viewBoxW, w.viewBoxH, ok = parseViewBox(a.Value)
					w.viewBoxAllInt = ok
				}
			}
			if w.viewBoxRaw == "" {
				w.viewBoxMissing = true
			}
		}

		switch {
		case strings.EqualFold(tagName, "script"):
			w.scripts = append(w.scripts, refLoc{tag: tagName})
		case strings.EqualFold(tagName, "text"):
			w.hasText = true
		case strings.EqualFold(tagName, "symbol"):
			symID := ""
			for _, a := range se.Attr {
				if a.Name.Local == "id" {
					symID = a.Value
				}
			}
			w.symbols = append(w.symbols, symbolLoc{id: symID})
		}

		for _, a := range se.Attr {
			local := a.Name.Local
			switch {
			case isResourceAttr(local):
				w.resourceRefs = append(w.resourceRefs, refLoc{tag: tagName, attr: local, value: a.Value})
			case local == "data-nav":
				id := ""
				for _, a2 := range se.Attr {
					if a2.Name.Local == "id" {
						id = a2.Value
					}
				}
				w.dataNavs = append(w.dataNavs, dataNavLoc{value: a.Value, id: id})
			}
		}
	}
	return w
}

// parseViewBox parses a viewBox value into its four components. ok is false
// when the value does not hold exactly four integers.
func parseViewBox(raw string) (minX, minY, w, h int, ok bool) {
	parts := strings.Fields(raw)
	if len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	vals := make([]int, 4)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], vals[3], true
}

// frameMatches reports whether (w,h) equals any declared device frame.
func frameMatches(w, h int, frames []Frame) bool {
	for _, f := range frames {
		if f.Width == w && f.Height == h {
			return true
		}
	}
	return false
}

// finalizeWireframeFindings normalizes a wireframe finding slice: never nil
// and sorted deterministically (file, line, rule, message). File and
// Severity are stamped at creation, so this only guards and sorts.
func finalizeWireframeFindings(findings []Finding) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}

// findTagOffset locates an opening <tag (not followed by a word character)
// at or after fromOff, returning the byte offset or -1 when absent.
func findTagOffset(content []byte, tag string, fromOff int) int {
	needle := []byte("<" + tag)
	base := fromOff
	for {
		idx := bytes.Index(content[base:], needle)
		if idx < 0 {
			return -1
		}
		abs := base + idx
		j := abs + len(needle)
		if j < len(content) {
			c := content[j]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				base = abs + 1
				continue
			}
		}
		return abs
	}
}

// findAttrValueOffset locates `attr="<value>"` (double- or single-quoted)
// at or after fromOff, returning the byte offset or -1 when absent. Best-
// effort line support for findings.
func findAttrValueOffset(content []byte, attr, value string, fromOff int) int {
	for _, q := range []string{"\"", "'"} {
		needle := []byte(attr + q + value + q)
		if idx := bytes.Index(content[fromOff:], needle); idx >= 0 {
			return fromOff + idx
		}
	}
	return -1
}

// dataURI is one data: URI occurrence under the shared design root.
type dataURI struct {
	line int
	size int
}

// findDataURIsIn scans content for embedded data: URIs (SP-140-1 §1h). It
// first tries the XML walk so a URI inside an attribute value is measured as
// the attribute value, then sweeps the raw text for anything the walk missed
// (for example a URI inside a <style> block, which is not an XML attribute).
// Overlapping candidates are deduplicated so one URI yields one result.
func findDataURIsIn(content []byte) []dataURI {
	var out []dataURI
	seen := map[int]bool{}
	add := func(off int) {
		if off < 0 || seen[off] {
			return
		}
		seen[off] = true
		end := dataURITokenEnd(content, off)
		out = append(out, dataURI{
			line: lineOfOffset(content, off),
			size: end - off,
		})
	}

	walk := walkSVG(content)
	var m xmlMap
	if walk.error == nil {
		m = xmlOffsets(content)
	}
	for _, ref := range walk.resourceRefs {
		if !strings.HasPrefix(strings.TrimSpace(ref.value), "data:") {
			continue
		}
		off := findResourceValueOffset(content, ref, m)
		if off < 0 {
			off = findDataURIValueOffset(content, ref.value, 0)
		}
		add(off)
	}

	for from := 0; ; {
		idx := bytes.Index(content[from:], []byte("data:"))
		if idx < 0 {
			break
		}
		off := from + idx
		from = off + len("data:")
		add(off)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].size < out[j].size
	})
	return out
}

// dataURITokenEnd returns the exclusive end offset of the data: URI starting
// at off. The scan stops at an unescaped whitespace character or at a
// terminator that would close the surrounding attribute, XML text, or CSS
// url() context: the marked quotes, and the angle brackets, backslash, and
// parentheses that delimit markup and CSS. A baseline-64 payload contains
// none of them, so the byte count is the encoded payload size — the value a
// reviewer would have to read in a diff. A malformed payload carrying one of
// those bytes stops the measurement early, which can only under-report the
// size; the same document fails the SVG well-formedness check anyway.
func dataURITokenEnd(content []byte, off int) int {
	for i := off; i < len(content); i++ {
		switch c := content[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			return i
		case c == '"' || c == '\'' || c == '<' || c == '>' || c == '\\' || c == '(' || c == ')':
			return i
		}
	}
	return len(content)
}

// xmlMap maps quoted attribute values to their byte offsets in a document.
type xmlMap map[string][]int

// xmlOffsets locates every "value" and 'value' occurrence in content, so the
// walk's decoded attribute values can be mapped back to a byte offset (and
// hence a line) even when the decoder normalized the value.
func xmlOffsets(content []byte) xmlMap {
	m := xmlMap{}
	for _, q := range []byte{'"', '\''} {
		for i := 0; i < len(content); i++ {
			if content[i] != q {
				continue
			}
			j := i + 1
			for j < len(content) && content[j] != q {
				j++
			}
			if j >= len(content) {
				break
			}
			value := string(content[i+1 : j])
			m[value] = append(m[value], i+1)
			i = j
		}
	}
	return m
}

// findResourceValueOffset resolves a walk resource reference to its literal
// byte offset: the recorded attribute-value opening quote, then the attribute
// name, then the first quoted occurrence of the decoded value.
func findResourceValueOffset(content []byte, ref refLoc, m xmlMap) int {
	attr := []byte(ref.attr)
	for q := 0; q < len(content); q++ {
		if content[q] != '"' && content[q] != '\'' {
			continue
		}
		if q+len(attr) >= len(content) || string(content[q+1:q+1+len(attr)]) != ref.attr {
			continue
		}
		j := q + 1 + len(attr)
		for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n' || content[j] == '\r') {
			j++
		}
		if j < len(content) && content[j] == '=' {
			j++
		}
		for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n' || content[j] == '\r') {
			j++
		}
		if j < len(content) && (content[j] == '"' || content[j] == '\'') {
			if m != nil {
				for _, off := range m[string(content[j+1:min(j+1+len(ref.value), len(content))])] {
					if off == j+1 {
						return off
					}
				}
			}
			return j + 1
		}
		if j < len(content) {
			return j
		}
	}
	return -1
}

// findDataURIValueOffset locates "value" or 'value' at or after fromOff,
// returning the offset of the value's first byte, or -1.
func findDataURIValueOffset(content []byte, value string, fromOff int) int {
	for _, q := range []string{"\"", "'"} {
		needle := []byte(q + value + q)
		if idx := bytes.Index(content[fromOff:], needle); idx >= 0 {
			return fromOff + idx + 1
		}
	}
	return -1
}
