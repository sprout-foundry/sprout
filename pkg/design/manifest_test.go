package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validManifest is a README that passes every §1e/§1g check against a tree
// that has the referenced assets present.
const validManifest = `# Design Workspace

## Device frames

frames:
  desktop: 1440x900
  mobile: 390x844

## Screens

- ` + "`login`" + ` — draft — sign-in entry point

## Flows

- ` + "`sign-up`" + ` — ready — account creation

## Status markers

Screens and flows carry one of: ` + "`draft`" + `, ` + "`review`" + `, ` + "`ready`" + `.

## Links

- [Brand guide](brand/brand.md)
- [Color tokens](tokens/color.tokens.json)
`

func TestValidateManifestValid(t *testing.T) {
	root := t.TempDir()
	writeDesignAssets(t, root, []string{
		"design/brand/brand.md",
		"design/tokens/color.tokens.json",
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, "design", "README.md"), []byte(validManifest), 0o644))

	findings := ValidateManifest(root)
	requireNoFindings(t, findings)
}

func TestValidateManifestFrames(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte("# Design\n\ndraft review ready\n"))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestFrames])
		for _, f := range findings {
			if f.Rule == ruleManifestFrames {
				assert.Equal(t, SeverityError, f.Severity)
			}
		}
	})
	t.Run("malformed", func(t *testing.T) {
		content := "frames:\n  desktop: 1440 900\n\n# draft review ready\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestFrames])
		for _, f := range findings {
			if f.Rule == ruleManifestFrames {
				assert.Contains(t, f.Message, "malformed")
			}
		}
	})
	t.Run("empty-block", func(t *testing.T) {
		content := "frames:\n# draft review ready\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestFrames])
	})
}

func TestValidateManifestLinks(t *testing.T) {
	t.Run("resolves", func(t *testing.T) {
		root := t.TempDir()
		writeDesignAssets(t, root, []string{"design/brand/brand.md", "design/tokens/color.tokens.json"})
		findings := validateManifestContent(root, "design/README.md", []byte(validManifest))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("dangling", func(t *testing.T) {
		root := t.TempDir()
		writeDesignAssets(t, root, []string{"design/brand/brand.md"}) // tokens file absent
		findings := validateManifestContent(root, "design/README.md", []byte(validManifest))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestLinkDangling])
		for _, f := range findings {
			if f.Rule == ruleManifestLinkDangling {
				assert.Equal(t, SeverityError, f.Severity, "a dangling relative link is a hard violation")
				assert.Contains(t, f.Message, "tokens/color.tokens.json")
			}
		}
	})
	t.Run("dangling-line-number", func(t *testing.T) {
		// The finding anchors at the 1-based line carrying the broken link.
		content := "frames:\n  m: 1x1\n\n- [gone](brand/nope.md)\n\ndraft review ready\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		require.Len(t, findings, 1)
		assert.Equal(t, ruleManifestLinkDangling, findings[0].Rule)
		assert.Equal(t, 4, findings[0].Line)
	})
	t.Run("dangling-deduplicated", func(t *testing.T) {
		// The same missing target linked twice yields one finding, not two.
		content := "frames:\n  m: 1x1\n\n- [a](brand/nope.md)\n- [b](brand/nope.md)\n\ndraft review ready\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("fenced-links-skipped", func(t *testing.T) {
		// A link inside a fenced code block is documentation, not a live
		// asset link, so a dangling target there must not fire.
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n```\n- [x](nope/missing.md)\n```\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("tilde-fenced-links-skipped", func(t *testing.T) {
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n~~~\n- [x](nope/missing.md)\n~~~\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("external-links-skipped", func(t *testing.T) {
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n- [Site](https://example.com/x)\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("anchor-links-skipped", func(t *testing.T) {
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n- [Jump](#status-markers)\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("absolute-links-skipped", func(t *testing.T) {
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n- [Abs](/etc/hosts)\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("scheme-relative-links-skipped", func(t *testing.T) {
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n- [Doc](//example.com/brand.md)\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("other-uri-scheme-links-skipped", func(t *testing.T) {
		// A scheme like mailto: must not be stat-ed as a relative path.
		content := "frames:\n  mobile: 390x844\n\n# draft review ready\n\n- [Mail](mailto:team@example.com)\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("fragment-and-query-stripped", func(t *testing.T) {
		// #fragment and ?query are stripped before resolving, so a real
		// asset reached through them resolves.
		root := t.TempDir()
		writeDesignAssets(t, root, []string{"design/brand/brand.md"})
		content := "frames:\n  m: 1x1\n\n- [Brand](brand/brand.md?v=2#usage)\n\ndraft review ready\n"
		findings := validateManifestContent(root, "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
	t.Run("parent-relative-links-resolve-verbatim", func(t *testing.T) {
		// ../ links are ordinary relative links: resolved verbatim against
		// design/, so a target that exists above design/ passes and a
		// missing one is a dangling finding.
		root := t.TempDir()
		content := "frames:\n  m: 1x1\n\n- [Out](../outside.md)\n\ndraft review ready\n"
		require.NoFileExists(t, filepath.Join(root, "outside.md"))
		findings := validateManifestContent(root, "design/README.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestLinkDangling])

		require.NoError(t, os.WriteFile(filepath.Join(root, "outside.md"), []byte(""), 0o644))
		findings = validateManifestContent(root, "design/README.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling])
	})
}

func TestValidateManifestStatusMarkers(t *testing.T) {
	t.Run("all-present", func(t *testing.T) {
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte("frames:\n  m: 1x1\n\n# draft review ready\n"))
		assert.Equal(t, 0, findingRules(findings)[ruleManifestStatusMarkers])
	})
	t.Run("missing-ready", func(t *testing.T) {
		content := "frames:\n  m: 1x1\n\n# draft and review\n"
		findings := validateManifestContent(t.TempDir(), "design/README.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleManifestStatusMarkers])
		for _, f := range findings {
			if f.Rule == ruleManifestStatusMarkers {
				assert.Equal(t, SeverityInfo, f.Severity)
				assert.Contains(t, f.Message, "ready")
			}
		}
	})
}

// TestValidateManifestSortedNeverNil covers the exported entry point's
// result contract (finding.go package convention): a multi-finding run
// comes back non-nil, stamped with the manifest path, and sorted by
// file, line, rule, message.
func TestValidateManifestSortedNeverNil(t *testing.T) {
	root := t.TempDir()
	// No assets on disk, no frames block, no status markers: three rule
	// classes fire, with the two dangling links on different lines so the
	// line ordering is observable (line 3 after line 0; line 5 after 3).
	content := "# Design\n\n- [b](nope-b.md)\n\n- [a](nope-a.md)\n"
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, ManifestName), []byte(content), 0o644))

	findings := ValidateManifest(root)
	require.NotNil(t, findings)
	require.Len(t, findings, 4)
	for _, f := range findings {
		assert.Equal(t, filepath.Join(DirName, ManifestName), f.File)
	}
	assertSortedFindings(t, findings)

	assert.Equal(t, ruleManifestFrames, findings[0].Rule)
	assert.Equal(t, ruleManifestStatusMarkers, findings[1].Rule)
	assert.Equal(t, ruleManifestLinkDangling, findings[2].Rule)
	assert.Contains(t, findings[2].Message, "nope-b.md")
	assert.Equal(t, 3, findings[2].Line)
	assert.Equal(t, ruleManifestLinkDangling, findings[3].Rule)
	assert.Contains(t, findings[3].Message, "nope-a.md")
	assert.Equal(t, 5, findings[3].Line)
}

// TestManifestFramesDriveWireframeFrameMatch demonstrates the §1e→§1b
// wiring end to end: device frames declared in the README manifest drive
// the wireframe advisory frame-match check (SP-140-1 §1g).
func TestManifestFramesDriveWireframeFrameMatch(t *testing.T) {
	const manifest = "frames:\n  desktop: 1440x900\n  mobile: 390x844\n\n# draft review ready\n"
	const matched = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`
	const mismatched = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"><text>Login</text></svg>`

	t.Run("matching-viewbox-clean", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": matched}, manifest)
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		// The §9a deprecation notice rides the walk; the per-file rules are clean.
		requireNoFindings(t, dropDeprecationFindings(findings))
		require.Equal(t, 1, findingRules(findings)[ruleWireframeDeprecated])
		// The manifest itself is clean against the same tree.
		requireNoFindings(t, ValidateManifest(root))
	})
	t.Run("mismatched-viewbox-flagged-once", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": mismatched}, manifest)
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		require.Len(t, dropDeprecationFindings(findings), 1)
		assert.Equal(t, ruleSVGFrameMatch, findings[0].Rule)
		assert.Equal(t, SeverityInfo, findings[0].Severity, "frame match is advisory")
		assert.Contains(t, findings[0].Message, "800x600")
	})
	t.Run("no-declared-frames-no-match-check", func(t *testing.T) {
		// Without a frames block the advisory check is skipped entirely.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": mismatched}, "# draft review ready\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGFrameMatch])
	})
}

func TestValidateManifestMissingREADME(t *testing.T) {
	root := t.TempDir()
	findings := ValidateManifest(root)
	require.NotNil(t, findings)
	assert.Empty(t, findings)
}

// assertSortedFindings asserts the package-wide finding order: file,
// then line, then rule, then message.
func assertSortedFindings(t *testing.T, findings []Finding) {
	t.Helper()
	for i := 1; i < len(findings); i++ {
		a, b := findings[i-1], findings[i]
		less := a.File != b.File && a.File < b.File ||
			a.File == b.File && a.Line < b.Line ||
			a.File == b.File && a.Line == b.Line && a.Rule < b.Rule ||
			a.File == b.File && a.Line == b.Line && a.Rule == b.Rule && a.Message < b.Message
		assert.True(t, less, "findings must be sorted by file, line, rule, message: %+v must precede %+v", a, b)
	}
}

// writeDesignAssets creates empty files (dirs included) under root for link
// resolution tests.
func writeDesignAssets(t *testing.T, root string, rels []string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(""), 0o644))
	}
}
