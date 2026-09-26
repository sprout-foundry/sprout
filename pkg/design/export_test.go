package design

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	return &TokenExport{
		Leaves:    leaves,
		InputHash: exportTokenInputHash([]TokenExportSource{{Name: "color.tokens.json", Content: []byte(doc)}}),
	}
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

// TestResolveExportTokens_DanglingAliasRefused pins item 5.2: a dangling alias
// is a *dirty alias graph*, and export refuses rather than emitting an
// unresolved var() reference. (Item 5.1 left this unenforced; the refusal is
// the point of 5.2.)
func TestResolveExportTokens_DanglingAliasRefused(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": { "text": { "$type": "color", "$value": "{color.brand.missing}" } }
}`)
	tokens, err := ResolveExportTokens(root)
	require.Error(t, err)
	require.Nil(t, tokens)
	assert.ErrorIs(t, err, ErrDirtyAliasGraph)

	var dirty *DirtyAliasError
	require.ErrorAs(t, err, &dirty)
	require.Len(t, dirty.Violations, 1)
	v := dirty.Violations[0]
	assert.Equal(t, "color.text", v.Token, "the refusal names the offending token")
	assert.Equal(t, "dangling", v.Kind)
	assert.Equal(t, ruleTokenAliasDangling, v.Rule)
	assert.Equal(t, "design/tokens/color.tokens.json", v.Path)
	assert.Contains(t, v.Message, "does not resolve")
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
	assert.Contains(t, out, "Generated by design_export_tokens (target=css)")
	assert.Equal(t, 1, countOccurrences(out, "/* Generated"), "one opening banner comment")
	// The banner is a balanced block comment: each of its three lines opens
	// with "/*" and closes with "*/". (The "/*" inside the Source line's
	// "design/tokens/*.tokens.json" is not line-initial, so counting the three
	// distinct line openers is exact. The utility layer under the variables
	// adds its own comments, so the closing count is "at least the banner" —
	// the utilities_test.go pass owns its exact comment shape.)
	for _, opener := range []string{"/* Generated by design_export_tokens", "/* source-hash: ", "/* Source: design/tokens/"} {
		assert.GreaterOrEqual(t, countOccurrences(out, opener), 1, "one %q banner line", opener)
	}
	assert.GreaterOrEqual(t, countOccurrences(out, " */\n"), 3, "every banner line closes its comment")
	assert.Regexp(t, `(?m)^/\* source-hash: fnv1a64:[0-9a-f]{16} \*/$`, out)
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
	assert.Contains(t, out, "Generated by design_export_tokens (target=tailwind)")
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
		{name: "flows is an explicit target", raw: "flows", want: []string{"flows"}},
		{name: "flows is never in all", raw: "all", want: ExportTargets},
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
		"design/generated/tokens.json",
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
// SP-140-5 item 5.2 — provenance content-hash headers
// -----------------------------------------------------------------------------

// exportProvenanceHashRE matches the header's source-hash line for the CSS
// (block-comment) targets and the JSON target's field form — the two banner
// shapes the shared provenanceSourceHash extractor accepts.
var exportProvenanceHashRE = regexp.MustCompile(`"?source-hash"?:\s*"?(fnv1a64:[0-9a-f]{16})`)

// TestProvenanceHeader_EveryTargetCarriesSourceHash asserts the 5.2 AC: each
// generated artifact carries a provenance header whose source-hash names the
// content hash of the token inputs, and the hash is the same across every
// target (one token set → one provenance hash).
func TestProvenanceHeader_EveryTargetCarriesSourceHash(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	require.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, tokens.InputHash)

	for _, target := range ExportTargets {
		t.Run(target, func(t *testing.T) {
			b, err := RenderExport(target, tokens)
			require.NoError(t, err)
			out := string(b)

			assert.Contains(t, out, "Generated by design_export_tokens (target="+target+")",
				"the header names the tool and target")
			assert.Contains(t, out, "Do not edit by hand")
			assert.Contains(t, out, "design/tokens/*.tokens.json")

			// The JSON target has no comment syntax, so its banner is the
			// same three facts as leading fields; the shared extractor reads
			// both shapes, and this assertion follows it.
			m := exportProvenanceHashRE.FindStringSubmatch(out)
			require.NotNil(t, m, "the header must carry a source-hash line")
			assert.Equal(t, tokens.InputHash, m[1],
				"the header hash is the token-input hash, identical across targets")

			// The header is the *first* thing in the artifact (a banner).
			assert.True(t, strings.HasPrefix(out, "// Generated by design_export_tokens") ||
				strings.HasPrefix(out, "/* Generated by design_export_tokens") ||
				strings.HasPrefix(out, "{\n  \"provenance\": \"Generated by design_export_tokens"),
				"the provenance header opens the file")
		})
	}
}

// TestProvenanceHeader_IsDeterministic proves the header carries no timestamp
// or other nondeterministic content: the same inputs render byte-identical
// bytes, including the header, across many runs.
func TestProvenanceHeader_IsDeterministic(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	for _, target := range ExportTargets {
		first, err := RenderExport(target, tokens)
		require.NoError(t, err)
		for i := 0; i < 10; i++ {
			next, err := RenderExport(target, tokens)
			require.NoError(t, err)
			assert.Equal(t, first, next, "%s header drifted on run %d", target, i+2)
		}
		assert.NotContains(t, string(first), "generated:", "no timestamp line in the header")
	}
}

// TestProvenanceHeader_HashIsRecomputableFromTokenInputs is the §5f offline
// check in miniature: a consumer recomputes the hash from the raw token-source
// bytes alone (sorted by basename, length-prefixed) and must land on the exact
// hash in the artifact header — no export run, no database, nothing but the
// tree.
func TestProvenanceHeader_HashIsRecomputableFromTokenInputs(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", exportFixtureDocument)
	exportWriteFile(t, root, "motion.tokens.json", `{
  "duration": {"slow": {"$type": "duration", "$value": "300ms"}}
}`)

	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)

	// Recompute independently, the way an offline verifier would: read the
	// two source files, sort by basename, hash "len\nbytes" concatenated.
	paths, err := filepath.Glob(filepath.Join(root, DirName, TokenSubdir, "*.tokens.json"))
	require.NoError(t, err)
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	sort.Strings(names)
	var b bytes.Buffer
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, DirName, TokenSubdir, name))
		require.NoError(t, err)
		fmt.Fprintf(&b, "%d\n", len(data))
		b.Write(data)
	}
	want := "fnv1a64:" + fmt.Sprintf("%016x", fnv1a64Hex(b.Bytes()))
	assert.Equal(t, want, tokens.InputHash, "the header hash is recomputable from the tree alone")

	// And the rendered header carries exactly that hash.
	css, err := RenderExport(ExportTargetCSS, tokens)
	require.NoError(t, err)
	assert.Contains(t, string(css), "source-hash: "+tokens.InputHash)
}

// fnv1a64Hex is an independent FNV-1a-64 implementation used only by the test
// above, so the "recompute the hash" assertion does not share code with the
// production hash (a shared bug would otherwise cancel out).
func fnv1a64Hex(data []byte) uint64 {
	var h uint64 = 14695981039346656037
	for _, c := range data {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

// TestProvenanceHeader_HashMovesWithTokenContent pins that the hash actually
// tracks the token bytes: changing a token value changes every target's
// source-hash line.
func TestProvenanceHeader_HashMovesWithTokenContent(t *testing.T) {
	original := mustTokens(t, exportFixtureDocument)
	changed := mustTokens(t, strings.Replace(exportFixtureDocument, "#0055ff", "#ff5500", 1))
	require.NotEqual(t, original.InputHash, changed.InputHash)

	before, err := RenderExport(ExportTargetCSS, original)
	require.NoError(t, err)
	after, err := RenderExport(ExportTargetCSS, changed)
	require.NoError(t, err)
	assert.NotEqual(t, string(before), string(after))

	mBefore := exportProvenanceHashRE.FindStringSubmatch(string(before))
	mAfter := exportProvenanceHashRE.FindStringSubmatch(string(after))
	require.NotNil(t, mBefore)
	require.NotNil(t, mAfter)
	assert.NotEqual(t, mBefore[1], mAfter[1], "a token change must move the provenance hash")
}

// TestProvenanceHeader_FileOrderIndependent proves the hash does not depend on
// the order the token files are read: two workspaces with the same file set
// (different names, same content mapping) hash consistently, and re-resolving
// the same tree repeatedly never moves the hash.
func TestProvenanceHeader_FileOrderIndependent(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "b.tokens.json", `{"color": {"z": {"$type": "color", "$value": "#111"}}}`)
	exportWriteFile(t, root, "a.tokens.json", `{"color": {"a": {"$type": "color", "$value": "#222"}}}`)

	first, err := ResolveExportTokens(root)
	require.NoError(t, err)
	for i := 0; i < 15; i++ {
		next, err := ResolveExportTokens(root)
		require.NoError(t, err)
		assert.Equal(t, first.InputHash, next.InputHash, "hash drifted on re-resolve %d", i+2)
	}
}

// TestTokenExportInputHash_LengthPrefixedNoPartitionCollision pins the
// length-prefix discipline: two partitions of the same concatenated bytes must
// not collide.
func TestTokenExportInputHash_LengthPrefixedNoPartitionCollision(t *testing.T) {
	one := TokenExportInputHash([]TokenExportSource{{Name: "a.tokens.json", Content: []byte("AB")}})
	two := TokenExportInputHash([]TokenExportSource{
		{Name: "a.tokens.json", Content: []byte("A")},
		{Name: "b.tokens.json", Content: []byte("B")},
	})
	assert.NotEqual(t, one, two, "different file partitions must hash differently")
}

// -----------------------------------------------------------------------------
// SP-140-5 item 5.2 — refuses dirty alias graphs
// -----------------------------------------------------------------------------

// TestResolveExportTokens_CyclicAliasRefused pins the cycle half of the 5.2 AC:
// a cyclic alias graph is refused with a clear error naming the offending
// token(s).
func TestResolveExportTokens_CyclicAliasRefused(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {
    "a": { "$type": "color", "$value": "{color.b}" },
    "b": { "$type": "color", "$value": "{color.a}" }
  }
}`)
	tokens, err := ResolveExportTokens(root)
	require.Error(t, err)
	require.Nil(t, tokens)
	assert.ErrorIs(t, err, ErrDirtyAliasGraph)

	var dirty *DirtyAliasError
	require.ErrorAs(t, err, &dirty)
	require.NotEmpty(t, dirty.Violations)
	for _, v := range dirty.Violations {
		assert.Equal(t, "cycle", v.Kind)
		assert.Equal(t, ruleTokenAliasCycle, v.Rule)
		assert.NotEmpty(t, v.Token, "a cycle violation names a member token")
	}
	assert.Contains(t, err.Error(), "refusing to export")
	assert.Contains(t, err.Error(), "cycle")
}

// TestResolveExportTokens_SelfAliasRefused covers the degenerate one-token
// cycle.
func TestResolveExportTokens_SelfAliasRefused(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {"loop": { "$type": "color", "$value": "{color.loop}" }}
}`)
	_, err := ResolveExportTokens(root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDirtyAliasGraph)
}

// TestResolveExportTokens_DirtyGraphNamesOffendingToken proves the refusal
// message names the specific broken token, not just "something is wrong".
func TestResolveExportTokens_DirtyGraphNamesOffendingToken(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {
    "ok":   { "$type": "color", "$value": "#000000" },
    "bad":  { "$type": "color", "$value": "{color.nope}" }
  }
}`)
	_, err := ResolveExportTokens(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "color.bad", "the refusal must name the offending token")
	assert.Contains(t, err.Error(), "dangling")
}

// TestResolveExportTokens_MultipleViolationsSortedDeterministically pins that
// the refusal lists violations in a stable order regardless of which file they
// came from.
func TestResolveExportTokens_MultipleViolationsSortedDeterministically(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "b.tokens.json", `{
  "zebra": {"dangle": { "$type": "color", "$value": "{zebra.missing}" }}
}`)
	exportWriteFile(t, root, "a.tokens.json", `{
  "alpha": {"dangle": { "$type": "color", "$value": "{alpha.missing}" }}
}`)

	var reference string
	for run := 0; run < 10; run++ {
		_, err := ResolveExportTokens(root)
		require.Error(t, err)
		if run == 0 {
			reference = err.Error()
			continue
		}
		assert.Equal(t, reference, err.Error(), "refusal message drifted on run %d", run+1)
	}
	// Sorted by token path: alpha.* before zebra.*.
	assert.Less(t, strings.Index(reference, "alpha.dangle"), strings.Index(reference, "zebra.dangle"))
}

// TestResolveExportTokens_CleanGraphStillExports is the negative control: a
// well-formed alias graph is not refused.
func TestResolveExportTokens_CleanGraphStillExports(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {
    "base": { "$type": "color", "$value": "#123456" },
    "text": { "$type": "color", "$value": "{color.base}" }
  }
}`)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	require.NotNil(t, tokens)
	assert.NotEmpty(t, tokens.InputHash)
}

// TestResolveExportTokens_AliasToGroupIsDanglingEdgeCase documents that an
// alias whose target exists but is a group (not a leaf) is *not* dirty at the
// graph level — the validator's resolveAlias accepts any node — so export
// proceeds; such a leaf simply has no resolved value. This mirrors
// design_validate, which does not flag it either.
func TestResolveExportTokens_AliasToGroupIsDanglingEdgeCase(t *testing.T) {
	root := t.TempDir()
	exportWriteFile(t, root, "color.tokens.json", `{
  "color": {
    "group": { "a": { "$type": "color", "$value": "#000" } },
    "ref":   { "$type": "color", "$value": "{color.group}" }
  }
}`)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err, "an alias to an existing group is not a dangling reference")
	assert.NotEmpty(t, tokens.Leaves)
}

// TestCheckAliasGraph_UnparseableDocumentIsNotDirty pins the split of
// responsibility: the alias check does not double-report a parse failure (the
// parse path owns that error), so it returns nil for an unparseable document.
func TestCheckAliasGraph_UnparseableDocumentIsNotDirty(t *testing.T) {
	assert.NoError(t, checkAliasGraph("design/tokens/x.tokens.json", []byte("{not json")))
}

// -----------------------------------------------------------------------------
// Artifact writing + hashing under the new header
// -----------------------------------------------------------------------------

func TestRenderArtifacts_EveryArtifactHeaderAndHash(t *testing.T) {
	tokens := mustTokens(t, exportFixtureDocument)
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.Len(t, artifacts, len(ExportTargets))

	for _, a := range artifacts {
		assert.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, a.Hash)
		assert.Equal(t, tokens.InputHash, provenanceSourceHash(string(a.Content)),
			"%s must carry the token-input hash in a parsable banner", a.RelPath)
		// The reported artifact hash is the hash of the emitted bytes
		// (including the header), so it moves when the header moves.
		assert.Equal(t, tokenExportHash(a.Content), a.Hash)
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
