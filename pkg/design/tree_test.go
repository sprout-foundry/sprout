package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validTreeManifest is a README manifest that satisfies every manifest rule:
// a parseable frames: block, all three status markers, and only resolvable
// relative links.
const validTreeManifest = `# Design Workspace

Inventory and contract for the design assets.

## Device frames

frames:
  desktop: 1440x900
  mobile: 390x844

## Screens

- ` + "`login`" + ` — draft — sign-in entry point
- ` + "`home`" + ` — ready — post-sign-in landing

## Components

- ` + "`button`" + ` — draft — action buttons in key states

## Flows

- ` + "`sign-up`" + ` — draft — account creation

## Status markers

Screens and flows carry one of: draft, review, ready.

## Links

- [Brand guide](brand/brand.md)
- [Color tokens](tokens/color.tokens.json)
- [Login screen](screens/login.html)
`

const validTokenJSON = `{
  "color": {
    "brand": {
      "primary": {"$value": "#0055ff", "$type": "color"},
      "secondary": {"$value": "{color.brand.primary}", "$type": "color"}
    },
    "semantic": {
      "surface": {"$value": "#ffffff", "$type": "color"}
    }
  }
}`

const validWireframeBody = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect id="submit" fill="#fff" x="24" y="200" width="342" height="52" data-nav="home" /><!-- {color.semantic.surface} -->
</svg>`

const validHomeWireframeBody = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Home</text>
</svg>`

const validFlowBody = "flowchart TD\n  login --> home\n  home --> home\n"

const validScreenHTML = `<!DOCTYPE html>
<html data-screen="login">
<head>
  <style>body { width: 390px; margin: 0; }</style>
</head>
<body><p>Login screen — draft</p></body>
</html>
`

const validTreeIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 3h18v18H3z" /></svg>`

const validBrandMD = "Logo usage: render in {color.brand.primary} on light backgrounds.\n"

// writeValidDesignTree populates root with a small design/ tree that
// satisfies every validator — including the SP-140-1 §1h git contract
// (.gitattributes diff rule, .gitignore cache-only policy) — so a whole-tree
// run yields zero findings.
func writeValidDesignTree(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write("design/README.md", validTreeManifest)
	write("design/tokens/color.tokens.json", validTokenJSON)
	write("design/tokens/typography.tokens.json", `{
  "typography": {
    "label": {
      "font-family": {"$value": "Inter, sans-serif", "$type": "fontFamily"}
    }
  }
}`)
	write("design/wireframes/login.svg", validWireframeBody)
	write("design/wireframes/home.svg", validHomeWireframeBody)
	write("design/components/button.svg", validComponentBody)
	write("design/flows/sign-up.mmd", validFlowBody)
	write("design/screens/login.html", validScreenHTML)
	write("design/icons/home.svg", validTreeIconSVG)
	write("design/brand/brand.md", validBrandMD)
	// SP-143 §143.5: the screen contract artifacts — derived index + runtime.
	writeScreensKitArtifacts(t, root)
	// Repository-level git contract: present, with unrelated rules that must
	// stay untouched, plus the required design lines.
	write(GitContractFile, "* text=auto eol=lf\n*.png binary\n"+GitAttributesDiffHTMLLine+"\n")
	write(GitIgnoreFile, "node_modules/\n"+GitIgnoreCacheLine+"\n")
}

func TestValidateTreeValid(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings, "a completed whole-tree run must return a non-nil slice")
	// The legacy wireframes carry their §9a deprecation notices (9.4
	// migrates the tier); every other rule must be clean.
	assert.Empty(t, dropDeprecationFindings(findings),
		"a valid design tree must yield no findings beyond the wireframe deprecation notices, got %#v", findings)
	assert.Equal(t, 2, findingRules(findings)[ruleWireframeDeprecated])
}

func TestValidateTreeFeedbackFinding(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// A §4d feedback file whose target does not resolve to any known stem:
	// the whole-tree run must surface it (the feedback validator is wired
	// into ValidateTree), with the correct file and advisory severity.
	path := filepath.Join(root, DirName, FeedbackSubdir, "login.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path,
		[]byte(fbDoc("design/wireframes/nowhere.svg", "changes-requested", fbAnnotation)), 0o644))

	findings, err := ValidateTree(root)
	require.NoError(t, err)

	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleFeedbackTargetDangling],
		"ValidateTree must surface the feedback dangling-target finding, got %#v", findings)
	for _, f := range findings {
		if f.Rule == ruleFeedbackTargetDangling {
			assert.Equal(t, "design/feedback/login.json", f.File)
			assert.Equal(t, SeverityInfo, f.Severity)
		}
	}
}

func TestValidateTreeMissingDesignDir(t *testing.T) {
	root := t.TempDir()

	findings, err := ValidateTree(root)
	require.NoError(t, err, "a workspace without design/ is not an error")
	require.NotNil(t, findings)
	assert.Empty(t, findings, "no design/ tree means no git-contract findings either")

	// A design/ directory with only some subdirectories present is a partial
	// mid-scaffold tree whose assets are all missing; the §1h git contract is
	// the only thing the run can report, and it does, as fix findings.
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "tokens"), 0o755))
	findings, err = ValidateTree(root)
	require.NoError(t, err)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[FixGitAttributesRule])
	assert.Equal(t, 1, rules[FixGitIgnoreCacheRule])
	assert.Len(t, findings, 2, "a partial tree yields only the git-contract fixes, got %#v", findings)
}

func TestValidateTreeSeededBad(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// Seed a bad token: a dangling alias reference.
	badToken := filepath.Join(root, DirName, "tokens", "spacing.tokens.json")
	require.NoError(t, os.WriteFile(badToken,
		[]byte(`{"spacing": {"md": {"$value": "{spacing.missing}", "$type": "dimension"}}}`), 0o644))

	// Seed a bad wireframe: a data-nav target with no matching stem.
	badWireframe := filepath.Join(root, DirName, "wireframes", "signup.svg")
	require.NoError(t, os.WriteFile(badWireframe,
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Sign up</text><rect id="go" data-nav="nowhere" /></svg>`), 0o644))

	findings, err := ValidateTree(root)
	require.NoError(t, err)

	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleTokenAliasDangling], "expected the dangling token alias finding, got %#v", findings)
	assert.Equal(t, 1, rules[ruleSVGDataNavDangling], "expected the dangling data-nav finding, got %#v", findings)

	// The findings carry the structured shape with file/severity filled in.
	for _, f := range findings {
		if f.Rule == ruleTokenAliasDangling {
			assert.Equal(t, "design/tokens/spacing.tokens.json", f.File)
			assert.Equal(t, SeverityError, f.Severity)
		}
		if f.Rule == ruleSVGDataNavDangling {
			assert.Equal(t, "design/wireframes/signup.svg", f.File)
			assert.Equal(t, SeverityError, f.Severity)
		}
	}

	// The combined result is deterministically sorted by file.
	for i := 1; i < len(findings); i++ {
		assert.LessOrEqual(t, findings[i-1].File, findings[i].File,
			"ValidateTree findings must be sorted by file")
	}
}

func TestValidateTreeEverySeverityClassReported(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// A raw hex color in brand.md is a warn — assert the whole-tree run
	// surfaces advisory severities too, not just hard errors.
	brandPath := filepath.Join(root, DirName, "brand", "brand.md")
	require.NoError(t, os.WriteFile(brandPath, []byte("Primary is #ff0000; accent is {color.brand.secondary}.\n"), 0o644))

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleBrandRawHex])
	for _, f := range findings {
		if f.Rule == ruleBrandRawHex {
			assert.Equal(t, SeverityWarn, f.Severity)
		}
	}
}

func TestValidateFileWireframe(t *testing.T) {
	t.Run("good", func(t *testing.T) {
		root := t.TempDir()
		writeValidDesignTree(t, root)

		findings, err := ValidateFile(root, "design/wireframes/login.svg")
		require.NoError(t, err)
		// The §9a deprecation notice rides the single-file walk; the
		// per-file rules must be clean.
		requireNoWireframeFindings(t, dropDeprecationFindings(findings))
		assert.Equal(t, 1, findingRules(findings)[ruleWireframeDeprecated])
	})

	t.Run("design-prefix-optional", func(t *testing.T) {
		root := t.TempDir()
		writeValidDesignTree(t, root)

		findings, err := ValidateFile(root, "wireframes/login.svg")
		require.NoError(t, err)
		requireNoWireframeFindings(t, dropDeprecationFindings(findings))
		assert.Equal(t, 1, findingRules(findings)[ruleWireframeDeprecated])
	})

	t.Run("missing-viewbox", func(t *testing.T) {
		root := t.TempDir()
		writeValidDesignTree(t, root)
		bad := filepath.Join(root, DirName, "wireframes", "checkout.svg")
		require.NoError(t, os.WriteFile(bad,
			[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text x="0" y="0">Checkout</text></svg>`), 0o644))

		findings, err := ValidateFile(root, "design/wireframes/checkout.svg")
		require.NoError(t, err)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleSVGViewBox], "expected the missing-viewBox finding, got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleSVGViewBox {
				assert.Equal(t, SeverityError, f.Severity)
				assert.Equal(t, "design/wireframes/checkout.svg", f.File)
			}
		}
	})

	t.Run("dangling-data-nav", func(t *testing.T) {
		root := t.TempDir()
		writeValidDesignTree(t, root)
		bad := filepath.Join(root, DirName, "wireframes", "checkout.svg")
		require.NoError(t, os.WriteFile(bad,
			[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Checkout</text><rect id="go" data-nav="nowhere" /></svg>`), 0o644))

		findings, err := ValidateFile(root, "design/wireframes/checkout.svg")
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGDataNavDangling])
	})
}

func TestValidateFileTokens(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateFile(root, "design/tokens/color.tokens.json")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// Broken alias: the dispatch itself succeeds; the rule violation is a
	// finding, never an error.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "tokens", "bad.tokens.json"),
		[]byte(`{"a": {"$value": "{nope.missing}", "$type": "color"}}`), 0o644))
	findings, err = ValidateFile(root, "design/tokens/bad.tokens.json")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleTokenAliasDangling])
}

func TestValidateFileFlow(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateFile(root, "design/flows/sign-up.mmd")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// A screen flow (a node id matches a wireframe stem) carrying an
	// unrelated node id trips the node-id == stem rule.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "broken.mmd"),
		[]byte("flowchart TD\n  login --> not-a-stem\n"), 0o644))
	findings, err = ValidateFile(root, "design/flows/broken.mmd")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleFlowchartNodeStem])
}

func TestValidateFileComponent(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// writeValidDesignTree already carries design/components/button.svg; a
	// clean component spec validates with zero findings through the
	// single-file dispatch (the path is recognized, not "not a recognized
	// design asset").
	findings, err := ValidateFile(root, "design/components/button.svg")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// The short form (no design/ prefix) dispatches too.
	findings, err = ValidateFile(root, "components/button.svg")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// A malformed component spec is a finding, not an error.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "components", "bad.svg"),
		[]byte(`<svg viewBox="0 0 10 10"><script>x()</script><text/></svg>`), 0o644))
	findings, err = ValidateFile(root, "design/components/bad.svg")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleSVGSelfContainment])
}

func TestValidateFileScreen(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateFile(root, "design/screens/login.html")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// A network script src is a hard self-containment finding.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "bad.html"),
		[]byte(`<html><head><script src="https://cdn.example.com/x.js"></script></head><body></body></html>`), 0o644))
	findings, err = ValidateFile(root, "design/screens/bad.html")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
}

func TestValidateFileIcon(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateFile(root, "design/icons/home.svg")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	// sprite.svg is exempt from the slug rule; its <symbol> ids are checked.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "icons", "sprite.svg"),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><symbol id="Bad ID" viewBox="0 0 24 24"></symbol></svg>`), 0o644))
	findings, err = ValidateFile(root, "design/icons/sprite.svg")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleIconSpriteSymbol])
}

func TestValidateFileBrand(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateFile(root, "design/brand/brand.md")
	require.NoError(t, err)
	requireNoWireframeFindings(t, findings)

	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "brand", "brand.md"),
		[]byte("Primary is #ff0000.\n"), 0o644))
	findings, err = ValidateFile(root, "design/brand/brand.md")
	require.NoError(t, err)
	assert.Equal(t, 1, findingRules(findings)[ruleBrandRawHex])
	assert.Equal(t, 1, findingRules(findings)[ruleBrandNoTokenRefs])
}

func TestValidateFileManifest(t *testing.T) {
	t.Run("frames-block-findings", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, ManifestName),
			[]byte("# Design\n\nNo frames block here.\n"), 0o644))

		findings, err := ValidateFile(root, "design/README.md")
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleManifestFrames])
	})

	t.Run("dangling-link-line", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, ManifestName),
			[]byte("frames:\n  mobile: 390x844\n\nStatus: draft, review, ready.\n\n- [Missing](tokens/nope.tokens.json)\n"), 0o644))

		findings, err := ValidateFile(root, "design/README.md")
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleManifestLinkDangling])
		for _, f := range findings {
			if f.Rule == ruleManifestLinkDangling {
				assert.Equal(t, 6, f.Line, "the finding must carry the dangling link's 1-based line")
			}
		}
	})
}

func TestValidateFileErrors(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	t.Run("nonexistent", func(t *testing.T) {
		_, err := ValidateFile(root, "design/wireframes/nope.svg")
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("nonexistent-without-design-prefix", func(t *testing.T) {
		_, err := ValidateFile(root, "wireframes/nope.svg")
		require.Error(t, err)
	})

	t.Run("outside-design-tree", func(t *testing.T) {
		_, err := ValidateFile(root, "../outside/secret.svg")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "under design/")

		_, err = ValidateFile(root, "/etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "relative")
	})

	t.Run("directory", func(t *testing.T) {
		_, err := ValidateFile(root, "design/wireframes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("empty", func(t *testing.T) {
		_, err := ValidateFile(root, "  ")
		require.Error(t, err)
	})

	t.Run("unrecognized-asset", func(t *testing.T) {
		// The files must exist so the dispatch (not the existence check)
		// produces the error.
		for _, rel := range []string{
			"design/notes.txt",
			"design/tokens/colors.json",   // wrong extension for the tokens dir
			"design/wireframes/thing.png", // not an SVG
			"design/components/thing.png", // not an SVG
			"design/brand/logo.png",       // logos must be SVG
		} {
			path := filepath.Join(root, filepath.FromSlash(rel))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
			_, err := ValidateFile(root, rel)
			require.Error(t, err, "path %q must not validate", rel)
			assert.Contains(t, err.Error(), "not a recognized design asset", "path %q", rel)
		}
	})
}

func TestValidateFileMissingDesignDir(t *testing.T) {
	root := t.TempDir()
	_, err := ValidateFile(root, "design/wireframes/login.svg")
	require.Error(t, err)
}
