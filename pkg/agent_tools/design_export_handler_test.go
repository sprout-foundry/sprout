package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_export_tokens handler tests (SP-140-5 §5a, item 5.1)
//
// The DTCG -> text logic is unit tested in pkg/design/export_test.go. These
// tests cover the ToolHandler seam: argument resolution, Gate-1 prechecks,
// write confinement to design/generated/, deterministic byte-identical output
// through the real filesystem, and the structured result.
// ---------------------------------------------------------------------------

// dxTokenJSON is a colour/typography/spacing tier exercising colors,
// dimensions (string + numeric), fontFamily, fontWeight, and an alias.
const dxTokenJSON = `{
  "color": {
    "brand": {
      "primary":   { "$type": "color", "$value": "#0055ff" },
      "secondary": { "$type": "color", "$value": "#00ff88" },
      "text":      { "$type": "color", "$value": "{color.brand.primary}" }
    }
  },
  "dimension": {
    "space": {
      "small":  { "$type": "dimension", "$value": "4px" },
      "medium": { "$type": "dimension", "$value": "8px" },
      "wide":   { "$type": "dimension", "$value": 160 }
    }
  },
  "fontFamily": { "body": { "$type": "fontFamily", "$value": "Inter, system-ui" } },
  "fontWeight": { "bold": { "$type": "fontWeight", "$value": 700 } }
}`

// dxWrite wraps daWrite (already defined in design_assets_handler_test.go) with
// the fixture token file for readability.
func dxWriteTokens(t *testing.T, root, content string) {
	t.Helper()
	daWrite(t, root, "design/tokens/color.tokens.json", content)
}

// dxGeneratedFiles lists the files written under root/design/generated.
func dxGeneratedFiles(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, design.DirName, design.GeneratedSubdir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// dxReadGenerated reads a generated artifact, failing the test if absent.
func dxReadGenerated(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, design.DirName, design.GeneratedSubdir, name))
	require.NoError(t, err, "generated artifact %s must exist", name)
	return data
}

// ---------------------------------------------------------------------------
// Definition / Validate
// ---------------------------------------------------------------------------

func TestDesignExportHandler_NameAndDefinition(t *testing.T) {
	t.Parallel()
	h := &designExportHandler{}

	require.Equal(t, "design_export_tokens", h.Name())

	def := h.Definition()
	require.Equal(t, "design_export_tokens", def.Name)
	require.NotEmpty(t, def.Description)
	for _, want := range []string{"css", "ts", "tailwind", "swift", "kotlin", "design/generated/"} {
		require.Contains(t, def.Description, want, "the description must name %q", want)
	}
	require.Empty(t, def.Required, "targets and out_dir are both optional")

	params := map[string]bool{}
	for _, p := range def.Parameters {
		params[p.Name] = true
	}
	assert.True(t, params["targets"])
	assert.True(t, params["out_dir"])
}

func TestDesignExportHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designExportHandler{}

	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"targets": "css", "out_dir": "design/generated"}))
	assert.Error(t, h.Validate(map[string]any{"targets": 5}))
	assert.Error(t, h.Validate(map[string]any{"out_dir": 5}))
}

// ---------------------------------------------------------------------------
// Default full export
// ---------------------------------------------------------------------------

func TestDesignExportHandler_DefaultExportAllTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(designExportOutput)
	require.True(t, ok)
	assert.True(t, out.Exists)
	assert.Equal(t, "design/tokens", out.TokensPath)
	assert.Equal(t, "design/generated", out.OutDir)
	assert.Equal(t, 6, out.TargetCount)
	assert.Equal(t, 8, out.TokenCount, "color.brand.{primary,secondary,text} + 3 dimensions + fontFamily + fontWeight")

	// All five files landed under design/generated/.
	assert.Equal(t, []string{
		"tailwind.theme.css", "tokens.css", "tokens.json", "tokens.kt", "tokens.swift", "tokens.ts",
	}, dxGeneratedFiles(t, root))

	// Structured file rows carry path, byte count, and a content hash.
	paths := make([]string, 0, len(out.Files))
	for _, f := range out.Files {
		paths = append(paths, f.Path)
		assert.Greater(t, f.Bytes, 0)
		assert.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, f.ContentHash)
	}
	assert.Equal(t, []string{
		"design/generated/tokens.css",
		"design/generated/tokens.ts",
		"design/generated/tokens.json",
		"design/generated/tailwind.theme.css",
		"design/generated/tokens.swift",
		"design/generated/tokens.kt",
	}, paths, "files are reported in canonical target order")

	// The summary names the outputs.
	assert.Contains(t, res.Output, "design/generated/")
	assert.Contains(t, res.Output, "tokens.css")
}

func TestDesignExportHandler_TOKEN_SUBSTITUTION_DOES_NOT_LEAK(t *testing.T) {
	// Guard: the handler is a plain export, not a template engine. A token
	// value containing a shell/variable-looking string survives verbatim.
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, `{"odd": {"value": {"$type": "fontFamily", "$value": "$HOME, sans-serif"}}}`)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Contains(t, string(dxReadGenerated(t, root, "tokens.css")), "$HOME, sans-serif")
}

// ---------------------------------------------------------------------------
// Byte-identical determinism through the filesystem
// ---------------------------------------------------------------------------

func TestDesignExportHandler_DeterministicByteIdentical(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	// A second tier in a separate file, so multi-file ordering is in play.
	daWrite(t, root, "design/tokens/motion.tokens.json", `{
  "duration": {"slow": {"$type": "duration", "$value": "300ms"}},
  "cubicBezier": {"ease": {"$type": "cubicBezier", "$value": [0.3, 0, 0, 1]}}
}`)
	h := &designExportHandler{}

	// First run establishes the reference bytes.
	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	names := dxGeneratedFiles(t, root)
	require.Len(t, names, 6)
	reference := map[string][]byte{}
	for _, name := range names {
		reference[name] = dxReadGenerated(t, root, name)
	}

	// Re-run many times; every artifact must be byte-identical.
	for run := 0; run < 15; run++ {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
		require.NoError(t, err)
		require.False(t, res.IsError)
		for _, name := range names {
			assert.Equal(t, reference[name], dxReadGenerated(t, root, name),
				"%s drifted on run %d", name, run+2)
		}
	}
}

// ---------------------------------------------------------------------------
// Target selection
// ---------------------------------------------------------------------------

func TestDesignExportHandler_TargetSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css,ts"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	assert.Equal(t, 2, out.TargetCount)
	assert.Equal(t, []string{"tokens.css", "tokens.ts"}, dxGeneratedFiles(t, root),
		"only the requested targets are written")
}

func TestDesignExportHandler_UnknownTargetIsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "sass"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "unknown export target")
	assert.Empty(t, dxGeneratedFiles(t, root), "a usage error writes nothing")
}

func TestDesignExportHandler_TargetOrderCanonicalized(t *testing.T) {
	t.Parallel()
	rootA := t.TempDir()
	rootB := t.TempDir()
	dxWriteTokens(t, rootA, dxTokenJSON)
	dxWriteTokens(t, rootB, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(rootA), newTestEnv(t, rootA), map[string]any{"targets": "swift,css"})
	require.NoError(t, err)
	_, err = h.Execute(newTestCtx(rootB), newTestEnv(t, rootB), map[string]any{"targets": "css,swift"})
	require.NoError(t, err)

	for _, name := range []string{"tokens.css", "tokens.swift"} {
		assert.Equal(t, dxReadGenerated(t, rootA, name), dxReadGenerated(t, rootB, name),
			"%s must be identical regardless of argument order", name)
	}
}

// ---------------------------------------------------------------------------
// Target file contents
// ---------------------------------------------------------------------------

func TestDesignExportHandler_CSSVariables(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.NoError(t, err)

	css := string(dxReadGenerated(t, root, "tokens.css"))
	assert.Contains(t, css, ":root {")
	assert.Contains(t, css, "--color-brand-primary: #0055ff;")
	assert.Contains(t, css, "--color-brand-secondary: #00ff88;")
	// The alias emits a var() reference to the target's CSS variable.
	assert.Contains(t, css, "--color-brand-text: var(--color-brand-primary);")
	assert.Contains(t, css, "--dimension-space-small: 4px;")
	assert.Contains(t, css, "--dimension-space-wide: 160;")
	assert.Contains(t, css, "--fontfamily-body: Inter, system-ui;")
	assert.Contains(t, css, "--fontweight-bold: 700;")
	assert.True(t, strings.HasSuffix(css, "}\n"))
}

func TestDesignExportHandler_TypeScript(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "ts"})
	require.NoError(t, err)

	ts := string(dxReadGenerated(t, root, "tokens.ts"))
	assert.Contains(t, ts, "export const tokens = {")
	assert.Contains(t, ts, `  "color.brand.primary": "#0055ff",`)
	assert.Contains(t, ts, `  "color.brand.text": "#0055ff",`)
	assert.Contains(t, ts, "} as const;")
	assert.Contains(t, ts, "export type TokenName = keyof typeof tokens;")
	assert.Contains(t, ts, "export const cssVar: Record<TokenName, string> = {")
	assert.Contains(t, ts, `  "color.brand.primary": "--color-brand-primary",`)
}

func TestDesignExportHandler_TailwindTheme(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "tailwind"})
	require.NoError(t, err)

	tw := string(dxReadGenerated(t, root, "tailwind.theme.css"))
	assert.Contains(t, tw, "@theme {")
	assert.Contains(t, tw, "--color-brand-primary: #0055ff;")
	assert.Contains(t, tw, "--color-brand-text: var(--color-brand-primary);")
	assert.True(t, strings.HasSuffix(tw, "}\n"))
}

func TestDesignExportHandler_SwiftAndKotlin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "swift,kotlin"})
	require.NoError(t, err)

	swift := string(dxReadGenerated(t, root, "tokens.swift"))
	assert.Contains(t, swift, "public enum DesignTokens {")
	assert.Contains(t, swift, `  public static let colorBrandPrimary: String = "#0055ff"`)

	kotlin := string(dxReadGenerated(t, root, "tokens.kt"))
	assert.Contains(t, kotlin, "object DesignTokens {")
	assert.Contains(t, kotlin, `  const val colorBrandPrimary: String = "#0055ff"`)
}

// ---------------------------------------------------------------------------
// out_dir handling — writes confined to design/
// ---------------------------------------------------------------------------

func TestDesignExportHandler_OutDirOverride(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"targets": "css", "out_dir": "design/generated/theme"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	assert.Equal(t, "design/generated/theme", out.OutDir)
	assert.Equal(t, "design/generated/theme/tokens.css", out.Files[0].Path)

	data, err := os.ReadFile(filepath.Join(root, "design", "generated", "theme", "tokens.css"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "--color-brand-primary: #0055ff;")
}

func TestDesignExportHandler_OutDirRefusedOutsideDesign(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	for _, outDir := range []string{
		"src/generated",                 // outside design/
		"design",                        // design/ root itself, not a subdirectory
		"design/tokens",                 // the token source tier
		"design/wireframes",             // another source tier
		"../escape",                     // parent traversal
		"design/generated/../../escape", // traversal that resolves outside
	} {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
			map[string]any{"targets": "css", "out_dir": outDir})
		require.Error(t, err, "out_dir %q must be refused", outDir)
		require.True(t, res.IsError, "out_dir %q must be a tool failure", outDir)
		assert.Empty(t, dxGeneratedFiles(t, root), "out_dir %q must write nothing", outDir)
	}
}

// TestDesignExportHandler_WriteConfinedToGenerated proves the export never
// modifies the token sources: the token file bytes are unchanged after a full
// run and no file exists outside design/generated/.
func TestDesignExportHandler_WriteConfinedToGenerated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	daWrite(t, root, "design/README.md", "# Design Workspace\n")
	tokenPath := filepath.Join(root, "design", "tokens", "color.tokens.json")
	before, err := os.ReadFile(tokenPath)
	require.NoError(t, err)

	h := &designExportHandler{}
	_, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	after, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the token source must be untouched by export")

	// Walk the whole workspace and assert every new file lives under
	// design/generated/.
	require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		require.NoError(t, relErr)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "design/generated/") {
			return nil
		}
		assert.Contains(t, []string{"design/tokens/color.tokens.json", "design/README.md"}, rel,
			"export must not create or modify files outside design/generated/: found %s", rel)
		return nil
	}))
}

// ---------------------------------------------------------------------------
// Gate-1
// ---------------------------------------------------------------------------

func TestDesignExportHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	assert.Contains(t, res.Output, "design_export_tokens blocked")
	assert.Empty(t, dxGeneratedFiles(t, root), "a denied run writes nothing")
}

// TestDesignExportHandler_Gate1DenyOnOutputPath proves that a deny scoped to
// just the output file (rather than design/ wholesale) still blocks the write:
// the per-file precheck runs before each write.
func TestDesignExportHandler_Gate1DenyOnOutputPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	env := newTestEnv(t, root)
	// Deny only paths under design/generated; allow everything else so the
	// read of design/tokens succeeds and the failure is pinned to the write.
	env.FileAccessClassifier = denyGeneratedClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"targets": "css"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_export_tokens blocked")
	assert.Empty(t, dxGeneratedFiles(t, root))
}

// denyGeneratedClassifier denies any path under design/generated and allows
// everything else, isolating the write-side precheck.
type denyGeneratedClassifier struct{}

func (denyGeneratedClassifier) ClassifyFileAccess(_ context.Context, filePath, resolvedPath, _ string) string {
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	if strings.Contains(filepath.ToSlash(target), "/design/generated") {
		return "deny"
	}
	return "allow"
}

func (denyGeneratedClassifier) IsFolderSessionAllowed(_ string) bool { return false }

// ---------------------------------------------------------------------------
// Missing tree / no tokens
// ---------------------------------------------------------------------------

func TestDesignExportHandler_NoDesignTreeGuidance(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err, "a missing design/ tree is guidance, not a tool error")
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	assert.False(t, out.Exists)
	assert.NotEmpty(t, out.Guidance)
	assert.Contains(t, res.Output, "no design/ directory")
	assert.Empty(t, dxGeneratedFiles(t, root))
}

func TestDesignExportHandler_NoTokensIsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWrite(t, root, "design/README.md", "# Design Workspace\n")
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "no tokens found")
	assert.Empty(t, dxGeneratedFiles(t, root))
}

func TestDesignExportHandler_MalformedTokensIsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, "{\n  \"color\":")
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Empty(t, dxGeneratedFiles(t, root), "a parse failure writes nothing")
}

// TestDesignExportHandler_SymlinkedGeneratedDirRefused covers the physical
// confinement check: a design/generated that is a symlink pointing outside the
// design tree must be refused even though the logical path passes the prefix
// test.
func TestDesignExportHandler_SymlinkedGeneratedDirRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)

	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "design", design.GeneratedSubdir)))

	h := &designExportHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "outside design/")

	entries, readErr := os.ReadDir(outside)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "nothing may be written through the escaping symlink")
}

// ---------------------------------------------------------------------------
// Item 5.2 — provenance content-hash headers
// ---------------------------------------------------------------------------

// dxProvenanceHashRE matches the header's source-hash line (both comment
// styles: "source-hash: <hash>" appears verbatim either way).
var dxProvenanceHashRE = regexp.MustCompile(`"?source-hash"?:\s*"?(fnv1a64:[0-9a-f]{16})`)

// TestDesignExportHandler_ProvenanceHeaderInEveryArtifact is the 5.2 AC at the
// tool boundary: every generated file carries a provenance content-hash header,
// the hash is the same across all five artifacts, and the structured result
// echoes it (run-level SourceHash + per-file SourceHash).
func TestDesignExportHandler_ProvenanceHeaderInEveryArtifact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	require.NotEmpty(t, out.SourceHash)
	require.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, out.SourceHash)
	assert.Contains(t, res.Output, "source-hash: "+out.SourceHash,
		"the summary names the provenance hash")

	for _, f := range out.Files {
		assert.Equal(t, out.SourceHash, f.SourceHash,
			"each file row echoes the run's source hash")
		content := dxReadGenerated(t, root, filepath.Base(f.Path))
		m := dxProvenanceHashRE.FindStringSubmatch(string(content))
		require.NotNil(t, m, "%s must carry a source-hash header", f.Path)
		assert.Equal(t, out.SourceHash, m[1],
			"%s header hash must equal the run's source hash", f.Path)
		// The header must be the first thing in the file, i.e. a banner (the
		// JSON target's banner is the leading field, not a comment).
		assert.Regexp(t, `^(//|/\*) Generated by design_export_tokens|^\{\n  "provenance": "Generated by design_export_tokens`, string(content))
	}
}

// TestDesignExportHandler_ProvenanceHeaderDeterministic repeats a
// filesystem-level export and asserts the header (and thus the whole file)
// stays byte-identical — no timestamp leaks into the banner.
func TestDesignExportHandler_ProvenanceHeaderDeterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	names := dxGeneratedFiles(t, root)
	require.Len(t, names, 6)
	reference := map[string][]byte{}
	for _, name := range names {
		data := dxReadGenerated(t, root, name)
		require.Regexp(t, `"?source-hash"?:\s*"?(fnv1a64:[0-9a-f]{16})`, string(data))
		assert.NotContains(t, string(data), "generated:", "no timestamp in the header")
		reference[name] = data
	}
	for run := 0; run < 10; run++ {
		_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
		require.NoError(t, err)
		for _, name := range names {
			assert.Equal(t, reference[name], dxReadGenerated(t, root, name),
				"%s drifted on run %d", name, run+2)
		}
	}
}

// TestDesignExportHandler_SourceHashRecomputableOffline is the §5f offline
// check at the tool boundary: an independent recomputation from the token
// source file bytes alone reproduces the hash in the artifacts' headers.
func TestDesignExportHandler_SourceHashRecomputableOffline(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.NoError(t, err)
	out := res.StructuredOut.(designExportOutput)

	// Recompute exactly as an offline verifier would: read
	// design/tokens/*.tokens.json, sort by base name, hash "len\n" + bytes.
	source, err := os.ReadFile(filepath.Join(root, design.DirName, design.TokenSubdir, "color.tokens.json"))
	require.NoError(t, err)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d\n", len(source))
	buf.Write(source)
	want := "fnv1a64:" + fmt.Sprintf("%016x", dxFNV1a64(buf.Bytes()))
	assert.Equal(t, want, out.SourceHash)

	css := string(dxReadGenerated(t, root, "tokens.css"))
	assert.Contains(t, css, "source-hash: "+want)
}

// dxFNV1a64 is an independent FNV-1a-64 implementation so the offline-check
// test does not share the production hash code (a shared bug would cancel).
func dxFNV1a64(data []byte) uint64 {
	var h uint64 = 14695981039346656037
	for _, c := range data {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

// ---------------------------------------------------------------------------
// Item 5.2 — refuses dirty alias graphs
// ---------------------------------------------------------------------------

// TestDesignExportHandler_DanglingAliasRefused pins the core 5.2 AC at the tool
// boundary: a dangling alias makes design_export_tokens refuse (hard failure),
// write nothing, and name the offending token in both the summary and the
// structured Refused detail.
func TestDesignExportHandler_DanglingAliasRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, `{
  "color": {
    "brand": { "primary": { "$type": "color", "$value": "#0055ff" } },
    "text":  { "$type": "color", "$value": "{color.brand.missing}" }
  }
}`)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError, "a dirty alias graph is a hard failure")
	assert.ErrorIs(t, err, design.ErrDirtyAliasGraph)
	assert.Contains(t, res.Output, "refused")
	assert.Contains(t, res.Output, "color.text", "the refusal names the offending token")
	assert.Empty(t, dxGeneratedFiles(t, root), "a refused export writes nothing")

	out := res.StructuredOut.(designExportOutput)
	require.Len(t, out.Refused, 1)
	v := out.Refused[0]
	assert.Equal(t, "color.text", v.Token)
	assert.Equal(t, "dangling", v.Kind)
	assert.Equal(t, "design/tokens/color.tokens.json", v.File)
	assert.NotEmpty(t, v.Message)
	assert.Greater(t, v.Line, 0)
}

// TestDesignExportHandler_CyclicAliasRefused covers the cycle half: the tool
// refuses a cyclic alias graph and names a member of the loop.
func TestDesignExportHandler_CyclicAliasRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, `{
  "color": {
    "a": { "$type": "color", "$value": "{color.b}" },
    "b": { "$type": "color", "$value": "{color.a}" }
  }
}`)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.ErrorIs(t, err, design.ErrDirtyAliasGraph)
	assert.Contains(t, res.Output, "cycle")

	out := res.StructuredOut.(designExportOutput)
	require.NotEmpty(t, out.Refused)
	for _, v := range out.Refused {
		assert.Equal(t, "cycle", v.Kind)
		assert.Equal(t, "token_alias_cycle", v.Rule)
		assert.NotEmpty(t, v.Token)
	}
	assert.Empty(t, dxGeneratedFiles(t, root))
}

// TestDesignExportHandler_DirtyGraphRefusedEvenWithTargetSubset proves the
// refusal is graph-level, not target-level: asking for a single target does not
// bypass it.
func TestDesignExportHandler_DirtyGraphRefusedEvenWithTargetSubset(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, `{
  "color": {"x": { "$type": "color", "$value": "{color.nope}" }}
}`)
	h := &designExportHandler{}

	for _, targets := range []string{"css", "ts", "all"} {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": targets})
		require.Error(t, err, "targets=%q must still refuse", targets)
		require.True(t, res.IsError)
	}
	assert.Empty(t, dxGeneratedFiles(t, root))
}

// TestDesignExportHandler_DirtyGraphDoesNotTouchExistingArtifacts proves the
// refusal is non-destructive: a previously-exported (clean) artifact is left
// byte-identical when a later token edit makes the graph dirty and export is
// re-run.
func TestDesignExportHandler_DirtyGraphDoesNotTouchExistingArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.NoError(t, err)
	before := dxReadGenerated(t, root, "tokens.css")

	// Introduce a dangling alias.
	dxWriteTokens(t, root, `{
  "color": {
    "brand": { "primary": { "$type": "color", "$value": "#0055ff" } },
    "broken": { "$type": "color", "$value": "{color.gone}" }
  }
}`)
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Equal(t, before, dxReadGenerated(t, root, "tokens.css"),
		"a refused export must not overwrite the existing artifact")
}

// TestDesignExportHandler_CleanAliasGraphStillExports is the negative control:
// a valid alias chain exports cleanly, with no Refused detail.
func TestDesignExportHandler_CleanAliasGraphStillExports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteTokens(t, root, dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := res.StructuredOut.(designExportOutput)
	assert.Empty(t, out.Refused)
	assert.NotEmpty(t, out.SourceHash)
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestDesignExportHandler_RegisteredInAllTools(t *testing.T) {
	t.Parallel()
	found := false
	for _, h := range AllTools() {
		if h.Name() == "design_export_tokens" {
			found = true
			break
		}
	}
	require.True(t, found, "AllTools() must register design_export_tokens on native builds")
}

// TestDesignExportHandler_NotInSharedList proves SP-140 invariant 7: the
// handler is reached through its build-tagged registrar, not constructed in
// all.go's unconditional shared list (which would put it on the WASM roster).
func TestDesignExportHandler_NotInSharedList(t *testing.T) {
	t.Parallel()
	all, err := os.ReadFile("all.go")
	require.NoError(t, err)
	assert.NotContains(t, string(all), "&designExportHandler{}",
		"design_export_tokens must be registered via registerDesignExportTools(), not the shared list")
	assert.Contains(t, string(all), "registerDesignExportTools()")
}
