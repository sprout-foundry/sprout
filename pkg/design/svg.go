package design

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Rule ids for the SVG wireframe validator, SP-140-1 §1b/§1g. Constants
// rather than literals so findings and future tooling share one spelling.
const (
	// ruleSVGWellformed fires when the document is not well-formed XML.
	ruleSVGWellformed = "svg_wellformed"

	// ruleSVGViewBox fires when the root is not <svg> or its viewBox is
	// missing or holds non-integer values.
	ruleSVGViewBox = "svg_viewbox"

	// ruleSVGSelfContainment fires on <script> elements and on external or
	// local resource references (href/src).
	ruleSVGSelfContainment = "svg_self_containment"

	// ruleSVGSlugName fires when the wireframe file stem breaks the slug rule.
	ruleSVGSlugName = "svg_slug_name"

	// ruleSVGDataNavDangling fires when a data-nav target is not a wireframe stem.
	ruleSVGDataNavDangling = "svg_data_nav_dangling"

	// ruleSVGFrameMatch is advisory: the root viewBox does not match any
	// declared device frame.
	ruleSVGFrameMatch = "svg_frame_match"

	// ruleSVGTextUsage is advisory: the wireframe has no <text> elements.
	ruleSVGTextUsage = "svg_text_usage"

	// ruleSVGStableIDs is advisory: an interactive element (data-nav) lacks
	// a stable id.
	ruleSVGStableIDs = "svg_stable_ids"

	// ruleSVGDataURISize is advisory (warn): an embedded data: URI exceeds
	// the size threshold, so the SVG diff is no longer reviewable and the
	// raster should move to brand/ (SP-140-1 §1h).
	ruleSVGDataURISize = DataURISizeRule
)

// resourceAttrs are the SVG attribute local names that may reference a
// (potentially external) resource. xlink:href decodes to Local "href".
var resourceAttrs = map[string]struct{}{
	"href": {},
	"src":  {},
}

// isResourceAttr reports whether an XML attribute local name is one that may
// reference a resource.
func isResourceAttr(local string) bool {
	_, ok := resourceAttrs[local]
	return ok
}

// classifyResourceRef classifies an href/src value for the self-containment
// check (SP-140-1 §1b). "" means the reference is allowed (empty, an
// in-document fragment, or a data: URI); "external" is a remote scheme;
// "local" is a bare file path that would break self-containment.
func classifyResourceRef(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return ""
	case strings.HasPrefix(v, "#"):
		return ""
	case strings.HasPrefix(v, "data:"):
		return ""
	case strings.Contains(v, "://"), strings.HasPrefix(v, "//"):
		return "external"
	default:
		return "local"
	}
}

// ValidateWireframe validates one SVG wireframe per SP-140-1 §1b. relPath is
// the design-relative path (e.g. "design/wireframes/login.svg"); knownStems
// is the set of all wireframe file stems (without .svg) used to resolve
// data-nav targets; frames (optional) enables the advisory frame-match check
// and is nil for runs without declared device frames.
//
// Hard checks (SeverityError): XML well-formedness, a root <svg> with an
// integer viewBox, self-containment (no <script>, no external/local
// resource refs), the slug name rule, and data-nav targets resolving to a
// wireframe stem. Advisory checks (SeverityInfo): frame match, <text>
// usage, and stable ids on interactive elements. The result is never nil.
func ValidateWireframe(relPath string, content []byte, knownStems []string, frames []Frame) []Finding {
	stemSet := make(map[string]struct{}, len(knownStems))
	for _, s := range knownStems {
		stemSet[s] = struct{}{}
	}

	var findings []Finding

	// slug name rule (hard) — the file stem must match the shared slug rule.
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	if !frameNameRe.MatchString(stem) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGSlugName,
			Severity: SeverityError,
			Message:  fmt.Sprintf("wireframe stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}

	walk := walkSVG(content)

	if walk.error != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     walk.errorLine,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  fmt.Sprintf("not well-formed XML: %v", walk.error),
		})
		return finalizeWireframeFindings(findings)
	}

	if !walk.sawRoot {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  "missing root element; a wireframe must be a single <svg> document",
		})
		return finalizeWireframeFindings(findings)
	}

	// root <svg> + integer viewBox (hard).
	switch {
	case !walk.rootIsSVG:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("root element is <%s>, expected <svg>", walk.rootName),
		})
	case walk.viewBoxMissing:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  "root <svg> is missing the viewBox attribute",
		})
	case !walk.viewBoxAllInt:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("viewBox %q must hold four integer values (minX minY width height)", walk.viewBoxRaw),
		})
	}

	// self-containment (hard): no <script>, no external/local resource refs.
	scriptOff := 0
	for range walk.scripts {
		off := findTagOffset(content, "script", scriptOff)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  "wireframes must be self-contained: remove <script>",
		})
		if off >= 0 {
			scriptOff = off + 1
		}
	}
	for _, r := range walk.resourceRefs {
		verdict := classifyResourceRef(r.value)
		if verdict == "" {
			continue
		}
		off := findAttrValueOffset(content, r.attr, r.value, 0)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-containment: %s reference %q in <%s> breaks self-containment (embed a data: URI or reference from brand/)", verdict, r.value, r.tag),
		})
	}

	// data-nav targets must resolve to a wireframe stem (hard); interactive
	// elements without a stable id are advisory.
	for _, d := range walk.dataNavs {
		off := findAttrValueOffset(content, "data-nav", d.value, 0)
		line := lineOfOffset(content, off)
		if _, ok := stemSet[d.value]; !ok {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     line,
				Rule:     ruleSVGDataNavDangling,
				Severity: SeverityError,
				Message:  fmt.Sprintf("data-nav target %q does not match any wireframe stem", d.value),
			})
		}
		if d.value != "" && d.id == "" {
			findings = append(findings, Finding{
				File:     relPath,
				Line:     line,
				Rule:     ruleSVGStableIDs,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("interactive element with data-nav=%q has no stable id attribute", d.value),
			})
		}
	}

	// <text> usage (advisory): a screen wireframe should keep text as <text>.
	if !walk.hasText {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGTextUsage,
			Severity: SeverityInfo,
			Message:  "wireframe contains no <text> elements; keep text as <text> so it stays greppable and accessible",
		})
	}

	// frame match (advisory): the root viewBox W/H should match a declared
	// device frame. Skipped when no frames are declared or the viewBox is
	// absent / non-integer.
	if len(frames) > 0 && walk.rootIsSVG && !walk.viewBoxMissing && walk.viewBoxAllInt {
		if !frameMatches(walk.viewBoxW, walk.viewBoxH, frames) {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleSVGFrameMatch,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("viewBox %dx%d does not match any declared device frame", walk.viewBoxW, walk.viewBoxH),
			})
		}
	}

	// embedded data URI size (advisory warn, SP-140-1 §1h binary hygiene).
	findings = append(findings, ValidateDataURISizes(relPath, content)...)

	// literal fill/stroke/font-family values not backed by a {token.path}
	// comment (advisory info, SP-140-4 §4b "Token usage"): wireframes are
	// low-fidelity drafts so literals are allowed, but they are tracked for sync.
	findings = append(findings, validateTokenUsage(relPath, content)...)

	return finalizeWireframeFindings(findings)
}

// ValidateWireframesDir validates every design/wireframes/*.svg under root,
// SP-140-1 §1b/§1g. All wireframe stems are gathered first so data-nav
// targets resolve across files; device frames are read from the design
// README (when present) to enable the advisory frame-match check. A missing
// or empty wireframes directory yields no findings, not an error — a
// whole-tree validator run must not fail on workspaces without wireframes.
//
// Files are validated in glob order; the combined findings are sorted by
// file, line, rule, message. Errors are I/O failures only — per-file rule
// violations are findings, not errors.
func ValidateWireframesDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "wireframes", "*.svg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
	}

	// Gather the full stem set so data-nav targets resolve across files.
	stems := make([]string, 0, len(matches))
	for _, m := range matches {
		stems = append(stems, strings.TrimSuffix(path.Base(filepath.ToSlash(m)), ".svg"))
	}

	// Device frames from the design README (advisory frame-match only).
	var frames []Frame
	if data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName)); err == nil {
		frames, _ = ParseFrames(string(data))
	}

	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		findings = append(findings, ValidateWireframe(filepath.ToSlash(rel), data, stems, frames)...)
	}

	// SP-140-9 §9a: the wireframe tier is retired — one deprecation notice
	// per file rides the same walk (informational; item 9.4 migrates).
	rels := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			rels = append(rels, filepath.ToSlash(match))
			continue
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	findings = appendWireframeDeprecations(findings, rels)
	sortFindings(findings)
	return findings, nil
}

// ValidateComponent validates one design/components/*.svg component spec,
// SP-140-1 §1b applied to the component tier: a self-contained, low-fidelity
// SVG that shows one reusable UI component in its key variants and states.
// relPath is the design-relative path (e.g. "design/components/button.svg").
//
// Components are the composable layer beneath screens: a screen wireframe
// is a composition of components, so a component spec carries no navigation
// semantics (no data-nav check) and its viewBox is the component's bounding
// box, not a device frame (no frame-match check). Everything else the
// wireframe contract requires applies unchanged: well-formed XML, a root
// <svg> with an integer viewBox, self-containment, the slug name rule,
// <text> labels (advisory), data-URI size (advisory), and token-usage
// comments (advisory).
//
// Hard checks (SeverityError): well-formedness, root/viewBox, self-
// containment, slug name. Advisory (info/warn): text usage, data-URI size,
// token usage. The result is never nil.
func ValidateComponent(relPath string, content []byte) []Finding {
	findings := []Finding{}

	// slug name rule (hard) — the file stem must match the shared slug rule.
	stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
	if !frameNameRe.MatchString(stem) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGSlugName,
			Severity: SeverityError,
			Message:  fmt.Sprintf("component stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}

	walk := walkSVG(content)
	if walk.error != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     walk.errorLine,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  fmt.Sprintf("not well-formed XML: %v", walk.error),
		})
		return finalizeWireframeFindings(findings)
	}
	if !walk.sawRoot {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGWellformed,
			Severity: SeverityError,
			Message:  "missing root element; a component spec must be a single <svg> document",
		})
		return finalizeWireframeFindings(findings)
	}

	// root <svg> + integer viewBox (hard).
	switch {
	case !walk.rootIsSVG:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("root element is <%s>, expected <svg>", walk.rootName),
		})
	case walk.viewBoxMissing:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  "root <svg> is missing the viewBox attribute",
		})
	case !walk.viewBoxAllInt:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGViewBox,
			Severity: SeverityError,
			Message:  fmt.Sprintf("viewBox %q must hold four integer values (minX minY width height)", walk.viewBoxRaw),
		})
	}

	// self-containment (hard): no <script>, no external/local resource refs.
	scriptOff := 0
	for range walk.scripts {
		off := findTagOffset(content, "script", scriptOff)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  "component specs must be self-contained: remove <script>",
		})
		if off >= 0 {
			scriptOff = off + 1
		}
	}
	for _, r := range walk.resourceRefs {
		verdict := classifyResourceRef(r.value)
		if verdict == "" {
			continue
		}
		off := findAttrValueOffset(content, r.attr, r.value, 0)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleSVGSelfContainment,
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-containment: %s reference %q in <%s> breaks self-containment (embed a data: URI or reference from brand/)", verdict, r.value, r.tag),
		})
	}

	// <text> usage (advisory): variant and state labels should stay greppable.
	if !walk.hasText {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleSVGTextUsage,
			Severity: SeverityInfo,
			Message:  "component spec contains no <text> elements; label variants and states with <text> so they stay greppable and accessible",
		})
	}

	// embedded data URI size (advisory warn, SP-140-1 §1h binary hygiene).
	findings = append(findings, ValidateDataURISizes(relPath, content)...)

	// literal fill/stroke/font-family values not backed by a {token.path}
	// comment (advisory info, SP-140-4 §4b "Token usage").
	findings = append(findings, validateTokenUsage(relPath, content)...)

	return finalizeWireframeFindings(findings)
}

// ValidateComponentsDir validates every design/components/*.svg under root.
// A missing or empty components directory yields no findings, not an error —
// a whole-tree validator run must not fail on workspaces without a component
// tier. Findings are sorted by file, line, rule, message; errors are I/O
// failures only.
func ValidateComponentsDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "components", "*.svg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	if len(matches) == 0 {
		return findings, nil
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		findings = append(findings, ValidateComponent(filepath.ToSlash(rel), data)...)
	}
	sortFindings(findings)
	return findings, nil
}

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
