package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validWireframeSVG is a self-contained wireframe that satisfies every hard
// and advisory rule against a mobile frame and a resolvable data-nav target,
// so a consistent run yields zero findings.
// The literal font-family is backed by a {token.path} comment (SP-140-4 §4b
// "Token usage"), so the otherwise-clean fixture also satisfies the
// token-usage rule: a wireframe may use a literal value when the intended
// token is recorded in a comment beside it.
const validWireframeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28" font-family="Inter"><!-- {typography.body.font-family} -->Login</text>
  <rect id="submit" x="24" y="200" width="342" height="52" data-nav="home" />
</svg>`

const validWireframeRel = "design/wireframes/login.svg"

var validFrameMobile = Frame{Name: "mobile", Width: 390, Height: 844} // requireNoWireframeFindings asserts a clean run: a non-nil, empty slice.
func requireNoWireframeFindings(t *testing.T, findings []Finding) {
	t.Helper()
	require.NotNil(t, findings, "a completed validation run must return a non-nil slice")
	assert.Empty(t, findings)
}

// findingRules returns the set of rule ids present in findings.
func findingRules(findings []Finding) map[string]int {
	out := make(map[string]int)
	for _, f := range findings {
		out[f.Rule]++
	}
	return out
}

func TestValidateWireframeValid(t *testing.T) {
	findings := ValidateWireframe(validWireframeRel, []byte(validWireframeSVG), []string{"login", "home"}, []Frame{validFrameMobile})
	requireNoWireframeFindings(t, findings)
}

func TestValidateWireframeWellFormed(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"truncated", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect`},
		{"stray-close", `<svg viewBox="0 0 10 10"></text></svg>`},
		{"unclosed-attr", `<svg viewBox="0 0 10 10" <text/>`},
		{"empty", ``},
		{"whitespace-only", `   `},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := ValidateWireframe(validWireframeRel, []byte(tc.content), nil, nil)
			rules := findingRules(findings)
			assert.Equal(t, 1, rules[ruleSVGWellformed], "expected exactly one well-formedness finding, got %#v", findings)
			for _, f := range findings {
				if f.Rule == ruleSVGWellformed {
					assert.Equal(t, SeverityError, f.Severity, "well-formedness is a hard violation")
				}
			}
		})
	}
}

func TestValidateWireframeRootNotSVG(t *testing.T) {
	findings := ValidateWireframe(validWireframeRel, []byte(`<g viewBox="0 0 10 10"></g>`), nil, nil)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleSVGViewBox])
	for _, f := range findings {
		if f.Rule == ruleSVGViewBox {
			assert.Contains(t, f.Message, "expected <svg>")
		}
	}
}

func TestValidateWireframeViewBox(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><text/></svg>`), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGViewBox])
	})
	t.Run("non-integer", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(`<svg viewBox="0 0 1440 90.5"><text/></svg>`), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGViewBox])
	})
	t.Run("too-few-values", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(`<svg viewBox="0 0 1440"><text/></svg>`), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGViewBox])
	})
	t.Run("integer-ok", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(`<svg viewBox="0 0 1440 900"><text/></svg>`), nil, nil)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGViewBox])
	})
}

func TestValidateWireframeSelfContainment(t *testing.T) {
	t.Run("script-clean", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><script>bad()</script><text/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGSelfContainment])
	})
	t.Run("external-href", func(t *testing.T) {
		content := `<svg viewBox="0 0 10 10"><text/><image href="https://cdn.example.com/logo.png"/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGSelfContainment])
	})
	t.Run("external-protocol-relative", func(t *testing.T) {
		content := `<svg viewBox="0 0 10 10"><text/><a href="//example.com"><text>go</text></a></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGSelfContainment])
	})
	t.Run("local-ref", func(t *testing.T) {
		content := `<svg viewBox="0 0 10 10"><text/><image src="./raster.png"/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGSelfContainment])
	})
	t.Run("data-uri-ok", func(t *testing.T) {
		content := `<svg viewBox="0 0 10 10"><text/><image href="data:image/png;base64,AA=="/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGSelfContainment])
	})
	t.Run("fragment-ok", func(t *testing.T) {
		content := `<svg viewBox="0 0 10 10"><defs><g id="box"/></defs><text/><use href="#box"/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGSelfContainment])
	})
}

func TestValidateWireframeSlugName(t *testing.T) {
	findings := ValidateWireframe("design/wireframes/My Screen.svg", []byte(validWireframeSVG), nil, nil)
	assert.Equal(t, 1, findingRules(findings)[ruleSVGSlugName])
}

func TestValidateWireframeDataNav(t *testing.T) {
	content := `<svg viewBox="0 0 10 10"><text/><rect id="go" data-nav="dashboard"/></svg>`
	t.Run("dangling", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(content), []string{"login"}, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGDataNavDangling])
	})
	t.Run("resolves", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(content), []string{"login", "dashboard"}, nil)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGDataNavDangling])
	})
}

func TestValidateWireframeAdvisories(t *testing.T) {
	t.Run("no-text", func(t *testing.T) {
		content := `<svg viewBox="0 0 390 844"><rect/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), nil, []Frame{validFrameMobile})
		assert.Equal(t, 1, findingRules(findings)[ruleSVGTextUsage])
	})
	t.Run("no-stable-id", func(t *testing.T) {
		content := `<svg viewBox="0 0 390 844"><text/><rect data-nav="home"/></svg>`
		findings := ValidateWireframe(validWireframeRel, []byte(content), []string{"home"}, []Frame{validFrameMobile})
		assert.Equal(t, 1, findingRules(findings)[ruleSVGStableIDs])
	})
	t.Run("frame-mismatch", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(validWireframeSVG), []string{"home"}, []Frame{{Name: "desktop", Width: 1440, Height: 900}})
		assert.Equal(t, 1, findingRules(findings)[ruleSVGFrameMatch])
	})
	t.Run("frame-match-ok", func(t *testing.T) {
		findings := ValidateWireframe(validWireframeRel, []byte(validWireframeSVG), []string{"home"}, []Frame{validFrameMobile})
		assert.Equal(t, 0, findingRules(findings)[ruleSVGFrameMatch])
	})
}

func TestValidateWireframeSeverityClasses(t *testing.T) {
	// A document tripping one hard and one advisory rule must carry the
	// right severities.
	content := `<svg viewBox="0 0 5 5"><script>x</script></svg>`
	findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
	byRule := map[string]Severity{}
	for _, f := range findings {
		byRule[f.Rule] = f.Severity
	}
	assert.Equal(t, SeverityError, byRule[ruleSVGSelfContainment])
	assert.Equal(t, SeverityInfo, byRule[ruleSVGTextUsage])
}

func TestValidateWireframeLineNumber(t *testing.T) {
	// A script on line 3 must carry Line=3, proving the line support works.
	content := `<svg viewBox="0 0 10 10">
  <text/>
  <script>bad()</script>
</svg>`
	findings := ValidateWireframe(validWireframeRel, []byte(content), nil, nil)
	var sc Finding
	for _, f := range findings {
		if f.Rule == ruleSVGSelfContainment {
			sc = f
		}
	}
	require.NotEmpty(t, findings)
	assert.Equal(t, 3, sc.Line)
}

// TestValidateWireframesDir exercises the dir-level entry point: stem
// gathering for cross-file data-nav resolution, README frame loading, and
// graceful handling of a missing/empty directory.
func TestValidateWireframesDir(t *testing.T) {
	t.Run("resolves-across-files", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="home"/></svg>`,
			"home.svg":  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text><rect id="back" data-nav="login"/></svg>`,
		}, "frames:\n  mobile: 390x844\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("dangling-across-files", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="nowhere"/></svg>`,
			"home.svg":  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`,
		}, "")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleSVGDataNavDangling])
		for _, f := range findings {
			if f.Rule == ruleSVGDataNavDangling {
				assert.Contains(t, f.Message, "nowhere")
			}
		}
	})

	t.Run("missing-dir", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("empty-dir", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})
}

// writeWireframeTree writes the given wireframe files under root/design/
// wireframes/ and an optional design/README.md, for dir-level tests.
func writeWireframeTree(t *testing.T, root string, files map[string]string, readme string) {
	t.Helper()
	dir := filepath.Join(root, "design", "wireframes")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	if readme != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "README.md"), []byte(readme), 0o644))
	}
}
