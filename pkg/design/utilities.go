package design

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The SP-143 §143.1 utility vocabulary: a fixed set of known token groups
// that map to utility classes in the css export target, so a screen styles
// with `.bg-*`/`.p-*` instead of re-deriving CSS from scratch. Unknown
// groups pass through as variables only — the vocabulary is closed on
// purpose (SP-143 Premise §1), so a project tier named `sizing` never
// generates surprise classes.
const (
	UtilityGroupColor  = "color"
	UtilityGroupSpace  = "space"
	UtilityGroupFont   = "font"
	UtilityGroupRadius = "radius"
	UtilityGroupShadow = "shadow"
)

// fontGroupNames are the group names the font vocabulary recognizes.
// SP-143 §143.1 names `font.*`; the repo's own token tier (and the
// design-system skill's canonical tier list) names it `typography.*`, so
// both map onto the font utilities. Keeping the two names in one table is
// the whole difference — anything else (sizing, duration, easing, …) stays
// variable-only.
var fontGroupNames = map[string]bool{
	UtilityGroupFont: true,
	"typography":     true,
}

// cssBoxShadowLengthRE detects a length inside a shadow value. A shadow
// token whose value carries no length (an inset-highlight *color*, as in
// the repo's own shadow tier) cannot be emitted as a box-shadow utility —
// `box-shadow: rgba(...)` is invalid CSS — so it stays variable-only.
var cssBoxShadowLengthRE = regexp.MustCompile(`[0-9](px|em|rem)`)

// utilityClass is one generated utility class in the css target's utility
// layer.
type utilityClass struct {
	// Name is the class name without the leading dot (bg-dark-bg-primary).
	Name string
	// Property is the CSS property the class sets.
	Property string
	// Value is the declaration value (always var(--token) — the utility
	// layer never bakes a literal, so a token edit re-themes every screen
	// on re-export).
	Value string
	// Family is the human label emitted as the family's section comment.
	Family string
}

// utilityFamilies is the fixed emission order of the utility layer: the
// spec's vocabulary order (color bg/text/border, space p/m/gap, font
// family/size/weight, radius, shadow). Rendering iterates this slice, never
// a map, so the bytes cannot shuffle between runs.
var utilityFamilies = []string{
	"bg",
	"text",
	"border",
	"padding",
	"margin",
	"gap",
	"font",
	"text-size",
	"text-weight",
	"rounded",
	"shadow",
}

// utilityClassesFor projects a resolved token set into the utility
// vocabulary. The result is grouped by family in utilityFamilies order and
// sorted by class name within a family; a token may contribute to several
// families (a color yields bg, text, and border) or none (an unknown group,
// or a value shape the family cannot express). The result is never nil.
func utilityClassesFor(tokens *TokenExport) []utilityClass {
	byFamily := make(map[string][]utilityClass, len(utilityFamilies))
	for _, t := range tokens.Leaves {
		for _, uc := range utilityClassesForToken(t) {
			byFamily[uc.Family] = append(byFamily[uc.Family], uc)
		}
	}
	out := make([]utilityClass, 0, len(tokens.Leaves)*2)
	for _, family := range utilityFamilies {
		classes := byFamily[family]
		if len(classes) == 0 {
			continue
		}
		sort.Slice(classes, func(i, j int) bool { return classes[i].Name < classes[j].Name })
		out = append(out, classes...)
	}
	return out
}

// utilityClassesForToken maps one token leaf onto its utility classes per
// the vocabulary: the group name picks the families, the leaf's $type and
// value shape decide whether each family can express the token.
func utilityClassesForToken(t ExportedToken) []utilityClass {
	switch {
	case strings.HasPrefix(t.Name, UtilityGroupColor+"."):
		if t.Type != "color" {
			return nil
		}
		suffix := utilitySuffix(t.Name, UtilityGroupColor)
		return []utilityClass{
			{Family: "bg", Name: "bg-" + suffix, Property: "background-color", Value: utilityVar(t)},
			{Family: "text", Name: "text-" + suffix, Property: "color", Value: utilityVar(t)},
			{Family: "border", Name: "border-" + suffix, Property: "border-color", Value: utilityVar(t)},
		}

	case strings.HasPrefix(t.Name, UtilityGroupSpace+"."):
		if t.Type != "dimension" {
			return nil
		}
		suffix := utilitySuffix(t.Name, UtilityGroupSpace)
		return []utilityClass{
			{Family: "padding", Name: "p-" + suffix, Property: "padding", Value: utilityVar(t)},
			{Family: "margin", Name: "m-" + suffix, Property: "margin", Value: utilityVar(t)},
			{Family: "gap", Name: "gap-" + suffix, Property: "gap", Value: utilityVar(t)},
		}

	case isFontGroupName(t.Name):
		suffix := utilityFontSuffix(t.Name)
		switch t.Type {
		case "fontFamily":
			return []utilityClass{{Family: "font", Name: "font-" + suffix, Property: "font-family", Value: utilityVar(t)}}
		case "dimension":
			return []utilityClass{{Family: "text-size", Name: "text-" + suffix + "-size", Property: "font-size", Value: utilityVar(t)}}
		case "fontWeight":
			return []utilityClass{{Family: "text-weight", Name: "text-" + suffix + "-weight", Property: "font-weight", Value: utilityVar(t)}}
		default:
			// line-height numbers and anything else in the font group stay
			// variable-only: the vocabulary is text sizes and weights.
			return nil
		}

	case strings.HasPrefix(t.Name, UtilityGroupRadius+"."):
		if t.Type != "dimension" {
			return nil
		}
		suffix := utilitySuffix(t.Name, UtilityGroupRadius)
		return []utilityClass{{Family: "rounded", Name: "rounded-" + suffix, Property: "border-radius", Value: utilityVar(t)}}

	case strings.HasPrefix(t.Name, UtilityGroupShadow+"."):
		if t.Type != "dimension" || !cssBoxShadowLengthRE.MatchString(utilityLiteral(t)) {
			return nil
		}
		suffix := utilitySuffix(t.Name, UtilityGroupShadow)
		return []utilityClass{{Family: "shadow", Name: "shadow-" + suffix, Property: "box-shadow", Value: utilityVar(t)}}

	default:
		// Unknown group: variable-only, per the closed vocabulary.
		return nil
	}
}

// isFontGroupName reports whether the token path lives in a group the font
// vocabulary recognizes.
func isFontGroupName(name string) bool {
	dot := strings.Index(name, ".")
	if dot < 0 {
		return false
	}
	return fontGroupNames[name[:dot]]
}

// utilitySuffix sanitizes the token path after the group prefix into a class
// suffix, with the same collapsing rules as the CSS variable names — so
// `color.dark.accent.warning-fg` yields the class stem `dark-accent-warning-fg`
// that visibly pairs with `--color-dark-accent-warning-fg`.
func utilitySuffix(name, group string) string {
	return saneName(strings.TrimPrefix(name, group+"."), '-', false)
}

// utilityFontSuffix sanitizes everything after the font group into a class
// suffix, dropping the kind sub-group when it would duplicate the class
// family (`typography.font.sans` → `.font-sans`, `typography.size.base` →
// `.text-base-size`) but keeping a lone leaf (`font.mono` → `.font-mono`).
func utilityFontSuffix(name string) string {
	dot := strings.Index(name, ".")
	segments := strings.Split(name[dot+1:], ".")
	if len(segments) > 1 {
		switch segments[0] {
		case "font", "size", "weight":
			segments = segments[1:]
		}
	}
	return saneName(strings.Join(segments, "."), '-', false)
}

// utilityVar renders the class's declaration value: a reference to the
// token's generated variable. Aliases need no special casing here — the
// :root block already chains them.
func utilityVar(t ExportedToken) string {
	return "var(--" + cssVarStem(t.Name) + ")"
}

// utilityLiteral stringifies a token the way the CSS target would, for value
// shape checks (the shadow length test).
func utilityLiteral(t ExportedToken) string {
	return cssLiteral(t)
}

// renderCSSUtilities emits the utility layer appended under the :root block:
// one section comment, then a family comment and one rule per class per
// family, in utilityFamilies order. A token set with no utility-mappable
// group emits nothing — a sheet of pure variables stays a sheet of pure
// variables.
func renderCSSUtilities(tokens *TokenExport) []byte {
	classes := utilityClassesFor(tokens)
	if len(classes) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("\n/* Utility classes generated from the token groups above (SP-143 §143.1).\n")
	b.WriteString("   Reference these from screens instead of raw values: a token edit plus\n")
	b.WriteString("   a re-export re-themes every screen with zero screen-file writes. */\n")

	family := ""
	for _, c := range classes {
		if c.Family != family {
			family = c.Family
			fmt.Fprintf(&b, "\n/* %s */\n", utilityFamilyLabel(family))
		}
		fmt.Fprintf(&b, ".%s { %s: %s; }\n", c.Name, c.Property, c.Value)
	}
	return b.Bytes()
}

// utilityFamilyLabel renders the section comment for a family.
func utilityFamilyLabel(family string) string {
	switch family {
	case "bg":
		return "color → backgrounds"
	case "text":
		return "color → text"
	case "border":
		return "color → borders"
	case "padding":
		return "space → padding"
	case "margin":
		return "space → margin"
	case "gap":
		return "space → gap"
	case "font":
		return "font → families"
	case "text-size":
		return "font → sizes"
	case "text-weight":
		return "font → weights"
	case "rounded":
		return "radius"
	case "shadow":
		return "shadow"
	default:
		return family
	}
}
