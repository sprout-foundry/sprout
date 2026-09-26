package design

// inventory_rules.go — the token-usage / inventory / naming-consistency
// validator pack (SP-140-4 §4b): the rule-id constants, the token-usage
// validators (validateTokenUsage + the color / font-family / token-comment
// helpers), and the two top-level dispatches (ValidateInventory,
// ValidateComponentInventory). The SVG attribute-parsing helpers live in
// inventory_rules_svg.go; the screen / component naming + inventory helpers
// in inventory_rules_naming.go.

import (
	"fmt"
	"path"
	"regexp"
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
