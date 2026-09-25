package design

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// utilitiesFixtureDocument covers every vocabulary branch: color (with an
// alias), space, typography (the repo's font tier name), font (the spec's
// name), radius, and shadow with both a length shadow and a color-only
// inset-highlight that must NOT become a box-shadow utility. sizing/duration
// are unknown groups — variables only.
const utilitiesFixtureDocument = `{
  "color": {
    "bg": { "primary": { "$type": "color", "$value": "#101010" } },
    "text": { "muted": { "$type": "color", "$value": "{color.bg.primary}" } }
  },
  "space": {
    "4": { "$type": "dimension", "$value": "8px" },
    "8": { "$type": "dimension", "$value": "16px" }
  },
  "typography": {
    "font": { "sans": { "$type": "fontFamily", "$value": "Inter, sans-serif" } },
    "size": { "base": { "$type": "dimension", "$value": "14px" } },
    "weight": { "bold": { "$type": "fontWeight", "$value": 700 } },
    "line-height": { "body": { "$type": "number", "$value": 1.5 } }
  },
  "font": {
    "mono": { "$type": "fontFamily", "$value": "ui-monospace, monospace" }
  },
  "radius": { "lg": { "$type": "dimension", "$value": "8px" } },
  "shadow": {
    "elevation": { "subtle": { "$type": "dimension", "$value": "0 2px 6px rgba(0,0,0,0.3)" } },
    "inset": { "3": { "$type": "color", "$value": "rgba(255,255,255,0.03)" } }
  },
  "sizing": { "sidebar": { "$type": "dimension", "$value": "288px" } },
  "duration": { "fast": { "$type": "duration", "$value": "150ms" } }
}`

// utilityClassNames renders the emitted utility layer as a flat set of class
// names, for readable assertions.
func utilityClassNames(t *testing.T, tokens *TokenExport) []string {
	t.Helper()
	classes := utilityClassesFor(tokens)
	names := make([]string, 0, len(classes))
	for _, c := range classes {
		names = append(names, c.Name)
	}
	return names
}

func TestUtilityClasses_ColorFamily(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	names := utilityClassNames(t, tokens)

	for _, want := range []string{
		"bg-bg-primary",
		"text-bg-primary",
		"border-bg-primary",
		"bg-text-muted",
		"text-text-muted",
		"border-text-muted",
	} {
		assert.Contains(t, names, want)
	}
}

func TestUtilityClasses_SpaceFamily(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	names := utilityClassNames(t, tokens)
	for _, want := range []string{"p-4", "m-4", "gap-4", "p-8", "m-8", "gap-8"} {
		assert.Contains(t, names, want)
	}
}

func TestUtilityClasses_FontFamilies(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	names := utilityClassNames(t, tokens)

	// font families → .font-*, sizes → .text-*-size, weights → .text-*-weight.
	for _, want := range []string{"font-sans", "text-base-size", "text-bold-weight", "font-mono"} {
		assert.Contains(t, names, want)
	}
	// line-height numbers are outside the vocabulary (text sizes/weights only).
	assert.NotContains(t, names, "text-body-line-height")
	assert.NotContains(t, names, "line-height-body")
}

func TestUtilityClasses_RadiusAndShadow(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	names := utilityClassNames(t, tokens)

	assert.Contains(t, names, "rounded-lg")
	// A length-bearing shadow becomes a box-shadow utility…
	assert.Contains(t, names, "shadow-elevation-subtle")
	// …a color-only inset-highlight cannot (box-shadow: rgba(...) is invalid).
	assert.NotContains(t, names, "shadow-inset-3")
}

func TestUtilityClasses_UnknownGroupsPassThrough(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	names := utilityClassNames(t, tokens)
	assert.NotContains(t, names, "p-sidebar")
	assert.NotContains(t, names, "duration-fast")
	assert.NotContains(t, names, "fast")
}

func TestUtilityClasses_EmissionOrderIsFamilyThenName(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	classes := utilityClassesFor(tokens)
	require.NotEmpty(t, classes)

	seenFamily := map[string]int{}
	order := make([]string, 0, len(classes))
	for _, c := range classes {
		if len(order) == 0 || order[len(order)-1] != c.Family {
			order = append(order, c.Family)
		}
		seenFamily[c.Family]++
		_ = seenFamily
	}
	// Families appear in the fixed vocabulary order.
	familyRank := map[string]int{}
	for i, f := range utilityFamilies {
		familyRank[f] = i
	}
	for i := 1; i < len(order); i++ {
		require.Less(t, familyRank[order[i-1]], familyRank[order[i]],
			"families must emit in utilityFamilies order, got %v", order)
	}
	// Within a family, class names ascend.
	for _, family := range utilityFamilies {
		var names []string
		for _, c := range classes {
			if c.Family == family {
				names = append(names, c.Name)
			}
		}
		require.True(t, sortedStrings(names), "family %s must emit sorted, got %v", family, names)
	}
}

func sortedStrings(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}

func TestUtilityClasses_ValuesAreVarReferences(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	varRe := regexp.MustCompile(`^var\(--(.+)\)$`)
	for _, c := range utilityClassesFor(tokens) {
		m := varRe.FindStringSubmatch(c.Value)
		require.NotNilf(t, m, "%s must reference a token variable, got %q", c.Name, c.Value)
		assert.Equal(t, cssVarStem(c.Value[len("var(--"):len(c.Value)-1]), m[1])
	}
}

func TestUtilityClasses_AliasedTokenUsesOwnVar(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	for _, c := range utilityClassesFor(tokens) {
		if c.Name == "text-text-muted" {
			assert.Equal(t, "var(--color-text-muted)", c.Value,
				"an aliased color utility references the alias leaf's own variable (the :root block chains it)")
		}
	}
}

func TestCSSUtilities_LayerAppendedUnderRoot(t *testing.T) {
	out := string(mustRenderCSS(t, utilitiesFixtureDocument))

	assert.Contains(t, out, ".bg-bg-primary { background-color: var(--color-bg-primary); }")
	assert.Contains(t, out, ".text-text-muted { color: var(--color-text-muted); }")
	assert.Contains(t, out, ".p-4 { padding: var(--space-4); }")
	assert.Contains(t, out, ".font-sans { font-family: var(--typography-font-sans); }")
	assert.Contains(t, out, ".font-mono { font-family: var(--font-mono); }")
	assert.Contains(t, out, ".text-base-size { font-size: var(--typography-size-base); }")
	assert.Contains(t, out, ".text-bold-weight { font-weight: var(--typography-weight-bold); }")
	assert.Contains(t, out, ".rounded-lg { border-radius: var(--radius-lg); }")
	assert.Contains(t, out, ".shadow-elevation-subtle { box-shadow: var(--shadow-elevation-subtle); }")

	// Exactly one :root block; the utility layer sits after it.
	assert.Equal(t, 1, countOccurrences(out, ":root {"))
	utilities := strings.Index(out, "/* Utility classes")
	rootEnd := strings.Index(out, "}\n")
	require.Greater(t, utilities, rootEnd, "the layer opens after the variables close")
	assert.Regexp(t, `(?s):root \{.*?\}\n\n/\* Utility classes`, out)
}

func TestCSSUtilities_NoUtilityGroupsMeansNoLayer(t *testing.T) {
	out := string(mustRenderCSS(t, `{
  "sizing": { "sidebar": { "$type": "dimension", "$value": "288px" } }
}`))
	assert.NotContains(t, out, "Utility classes")
	assert.True(t, strings.HasSuffix(out, "}\n"))
}

func TestCSSUtilities_DeterministicAcrossRuns(t *testing.T) {
	first := mustRenderCSS(t, utilitiesFixtureDocument)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, mustRenderCSS(t, utilitiesFixtureDocument), "run %d drifted", i+2)
	}
}

// -----------------------------------------------------------------------------
// The json target (SP-143 §143.1)
// -----------------------------------------------------------------------------

func TestRenderJSONTokens_ParsesAndCarriesValues(t *testing.T) {
	out, err := RenderExport(ExportTargetJSON, mustTokens(t, utilitiesFixtureDocument))
	require.NoError(t, err)

	var doc struct {
		Provenance string         `json:"provenance"`
		SourceHash string         `json:"source-hash"`
		Source     string         `json:"source"`
		Tokens     map[string]any `json:"tokens"`
		CSSVars    map[string]any `json:"cssVars"`
		Extra      map[string]any `json:"-"`
	}
	require.NoError(t, json.Unmarshal(out, &doc), "the json target must be valid JSON")

	assert.Contains(t, doc.Provenance, "target=json")
	assert.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, doc.SourceHash)
	assert.Contains(t, doc.Source, "design/tokens/*.tokens.json")

	// Native JSON values: numbers stay numbers, aliases resolve to values.
	assert.Equal(t, "#101010", doc.Tokens["color.bg.primary"])
	assert.Equal(t, float64(700), doc.Tokens["typography.weight.bold"])
	assert.Equal(t, "{color.bg.primary}" == doc.Tokens["color.text.muted"], false,
		"an alias leaf resolves to its final value")
	assert.Equal(t, "#101010", doc.Tokens["color.text.muted"])
	assert.Equal(t, "--color-bg-primary", doc.CSSVars["color.bg.primary"])
}

func TestRenderJSONTokens_HashMatchesInputHash(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", utilitiesFixtureDocument)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	out, err := RenderExport(ExportTargetJSON, tokens)
	require.NoError(t, err)
	assert.Equal(t, tokens.InputHash, provenanceSourceHash(string(out)),
		"the shared extractor reads the JSON banner")
}

func TestRenderJSONTokens_DeterministicAcrossRuns(t *testing.T) {
	tokens := mustTokens(t, utilitiesFixtureDocument)
	first, err := RenderExport(ExportTargetJSON, tokens)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		next, err := RenderExport(ExportTargetJSON, tokens)
		require.NoError(t, err)
		assert.Equal(t, first, next, "run %d drifted", i+2)
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

var utilitiesCSSRootRe = regexp.MustCompile(`:root \{`)

func mustRenderCSS(t *testing.T, doc string) []byte {
	t.Helper()
	out, err := RenderExport(ExportTargetCSS, mustTokens(t, doc))
	require.NoError(t, err)
	require.Regexp(t, utilitiesCSSRootRe, string(out))
	return out
}
