package design

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Rule ids for the token-usage / inventory / naming consistency pack,
// SP-140-4 §4b. Like the flow/wireframe bidirectionality pack
// (consistency.go) this is a part of the SP-140-1g validator — same
// {file, line?, severity, message, rule} findings schema, emitted through the
// standard dispatch in tree.go — rather than a separate tool; the rule ids are
// constants so findings and future tooling share one spelling.
const (
	// ruleSVGTokenUsage is advisory (info): a wireframe carries a literal SVG
	// fill/stroke/font-family value that is not backed by a `{token.path}`
	// comment. Wireframes are low-fidelity drafts, so literal values are allowed — they
	// are tracked so SP-140-5's sync reporting can quantify drift (SP-140-4
	// §4b "Token usage").
	ruleSVGTokenUsage = "svg_token_usage"

	// ruleConsistencyScreenOrphan is advisory (info): a wireframe stem appears
	// in neither a flow (as a node) nor the README (as a Screens listing), so
	// nothing references the screen (SP-140-4 §4b "Screen inventory").
	ruleConsistencyScreenOrphan = "consistency_screen_orphan"

	// ruleConsistencyComponentOrphan is advisory (info): a component stem
	// appears in no README Components listing, so nothing tracks the
	// component in the manifest. Components are referenced by screen
	// compositions rather than flows, so the README listing is the only
	// reference channel (SP-140-4 §4b "Component inventory").
	ruleConsistencyComponentOrphan = "consistency_component_orphan"

	// ruleConsistencyScreenNameMismatch fires (warn) when a delivered
	// design/screens/*.html file has no wireframe counterpart, so the two
	// inventories disagree about which screens exist (SP-140-4 §4b "Naming").
	ruleConsistencyScreenNameMismatch = "consistency_screen_name_mismatch"

	// ruleConsistencyScreenNameDuplicate fires (warn) when two delivered
	// screen files share a stem, so a screen name is ambiguous (SP-140-4 §4b
	// "Naming").
	ruleConsistencyScreenNameDuplicate = "consistency_screen_name_duplicate"
)

// tokenCommentRe matches a design-token reference in a comment — the
// `{group.token}` dot-path form shared with brand.md
// (brandTokenRefRe) — but anchored to a `<!-- ... -->` comment so a token
// reference appearing as literal SVG text (which would also satisfy the
// zero-adoption rule) is not mistaken for a reference on a value.
//
// §4b is explicit that the reference lives in a *comment*: an SVG has no
// attribute-level token syntax, so the wireframe records the intended token as
// `<!-- {color.semantic.danger} -->` next to `fill="red"`. The comment may
// precede the value on its own line (the idiom the conventions document) or
// trail the element; either way it associates with the element it sits beside.
var tokenCommentRe = regexp.MustCompile(`<!--[^>]*\{[A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z0-9_-]+)+\}[^>]*-->`)

// cssNamedColors is the CSS named-color set (the small, stable subset a
// wireframe plausibly uses). A fill/stroke value that is a plain identifier in
// this set is a literal color; an identifier that is not is treated as a
// non-color value (e.g. `none`, `currentColor`, a gradient id) and ignored.
var cssNamedColors = map[string]struct{}{
	"aliceblue": {}, "antiquewhite": {}, "aqua": {}, "aquamarine": {},
	"azure": {}, "beige": {}, "bisque": {}, "black": {}, "blanchedalmond": {},
	"blue": {}, "blueviolet": {}, "brown": {}, "burlywood": {}, "cadetblue": {},
	"chartreuse": {}, "chocolate": {}, "coral": {}, "cornflowerblue": {},
	"cornsilk": {}, "crimson": {}, "cyan": {}, "darkblue": {}, "darkcyan": {},
	"darkgoldenrod": {}, "darkgray": {}, "darkgreen": {}, "darkgrey": {},
	"darkkhaki": {}, "darkmagenta": {}, "darkolivegreen": {}, "darkorange": {},
	"darkorchid": {}, "darkred": {}, "darksalmon": {}, "darkseagreen": {},
	"darkslateblue": {}, "darkslategray": {}, "darkslategrey": {},
	"darkturquoise": {}, "darkviolet": {}, "deeppink": {}, "deepskyblue": {},
	"dimgray": {}, "dimgrey": {}, "dodgerblue": {}, "firebrick": {},
	"floralwhite": {}, "forestgreen": {}, "fuchsia": {}, "gainsboro": {},
	"ghostwhite": {}, "gold": {}, "goldenrod": {}, "gray": {}, "green": {},
	"greenyellow": {}, "grey": {}, "honeydew": {}, "hotpink": {},
	"indianred": {}, "indigo": {}, "ivory": {}, "khaki": {}, "lavender": {},
	"lavenderblush": {}, "lawngreen": {}, "lemonchiffon": {}, "lightblue": {},
	"lightcoral": {}, "lightcyan": {}, "lightgoldenrodyellow": {},
	"lightgray": {}, "lightgreen": {}, "lightgrey": {}, "lightpink": {},
	"lightsalmon": {}, "lightseagreen": {}, "lightskyblue": {},
	"lightslategray": {}, "lightslategrey": {}, "lightsteelblue": {},
	"lightyellow": {}, "lime": {}, "limegreen": {}, "linen": {}, "magenta": {},
	"maroon": {}, "mediumaquamarine": {}, "mediumblue": {}, "mediumorchid": {},
	"mediumpurple": {}, "mediumseagreen": {}, "mediumslateblue": {},
	"mediumspringgreen": {}, "mediumturquoise": {}, "mediumvioletred": {},
	"midnightblue": {}, "mintcream": {}, "mistyrose": {}, "moccasin": {},
	"navajowhite": {}, "navy": {}, "oldlace": {}, "olive": {}, "olivedrab": {},
	"orange": {}, "orangered": {}, "orchid": {}, "palegoldenrod": {},
	"palegreen": {}, "paleturquoise": {}, "palevioletred": {}, "papayawhip": {},
	"peachpuff": {}, "peru": {}, "pink": {}, "plum": {}, "powderblue": {},
	"purple": {}, "rebeccapurple": {}, "red": {}, "rosybrown": {}, "royalblue": {},
	"saddlebrown": {}, "salmon": {}, "sandybrown": {}, "seagreen": {},
	"seashell": {}, "sienna": {}, "silver": {}, "skyblue": {}, "slateblue": {},
	"slategray": {}, "slategrey": {}, "snow": {}, "springgreen": {},
	"steelblue": {}, "tan": {}, "teal": {}, "thistle": {}, "tomato": {},
	"turquoise": {}, "violet": {}, "wheat": {}, "white": {}, "whitesmoke": {},
	"yellow": {}, "yellowgreen": {},
}

// svgColorLiteralRe matches a CSS color-function value a wireframe may inline
// (rgb()/rgba()/hsl()/hsla()).
var svgColorLiteralRe = regexp.MustCompile(`(?i)^(?:rgb|rgba|hsl|hsla)\(`)

// isColorLiteral reports whether a fill/stroke attribute value is a literal
// color: a hex code, an rgb()/hsl() function, or a CSS named color. A `url(#…)`
// paint server, `none`, `currentColor`, an empty value, and an unknown
// identifier are not literal colors and are ignored.
func isColorLiteral(v string) bool {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return false
	case strings.HasPrefix(v, "#"):
		return true
	case strings.HasPrefix(v, "url("):
		return false
	case svgColorLiteralRe.MatchString(v):
		return true
	case strings.ContainsAny(v, " \t\n"):
		// Only the first token is a color (e.g. `red url(#g)`); recurse.
		return isColorLiteral(strings.Fields(v)[0])
	}
	_, ok := cssNamedColors[strings.ToLower(v)]
	return ok
}

// fontFamilyIsLiteral reports whether a font-family value names a concrete font
// — the first family in the CSS fallback list, per §4b "literal … font names".
// A negligible `font-family="inherit"`-style value and an empty value are
// ignored.
func fontFamilyIsLiteral(v string) bool {
	first := strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
	if i := strings.IndexByte(first, ','); i >= 0 {
		first = strings.ToLower(strings.Trim(strings.TrimSpace(first[:i]), `"'`))
	}
	switch first {
	case "", "inherit", "initial", "unset", "revert", "revert-layer":
		return false
	}
	return true
}

// tokenUsageAttr is one literal SVG styling attribute found during the
// token-usage walk: the attribute name, its literal value, whether it came from
// a styling element (a <text> with a literal font-family rather than a paint
// attribute), the absolute byte offset of the value (for best-effort line
// support), and the [start, end) byte span of the element's whole markup so the
// backing check can scope a comment to the element it sits in.
type tokenUsageAttr struct {
	attr    string
	value   string
	offset  int
	elemBeg int
	elemEnd int
}

// validateTokenUsage flags wireframes that carry literal fill/stroke/
// font-family values not backed by a `{token.path}` comment, SP-140-4 §4b
// "Token usage". Wireframes are low-fidelity drafts — literals are allowed — so
// the finding is advisory `info`, emitted so SP-140-5's sync reporting can
// quantify how much of the tree is still un-tokenized.
//
// A literal value is "backed" when a token-reference comment sits beside it —
// on the same source line, or anywhere inside the same element's markup — which
// is the `fill="red"` + `<!-- {color.semantic.danger} -->` idiom §4b describes.
// A comment on a different element does not back it.
//
// The document-wide special case is a wireframe whose *only* token comments are
// positional — a header/footer note such as `<!-- tokens: {color.brand.primary}
// -->` that none of its literals sit beside. There the check degrades to "has
// the wireframe adopted token references at all?" (zero adoption is flagged
// with an explanatory message) rather than flagging every literal against a
// comment the author did write.
//
// A document with no literals at all is clean, so the valid-tree fixtures stay
// finding-free. The result is sorted and never nil.
func validateTokenUsage(relPath string, content []byte) []Finding {
	attrs := svgTokenUsageAttrs(content)
	if len(attrs) == 0 {
		return []Finding{}
	}

	comments := tokenCommentSpans(content)
	if len(comments) == 0 {
		f := Finding{
			File:     relPath,
			Line:     lineOfOffset(content, attrs[0].offset),
			Rule:     ruleSVGTokenUsage,
			Severity: SeverityInfo,
			Message: fmt.Sprintf(
				"wireframe has %d literal fill/stroke/font-family value(s) and no {token.path} comment; wireframes may use literals, but reference tokens in a comment (e.g. <!-- {color.brand.primary} -->) so sync can track drift",
				len(attrs)),
		}
		return finalizeWireframeFindings([]Finding{f})
	}

	var findings []Finding
	for _, a := range attrs {
		if literalBackedByTokenComment(content, a, comments) {
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, a.offset),
			Rule:     ruleSVGTokenUsage,
			Severity: SeverityInfo,
			Message: fmt.Sprintf(
				"literal %s %q is not backed by a {token.path} comment; add one beside the value (e.g. <!-- {color.brand.primary} -->) so sync can track it",
				a.attr, a.value),
		})
	}
	if len(findings) == 0 {
		// Every literal is backed by a beside-it token comment: the wireframe
		// has adopted token references. Clean.
		return []Finding{}
	}
	return finalizeWireframeFindings(findings)
}

// literalBackedByTokenComment reports whether a token-reference comment backs
// the literal attribute a: the comment shares the value's source line, or it
// lies inside the element's markup (a direct child comment, or one wrapping the
// element's opening tag). Either form places the comment visibly beside the
// value so a reader knows which token the literal stands for.
func literalBackedByTokenComment(content []byte, a tokenUsageAttr, comments [][2]int) bool {
	line := lineOfOffset(content, a.offset)
	for _, c := range comments {
		if lineOfOffset(content, c[0]) == line {
			return true
		}
		if a.elemEnd > a.elemBeg && c[0] >= a.elemBeg && c[1] <= a.elemEnd {
			return true
		}
	}
	return false
}

// tokenCommentSpans returns the [start, end) byte spans of every
// token-reference comment in content, in document order.
func tokenCommentSpans(content []byte) [][2]int {
	idxs := tokenCommentRe.FindAllIndex(content, -1)
	spans := make([][2]int, 0, len(idxs))
	for _, idx := range idxs {
		spans = append(spans, [2]int{idx[0], idx[1]})
	}
	return spans
}

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

// ValidateInventory runs the screen-inventory pack over the whole design tree
// under root, SP-140-4 §4b "Screen inventory": every wireframe stem must appear
// in either a flow (as a node) or the README (as a Screens listing). A stem
// that appears in neither is an orphan screen, surfaced as `info` — the screen
// exists on disk but nothing navigates to it or tracks it.
//
// The tree's per-artifact rules stay with their own validators; this pack only
// reads the stems and the cross-artifact references. A workspace with no
// design/ tree yields no findings; the result is sorted and never nil.
func ValidateInventory(root string) []Finding {
	wireframeStems := assetStems(root, "wireframes", ".svg")
	if len(wireframeStems) == 0 {
		return []Finding{}
	}

	// A flow references a stem when a node id equals it; a README references a
	// stem when a Screens listing names it. Both reuse the §4b parsers.
	flowRefs := flowNodeStems(root)
	readmeRefs := readmeScreenEntries(root)

	var findings []Finding
	for _, stem := range wireframeStems {
		if flowRefs[stem] || readmeRefs[stem] {
			continue
		}
		findings = append(findings, Finding{
			File:     path.Join(DirName, "wireframes", stem+".svg"),
			Rule:     ruleConsistencyScreenOrphan,
			Severity: SeverityInfo,
			Message: fmt.Sprintf(
				"orphan screen: wireframe %q appears in no flow (as a node) and in no README Screens listing; add it to a flow or list it in the README",
				stem),
		})
	}
	sortFindings(findings)
	return findings
}

// flowNodeStems returns the set of node ids referenced across every
// design/flows/*.mmd file. A node counts whether it is an edge endpoint or a
// declared node in the flow, matching the §1c id == stem contract: a flow that
// taps into a screen keeps a reference to it either way.
func flowNodeStems(root string) map[string]bool {
	out := map[string]bool{}
	matches, err := filepath.Glob(filepath.Join(root, DirName, "flows", "*.mmd"))
	if err != nil {
		return out
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		fc := ParseFlowchart(string(data))
		for _, id := range fc.NodeOrder {
			out[id] = true
		}
		for _, e := range fc.Edges {
			out[e.Source] = true
			out[e.Target] = true
		}
	}
	return out
}

// readmeScreenEntries returns the set of names named by the README manifest's
// Screens listing bullets, reusing the §4b parser so the inventory and the
// bidirectionality pack agree on what "listed in the README" means. A missing
// README yields an empty set (every wireframe then pairs with a flow or is an
// orphan).
func readmeScreenEntries(root string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return out
	}
	for _, ref := range readmeScreenRefs(string(data)) {
		if ref.section == "Screens" {
			out[ref.name] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Component inventory — orphan components
// ---------------------------------------------------------------------------

// ValidateComponentInventory runs the component-inventory pack over the whole
// design tree under root, SP-140-4 §4b "Component inventory": every component
// stem must appear in the README's Components listing. A component that
// appears there is orphaned — it exists on disk but the manifest does not
// track it — and is surfaced as `info`. Components are referenced by screen
// compositions (README Composition table, data-component attributes) rather
// than by flows, so the README listing is their only reference channel;
// unlike screens, a flow reference does not rescue a component.
//
// A workspace with no components yields no findings; the result is sorted and
// never nil.
func ValidateComponentInventory(root string) []Finding {
	componentStems := assetStems(root, "components", ".svg")
	if len(componentStems) == 0 {
		return []Finding{}
	}

	readmeRefs := readmeComponentEntries(root)

	var findings []Finding
	for _, stem := range componentStems {
		if readmeRefs[stem] {
			continue
		}
		findings = append(findings, Finding{
			File:     path.Join(DirName, "components", stem+".svg"),
			Rule:     ruleConsistencyComponentOrphan,
			Severity: SeverityInfo,
			Message: fmt.Sprintf(
				"orphan component: %q appears in no README Components listing; list it in the manifest so the composition table can reference it",
				stem),
		})
	}
	sortFindings(findings)
	return findings
}

// readmeComponentEntries returns the set of names named by the README
// manifest's Components listing bullets, reusing the §4b parser so the
// inventory and the bidirectionality pack agree on what "listed in the
// README" means. A missing README yields an empty set (every component is
// then an orphan).
func readmeComponentEntries(root string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return out
	}
	for _, ref := range readmeScreenRefs(string(data)) {
		if ref.section == "Components" {
			out[ref.name] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Naming — screen names across wireframes/ and screens/
// ---------------------------------------------------------------------------

// screenSlugViolations reads every design/screens/*.html file and reports the
// stems that break the shared slug rule, as a warn. The stem is the screen
// name; a non-slug stem cannot be a wireframe counterpart and cannot appear in
// a flow or listing, so it is a naming inconsistency rather than the hard
// per-file rule (which validateScreen keeps as ruleScreenSlugName for a
// single-file run). A missing screens directory yields no findings.
func screenSlugViolations(root string) []Finding {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html"))
	if err != nil {
		return []Finding{}
	}
	sort.Strings(matches)
	var findings []Finding
	for _, match := range matches {
		stem := strings.TrimSuffix(path.Base(filepath.ToSlash(match)), ".html")
		if frameNameRe.MatchString(stem) {
			continue
		}
		findings = append(findings, Finding{
			File:     relAsset(root, match),
			Rule:     ruleScreenSlugName,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("screen stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}
	return findings
}

// screenStemRecord is one delivered screen file paired with its stem, the
// unit the naming comparison groups.
type screenStemRecord struct {
	stem string
	file string
}

// screenNameMismatches compares the design/screens/ inventory against the
// design/wireframes/ inventory by stem, SP-140-4 §4b "Naming", adjusted by
// SP-140-9: the wireframe tier is deprecated (9.4 migrates it), so a
// delivered screen whose stem has no wireframe counterpart is now
// informational, pointing at the migration instead of demanding the
// counterpart. Counterpart-present checks stay intact — a wireframe with a
// different case or a stale counterpart is still the mismatch signal it was
// (SP-140-4 §4b); only the counterpart-missing direction relaxes. Two
// findings exist, both advisory:
//
//   - a delivered screen whose stem has no wireframe counterpart (info,
//     ruleConsistencyScreenNameMismatch — remedied by 9.4's migration);
//   - two delivered screen files sharing a stem (warn,
//     ruleConsistencyScreenNameDuplicate) — an ambiguous screen name.
//
// A wireframe with no delivered screen is the normal pre-code state and is
// never a mismatch (the orphan rule covers an unreferenced wireframe). Missing
// directories yield no findings; the result is sorted.
func screenNameMismatches(root string) []Finding {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html"))
	if err != nil || len(matches) == 0 {
		return []Finding{}
	}
	sort.Strings(matches)

	records := make([]screenStemRecord, 0, len(matches))
	for _, match := range matches {
		records = append(records, screenStemRecord{
			stem: strings.TrimSuffix(path.Base(filepath.ToSlash(match)), ".html"),
			file: relAsset(root, match),
		})
	}

	wireframeStems := map[string]bool{}
	for _, s := range assetStems(root, "wireframes", ".svg") {
		wireframeStems[s] = true
	}

	return nameMismatchesForStems(records, wireframeStems)
}

// nameMismatchesForStems is the naming comparison core: given the delivered
// screen records and the wireframe stem set, it returns the mismatch and
// duplicate findings sorted by file. It is split out so the duplicate-stem
// branch — unreachable through a real glob, since a filesystem cannot hold two
// identical file names — is testable directly.
func nameMismatchesForStems(records []screenStemRecord, wireframeStems map[string]bool) []Finding {
	byStem := map[string][]string{}
	for _, r := range records {
		byStem[r.stem] = append(byStem[r.stem], r.file)
	}

	var findings []Finding
	stems := make([]string, 0, len(byStem))
	for stem := range byStem {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	for _, stem := range stems {
		files := byStem[stem]
		if len(files) > 1 {
			sort.Strings(files)
			for _, f := range files {
				findings = append(findings, Finding{
					File:     f,
					Rule:     ruleConsistencyScreenNameDuplicate,
					Severity: SeverityWarn,
					Message:  fmt.Sprintf("duplicate screen name %q: %d files in %s/ share the stem (%s); a screen name must be unique", stem, len(files), path.Join(DirName, "screens"), strings.Join(files, ", ")),
				})
			}
			continue
		}
		if wireframeStems[stem] {
			continue
		}
		if len(wireframeStems) == 0 {
			// Post-9.4 trees carry no wireframes at all; the counterpart
			// question no longer applies (screens are the primary tier,
			// §9a) and firing here would flag every screen forever.
			continue
		}
		findings = append(findings, Finding{
			File:     files[0],
			Rule:     ruleConsistencyScreenNameMismatch,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("screen %q has no wireframe counterpart (expected %s); the wireframe tier is deprecated (SP-140-9 §9a), item 9.4 migrates it", stem, path.Join(DirName, "wireframes", stem+".svg")),
		})
	}
	return findings
}
