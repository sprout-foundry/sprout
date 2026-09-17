package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportFixtureDocument is a self-contained DTCG tier exercising the value
// shapes token export must handle: colors, an alias to a color, dimensions
// (string and numeric), fontFamily, fontWeight, duration, and a structured
// cubicBezier array.
const exportFixtureDocument = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" },
      "text":    { "$type": "color", "$value": "{color.brand.primary}" }
    },
    "neutral": {
      "900": { "$type": "color", "$value": "#0a0a0a" }
    }
  },
  "dimension": {
    "space": {
      "small": { "$type": "dimension", "$value": "4px" },
      "wide":  { "$type": "dimension", "$value": 160 }
    }
  },
  "fontFamily": { "body": { "$type": "fontFamily", "$value": "Inter, system-ui" } },
  "fontWeight": { "bold": { "$type": "fontWeight", "$value": 700 } },
  "duration":   { "slow": { "$type": "duration", "$value": "300ms" } },
  "cubicBezier": { "ease": { "$type": "cubicBezier", "$value": [0.3, 0, 0, 1] } }
}`

// exportWriteFile writes a token tier under root/design/tokens/.
func exportWriteFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, DirName, TokenSubdir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func mustTokens(t *testing.T, doc string) *TokenExport {
	t.Helper()
	leaves, err := collectExportLeaves([]byte(doc))
	require.NoError(t, err)
	require.NotEmpty(t, leaves)
	return &TokenExport{Leaves: leaves}
}

// -----------------------------------------------------------------------------
// ResolveExportTokens / projection
// -----------------------------------------------------------------------------

func TestResolveExportTokens_ProjectsSortedLeaves(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", exportFixtureDocument)

	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)

	var names []string
	byName := map[string]ExportedToken{}
	for _, l := range tokens.Leaves {
		names = append(names, l.Name)
		byName[l.Name] = l
	}

	// Sorted by dotted name — the determinism guarantee.
	assert.Equal(t, []string{
		"color.brand.primary",
		"color.brand.text",
		"color.neutral.900",
		"cubicBezier.ease",
		"dimension.space.small",
		"dimension.space.wide",
		"duration.slow",
		"fontFamily.body",
		"fontWeight.bold",
	}, names)

	assert.Equal(t, "color", byName["color.brand.primary"].Type)
	assert.Equal(t, "#0055ff", byName["color.brand.primary"].Resolved)

	// Alias leaf: HasAlias + AliasPath carry the target; Resolved is the
	// resolved value.
	alias := byName["color.brand.text"]
	assert.True(t, alias.HasAlias)
	assert.Equal(t, "color.brand.primary", alias.AliasPath)
	assert.Equal(t, "#0055ff", alias.Resolved)
	assert.True(t, alias.ResolvedOK)

	// Numeric dimension stays numeric; structured cubicBezier is retained.
	assert.Equal(t, float64(160), byName["dimension.space.wide"].Resolved)
	assert.Equal(t, float64(700), byName["fontWeight.bold"].Resolved)
	assert.Equal(t, []any{0.3, float64(0), float64(0), float64(1)}, byName["cubicBezier.ease"].Resolved)
}

func TestResolveExportTokens_AliasChain(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {
    "base":  { "$type": "color", "$value": "#101010" },
    "mid":   { "$type": "color", "$value": "{color.base}" },
    "final": { "$type": "color", "$value": "{color.mid}" }
  }
}`)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	byName := map[string]ExportedToken{}
	for _, l := range tokens.Leaves {
		byName[l.Name] = l
	}
	assert.Equal(t, "#101010", byName["color.final"].Resolved,
		"a two-hop alias chain resolves to the base value")
	assert.True(t, byName["color.final"].HasAlias)
	assert.Equal(t, "color.mid", byName["color.final"].AliasPath,
		"the first alias hop is recorded for var() references")
}

func TestResolveExportTokens_DanglingAliasIsNotResolved(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": { "text": { "$type": "color", "$value": "{color.brand.missing}" } }
}`)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	leaf := tokens.Leaves[0]
	assert.True(t, leaf.HasAlias)
	assert.False(t, leaf.ResolvedOK, "a dangling alias has no resolved value")
}

func TestResolveExportTokens_NoTokensIsSentinel(t *testing.T) {
	t.Run("no tokens directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
		_, err := ResolveExportTokens(root)
		assert.ErrorIs(t, err, ErrNoTokensForExport)
	})
	t.Run("empty tokens directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, TokenSubdir), 0o755))
		_, err := ResolveExportTokens(root)
		assert.ErrorIs(t, err, ErrNoTokensForExport)
	})
	t.Run("only non-token files", func(t *testing.T) {
		root := t.TempDir()
		exportWriteFile(t, root, "notes.md", "# not tokens\n")
		_, err := ResolveExportTokens(root)
		assert.ErrorIs(t, err, ErrNoTokensForExport)
	})
}

func TestResolveExportTokens_MalformedFileIsError(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "broken.tokens.json", "{\n  \"color\":")
	_, err := ResolveExportTokens(root)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoTokensForExport)
}

func TestResolveExportTokens_MultiFileSortedAndDeduped(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "b.tokens.json", `{"dimension": {"z": {"$type": "dimension", "$value": "1px"}}}`)
	exportWriteFile(t, root, "a.tokens.json", `{"color": {"a": {"$type": "color", "$value": "#000"}}}`)

	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	require.Len(t, tokens.Leaves, 2)
	assert.Equal(t, "color.a", tokens.Leaves[0].Name)
	assert.Equal(t, "dimension.z", tokens.Leaves[1].Name)
}

// -----------------------------------------------------------------------------
// Determinism — byte-identical output is the hard requirement
// -----------------------------------------------------------------------------

func TestRenderExport_DeterministicAcrossRuns(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	for _, target := range ExportTargets {
		t.Run(target, func(t *testing.T) {
			first, err := RenderExport(target, tokens)
			require.NoError(t, err)
			for i := 0; i < 25; i++ {
				next, err := RenderExport(target, tokens)
				require.NoError(t, err)
				assert.Equal(t, first, next, "run %d must be byte-identical", i+2)
			}
		})
	}
}

// TestResolveExportTokens_DeterministicAcrossMapOrderings re-resolves the same
// multi-file tier set and asserts the rendered bytes never move — the property
// that a Go map iteration order cannot leak into the output.
func TestResolveExportTokens_DeterministicAcrossMapOrderings(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", exportFixtureDocument)
	exportWriteFile(t, root, "spacing.tokens.json", `{
  "spacing": {"base": {"$type": "dimension", "$value": "8px"}},
  "zebra": {"striped": {"$type": "color", "$value": "#eee"}}
}`)

	var reference map[string][]byte
	for run := 0; run < 20; run++ {
		tokens, err := ResolveExportTokens(root)
		require.NoError(t, err)
		rendered := map[string][]byte{}
		for _, target := range ExportTargets {
			b, err := RenderExport(target, tokens)
			require.NoError(t, err)
			rendered[target] = b
		}
		if run == 0 {
			reference = rendered
			continue
		}
		for _, target := range ExportTargets {
			assert.Equal(t, reference[target], rendered[target],
				"%s output drifted on run %d", target, run+1)
		}
	}
}

func TestTokenExportArtifactHash_Stable(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.Len(t, artifacts, len(ExportTargets))

	again, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	for i := range artifacts {
		assert.Equal(t, artifacts[i].Hash, again[i].Hash)
		assert.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, artifacts[i].Hash)
	}
	// Distinct content must not collide trivially.
	assert.NotEqual(t, artifacts[0].Hash, artifacts[1].Hash)
}

// -----------------------------------------------------------------------------
// CSS variables
// -----------------------------------------------------------------------------

func TestRenderCSSVariables(t *testing.T) {
	b, err := RenderExport(ExportTargetCSS, mustTokens(t, exportFixtureDocument))
	require.NoError(t, err)
	out := string(b)

	assert.Contains(t, out, ":root {")
	assert.Contains(t, out, "  --color-brand-primary: #0055ff;")
	// Alias emits a var() reference to the target's variable.
	assert.Contains(t, out, "  --color-brand-text: var(--color-brand-primary);")
	assert.Contains(t, out, "  --dimension-space-small: 4px;")
	assert.Contains(t, out, "  --dimension-space-wide: 160;")
	assert.Contains(t, out, "  --fontfamily-body: Inter, system-ui;")
	assert.Contains(t, out, "  --fontweight-bold: 700;")
	assert.Contains(t, out, "  --duration-slow: 300ms;")
	assert.True(t, strings.HasSuffix(out, "}\n"), "CSS output ends with a single closing brace + newline")

	// The header comment is balanced and names the tool.
	assert.Contains(t, out, "Generated by sprout design_export_tokens (css)")
	assert.Equal(t, 1, countOccurrences(out, "/* Generated"), "one opening banner comment")
	assert.Equal(t, 1, countOccurrences(out, "*/"), "the banner comment is closed exactly once")
}

// -----------------------------------------------------------------------------
// TypeScript
// -----------------------------------------------------------------------------

func TestRenderTypeScript(t *testing.T) {
	b, err := RenderExport(ExportTargetTS, mustTokens(t, exportFixtureDocument))
	require.NoError(t, err)
	out := string(b)

	assert.Contains(t, out, "export const tokens = {")
	assert.Contains(t, out, `  "color.brand.primary": "#0055ff",`)
	// Alias resolves to its value in the typed map.
	assert.Contains(t, out, `  "color.brand.text": "#0055ff",`)
	assert.Contains(t, out, `  "dimension.space.wide": "160",`)
	assert.Contains(t, out, `  "fontWeight.bold": "700",`)
	assert.Contains(t, out, "} as const;")
	assert.Contains(t, out, "export type TokenName = keyof typeof tokens;")
	assert.Contains(t, out, "export const cssVar: Record<TokenName, string> = {")
	assert.Contains(t, out, `  "color.brand.primary": "--color-brand-primary",`)
}

// -----------------------------------------------------------------------------
// Tailwind @theme
// -----------------------------------------------------------------------------

func TestRenderTailwind(t *testing.T) {
	b, err := RenderExport(ExportTargetTailwind, mustTokens(t, exportFixtureDocument))
	require.NoError(t, err)
	out := string(b)

	assert.Contains(t, out, "@theme {")
	assert.Contains(t, out, "  --color-brand-primary: #0055ff;")
	assert.Contains(t, out, "  --color-brand-text: var(--color-brand-primary);")
	assert.True(t, strings.HasSuffix(out, "}\n"))
	assert.Contains(t, out, "Generated by sprout design_export_tokens (tailwind)")
}

// -----------------------------------------------------------------------------
// Swift / Kotlin
// -----------------------------------------------------------------------------

func TestRenderSwift(t *testing.T) {
	b, err := RenderExport(ExportTargetSwift, mustTokens(t, exportFixtureDocument))
	require.NoError(t, err)
	out := string(b)

	assert.Contains(t, out, "import Foundation")
	assert.Contains(t, out, "public enum DesignTokens {")
	assert.Contains(t, out, `  public static let colorBrandPrimary: String = "#0055ff"`)
	assert.Contains(t, out, `  public static let dimensionSpaceWide: String = "160"`)
	assert.Contains(t, out, `  public static let fontFamilyBody: String = "Inter, system-ui"`)
	assert.Contains(t, out, `  public static let cubicBezierEase: String = "[0.3,0,0,1]"`)
}

func TestRenderKotlin(t *testing.T) {
	b, err := RenderExport(ExportTargetKotlin, mustTokens(t, exportFixtureDocument))
	require.NoError(t, err)
	out := string(b)

	assert.Contains(t, out, "object DesignTokens {")
	assert.Contains(t, out, `  const val colorBrandPrimary: String = "#0055ff"`)
	assert.Contains(t, out, `  const val dimensionSpaceSmall: String = "4px"`)
	assert.Contains(t, out, `  const val fontWeightBold: String = "700"`)
}

func TestRenderKotlin_EscapesDollar(t *testing.T) {
	tokens := mustTokens(t, `{"odd": {"price": {"$type": "fontFamily", "$value": "$primary, sans-serif"}}}`)
	b, err := RenderExport(ExportTargetKotlin, tokens)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"\$primary, sans-serif"`,
		"a literal $ must be escaped so Kotlin does not read it as a string template")
}

// -----------------------------------------------------------------------------
// Target resolution
// -----------------------------------------------------------------------------

func TestResolveExportTargets(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "empty means all", raw: "", want: ExportTargets},
		{name: "all means all", raw: "all", want: ExportTargets},
		{name: "ALL is case-insensitive", raw: "ALL", want: ExportTargets},
		{name: "single target", raw: "css", want: []string{"css"}},
		{name: "comma list", raw: "css, ts", want: []string{"css", "ts"}},
		{name: "argument order is canonicalized", raw: "swift,css", want: []string{"css", "swift"}},
		{name: "duplicates collapse", raw: "css,css,ts", want: []string{"css", "ts"}},
		{name: "whitespace tolerated", raw: "  kotlin , tailwind  ", want: []string{"tailwind", "kotlin"}},
		{name: "all mixed in means all", raw: "css,all", want: ExportTargets},
		{name: "unknown target errors", raw: "sass", wantErr: true},
		{name: "empty after commas errors", raw: ",,", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveExportTargets(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var names []string
			for _, g := range got {
				names = append(names, g.Name)
				assert.NotEmpty(t, g.File, "every target carries its output filename")
			}
			assert.Equal(t, tt.want, names)
		})
	}
}

// -----------------------------------------------------------------------------
// Artifact rendering + writing
// -----------------------------------------------------------------------------

func TestRenderArtifacts_DefaultPathsUnderGenerated(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.Len(t, artifacts, len(ExportTargets))

	wantPaths := []string{
		"design/generated/tokens.css",
		"design/generated/tokens.ts",
		"design/generated/tailwind.theme.css",
		"design/generated/tokens.swift",
		"design/generated/tokens.kt",
	}
	for i, a := range artifacts {
		assert.Equal(t, wantPaths[i], a.RelPath)
		assert.Equal(t, ExportTargets[i], a.Target)
		assert.NotEmpty(t, a.Content)
	}
}

func TestWriteExportedArtifacts_WritesUnderRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
	tokens := mustTokens(t, exportFixtureDocument)
	artifacts, err := RenderArtifacts(tokens, exportTargetList([]string{ExportTargetCSS, ExportTargetTS}))
	require.NoError(t, err)
	require.NoError(t, WriteExportedArtifacts(root, artifacts))

	for _, a := range artifacts {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.RelPath)))
		require.NoError(t, err, "artifact %s must exist", a.RelPath)
		assert.Equal(t, a.Content, data)
	}
}

func TestWriteExportedArtifacts_RefusesOutsideDesign(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
	err := WriteExportedArtifacts(root, []ExportedArtifact{{
		Target:  "css",
		RelPath: "src/tokens.css",
		Content: []byte("x"),
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside design/")
	_, statErr := os.Stat(filepath.Join(root, "src", "tokens.css"))
	assert.True(t, os.IsNotExist(statErr), "a refused artifact must not be written")
}

// -----------------------------------------------------------------------------
// Identifier helpers
// -----------------------------------------------------------------------------

func TestCSSVarStem(t *testing.T) {
	tests := map[string]string{
		"color.brand.primary": "color-brand-primary",
		"fontFamily.body":     "fontfamily-body",
		"dimension.space.xl":  "dimension-space-xl",
		"a..b":                "a-b",
		".leading":            "leading",
		"UPPER.Case":          "upper-case",
	}
	for in, want := range tests {
		assert.Equal(t, want, cssVarStem(in), "cssVarStem(%q)", in)
	}
	assert.Equal(t, "token", cssVarStem("..."), "a path with no alphanumerics falls back")
}

func TestCamelIdent(t *testing.T) {
	tests := map[string]string{
		"color.brand.primary": "colorBrandPrimary",
		"fontFamily.body":     "fontFamilyBody",
		"fontWeight.bold":     "fontWeightBold",
		"color.neutral.900":   "colorNeutral900",
		"1st.thing":           "_1stThing",
	}
	for in, want := range tests {
		assert.Equal(t, want, camelIdent(in), "camelIdent(%q)", in)
	}
}

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

func countOccurrences(haystack, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
		}
	}
	return count
}
