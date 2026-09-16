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
    }
  }
}`

const validWireframeBody = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect id="submit" x="24" y="200" width="342" height="52" data-nav="home" />
</svg>`

const validHomeWireframeBody = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Home</text>
</svg>`

const validFlowBody = "flowchart TD\n  login --> home\n"

const validScreenHTML = `<!DOCTYPE html>
<html>
<head>
  <style>body { width: 390px; margin: 0; }</style>
</head>
<body><p>Login screen — draft</p></body>
</html>
`

const validTreeIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 3h18v18H3z" /></svg>`

const validBrandMD = "Logo usage: render in {color.brand.primary} on light backgrounds.\n"

// writeValidDesignTree populates root with a small design/ tree that
// satisfies every validator so a whole-tree run yields zero findings.
func writeValidDesignTree(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write("design/README.md", validTreeManifest)
	write("design/tokens/color.tokens.json", validTokenJSON)
	write("design/wireframes/login.svg", validWireframeBody)
	write("design/wireframes/home.svg", validHomeWireframeBody)
	write("design/flows/sign-up.mmd", validFlowBody)
	write("design/screens/login.html", validScreenHTML)
	write("design/icons/home.svg", validTreeIconSVG)
	write("design/brand/brand.md", validBrandMD)
}

func TestValidateTreeValid(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings, "a completed whole-tree run must return a non-nil slice")
	assert.Empty(t, findings, "a valid design tree must yield zero findings, got %#v", findings)
}

func TestValidateTreeMissingDesignDir(t *testing.T) {
	root := t.TempDir()

	findings, err := ValidateTree(root)
	require.NoError(t, err, "a workspace without design/ is not an error")
	require.NotNil(t, findings)
	assert.Empty(t, findings)

	// A design/ directory with only some subdirectories present is also
	// not an error — the tree may be partial mid-scaffold.
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "tokens"), 0o755))
	findings, err = ValidateTree(root)
	require.NoError(t, err)
	assert.Empty(t, findings)
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
		requireNoWireframeFindings(t, findings)
	})

	t.Run("design-prefix-optional", func(t *testing.T) {
		root := t.TempDir()
		writeValidDesignTree(t, root)

		findings, err := ValidateFile(root, "wireframes/login.svg")
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
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
