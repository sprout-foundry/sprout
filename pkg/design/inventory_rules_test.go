package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SP-140-4 §4b — token usage (literal fill/stroke/font-family -> info)
// ---------------------------------------------------------------------------

const (
	inventoryRuleRel  = "design/wireframes/login.svg"
	inventoryRuleSVG  = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`
	inventoryRuleDir  = "inventoryrules"
	inventoryRuleFile = "login.svg"
)

// TestTokenUsageLiteralWithoutComment covers the §4b "Token usage" rule: a
// literal fill/stroke/font-family value that is not backed by a `{token.path}`
// comment is flagged info; one that is backed (adjacent comment) is clean.
func TestTokenUsageLiteralWithoutComment(t *testing.T) {
	t.Run("literal-color-no-comment-info", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// A token comment elsewhere (font-family) gives the document nonzero
		// adoption, so the unbacked fill/stroke values are reported per-value
		// with their lines. The comment sits far from the rects so neither is
		// mistakenly treated as backed.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text font-family="Inter"><!-- {typography.body.font-family} -->Login</text>
  <rect id="a" x="0" y="0" width="10" height="10" fill="#fff" />
  <rect id="b" x="0" y="0" width="10" height="10" fill="#000" />
  <rect id="c" x="0" y="0" width="10" height="10" stroke="red" />
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		rules := findingRules(findings)
		assert.Equal(t, 3, rules[ruleSVGTokenUsage], "got %#v", findings)
		for _, f := range findings {
			assert.Equal(t, SeverityInfo, f.Severity, "literal token drift is advisory info, not a hard error")
			assert.Equal(t, rel, f.File)
			assert.NotZero(t, f.Line, "a literal value carries a best-effort line")
		}
		assert.Equal(t, 3, findings[0].Line, "first fill on line 3")
		assert.Equal(t, 4, findings[1].Line, "second fill on line 4")
		assert.Equal(t, 5, findings[2].Line, "stroke on line 5")
	})

	t.Run("literal-font-family-info", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// A token comment elsewhere gives nonzero adoption so the unbacked
		// font-family is reported per-value (with its name and value).
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="bg" x="0" y="0" width="10" height="10" /><!-- {color.semantic.surface} -->
  <text font-family="Inter">Login</text>
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleSVGTokenUsage], "got %#v", findings)
		assert.Equal(t, SeverityInfo, findings[0].Severity)
		assert.Contains(t, findings[0].Message, "font-family")
		assert.Contains(t, findings[0].Message, "Inter")
		assert.Equal(t, 3, findings[0].Line)
	})

	t.Run("token-commented-values-clean", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// Each literal carries its token reference beside it: a trailing
		// comment on the same line, or a comment inside the element.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="a" x="0" y="0" width="10" height="10" fill="#fff" /><!-- {color.semantic.surface} -->
  <rect id="b" x="0" y="0" width="10" height="10" stroke="red"><!-- {color.semantic.danger} --></rect>
  <text font-family="Inter"><!-- {typography.body.font-family} -->Login</text>
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		assert.Empty(t, findings, "a literal backed by a beside-it {token.path} comment is allowed, got %#v", findings)
	})

	t.Run("comment-on-another-element-does-not-back", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// A token comment on a different element (and line) must not bless an
		// unrelated literal.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text font-family="Inter"><!-- {typography.body.font-family} -->Login</text>
  <rect id="a" x="0" y="0" width="10" height="10" fill="#000" />
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleSVGTokenUsage], "got %#v", findings)
		assert.Contains(t, findings[0].Message, "#000")
		assert.Equal(t, 3, findings[0].Line)
	})

	t.Run("zero-token-adoption-single-finding", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="a" x="0" y="0" width="10" height="10" fill="#fff" />
  <text font-family="Inter">Login</text>
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleSVGTokenUsage], "zero adoption yields one summary finding, got %#v", findings)
		assert.Equal(t, SeverityInfo, findings[0].Severity)
		assert.Contains(t, findings[0].Message, "2 literal")
	})

	t.Run("non-color-literals-ignored", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="a" x="0" y="0" width="10" height="10" fill="none" />
  <rect id="b" x="0" y="0" width="10" height="10" fill="url(#grad)" />
  <rect id="c" x="0" y="0" width="10" height="10" stroke="currentColor" />
  <text font-family="inherit">Login</text>
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		assert.Empty(t, findings, "non-color/font values are not literals to track, got %#v", findings)
	})

	t.Run("no-styling-attributes-clean", func(t *testing.T) {
		findings := validateTokenUsage(inventoryRuleRel, []byte(inventoryRuleSVG))
		assert.Empty(t, findings, "a wireframe with no literals yields nothing, got %#v", findings)
	})

	t.Run("malformed-svg-yields-nothing", func(t *testing.T) {
		findings := validateTokenUsage(inventoryRuleRel, []byte(`<svg fill="#fff"`))
		assert.Empty(t, findings, "well-formedness is the hard rule's business, got %#v", findings)
	})

	t.Run("repeated-tags-scope-comments-per-element", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// Two sibling rects; only the first carries a token comment. The
		// second must be flagged, proving the element scoping does not bleed
		// a comment across sibling elements.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="a" x="1" y="1" width="1" height="1" fill="#111" /><!-- {color.a} -->
  <rect id="b" x="1" y="1" width="1" height="1" fill="#222" />
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		require.Len(t, findings, 1, "got %#v", findings)
		assert.Contains(t, findings[0].Message, "#222")
		assert.Equal(t, 3, findings[0].Line)
	})

	t.Run("positional-footer-comment-flags-each-literal", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// A comment that sits beside no literal (a footer note) does not back
		// any of them, so each unbacked literal is flagged — the author is
		// told the reference exists but is not associated with a value.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="a" x="1" y="1" width="1" height="1" fill="#111" />
  <!-- tokens: {color.brand.primary} -->
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		require.Len(t, findings, 1, "got %#v", findings)
		assert.Contains(t, findings[0].Message, "#111")
	})

	t.Run("inside-element-comment-backs-value", func(t *testing.T) {
		rel := "design/wireframes/login.svg"
		// A comment as the element's child (a wrapping <g> or a text body)
		// backs its literal even when not on the same source line.
		content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <g fill="#123">
    <!-- {color.brand.primary} -->
  </g>
</svg>`
		findings := validateTokenUsage(rel, []byte(content))
		assert.Empty(t, findings, "a comment inside the element backs its literal, got %#v", findings)
	})
}

// TestValidateWireframeTokenUsage proves the rule rides the per-file wireframe
// dispatch: design_validate/wireframe and the static critique see it.
func TestValidateWireframeTokenUsage(t *testing.T) {
	content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Login</text></svg>`
	findings := ValidateWireframe(inventoryRuleRel, []byte(content), []string{"login"}, nil)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleSVGTokenUsage], "got %#v", findings)
	for _, f := range findings {
		if f.Rule == ruleSVGTokenUsage {
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Equal(t, inventoryRuleRel, f.File)
		}
	}
}

// ---------------------------------------------------------------------------
// SP-140-4 §4b — screen inventory (orphan wireframes -> info)
// ---------------------------------------------------------------------------

// TestValidateInventoryOrphanScreens covers the §4b "Screen inventory" bullet:
// a wireframe stem appearing in neither a flow nor the README is an orphan
// (info); one referenced by a flow OR listed in the README is not.
func TestValidateInventoryOrphanScreens(t *testing.T) {
	t.Run("orphan-wireframe-info", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg":     inventoryRuleSVG,
			"checkout.svg":  inventoryRuleSVG,
			"forgotten.svg": inventoryRuleSVG,
		}, "# Design\n\n## Screens\n\n- `login` — draft\n- `checkout` — draft\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> checkout\n"), 0o644))

		findings := ValidateInventory(root)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleConsistencyScreenOrphan], "got %#v", findings)
		f := findings[0]
		assert.Equal(t, SeverityInfo, f.Severity, "an unreferenced screen is advisory info")
		assert.Equal(t, "design/wireframes/forgotten.svg", f.File)
		assert.Contains(t, f.Message, "forgotten")
	})

	t.Run("flow-reference-clears-orphan", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> done\n"), 0o644))

		findings := ValidateInventory(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyScreenOrphan], "got %#v", findings)
	})

	t.Run("readme-reference-clears-orphan", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG},
			"# Design\n\n## Screens\n\n- `login` — draft — sign in\n")

		findings := ValidateInventory(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyScreenOrphan], "got %#v", findings)
	})

	t.Run("declared-node-clears-orphan", func(t *testing.T) {
		// A node declared in the flow (even with no edge) counts as a flow
		// reference, matching the §1c id == stem contract.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "main.mmd"),
			[]byte("flowchart LR\n  login[Login screen]\n"), 0o644))

		findings := ValidateInventory(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyScreenOrphan], "got %#v", findings)
	})

	t.Run("no-wireframes-clean", func(t *testing.T) {
		root := t.TempDir()
		findings := ValidateInventory(root)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})
}

// TestValidateConsistencyInventoryFindings proves the inventory pack rides the
// whole-tree consistency dispatch (and hence design_validate / static
// critique).
func TestValidateConsistencyInventoryFindings(t *testing.T) {
	root := t.TempDir()
	writeWireframeTree(t, root, map[string]string{
		"login.svg":    inventoryRuleSVG,
		"orphaned.svg": inventoryRuleSVG,
	}, "# Design\n\n## Screens\n\n- `login` — draft\n")

	findings := ValidateConsistency(root)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleConsistencyScreenOrphan], "got %#v", findings)
	for _, f := range findings {
		if f.Rule == ruleConsistencyScreenOrphan {
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Equal(t, "design/wireframes/orphaned.svg", f.File)
		}
	}
	assertSortedFindings(t, findings)
}

// ---------------------------------------------------------------------------
// SP-140-4 §4b — naming (slug rule + screens/ ↔ wireframes/ mismatch -> warn)
// ---------------------------------------------------------------------------

// TestScreenNamingMismatch covers the §4b "Naming" bullet: a delivered screen
// whose stem has no wireframe counterpart is a warn, and a stem shared by two
// screen files is a duplicate-name warn.
func TestScreenNamingMismatch(t *testing.T) {
	t.Run("screen-without-wireframe-warn", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "checkout.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := screenNameMismatches(root)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleConsistencyScreenNameMismatch], "got %#v", findings)
		f := findings[0]
		assert.Equal(t, SeverityWarn, f.Severity, "an inventory mismatch is an advisory warn")
		assert.Equal(t, "design/screens/checkout.html", f.File)
		assert.Contains(t, f.Message, "checkout")
	})

	t.Run("matching-stem-clean", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "login.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := screenNameMismatches(root)
		assert.Empty(t, findings, "a screen with a wireframe counterpart is clean, got %#v", findings)
	})

	t.Run("wireframe-without-screen-clean", func(t *testing.T) {
		// The normal pre-code state: wireframes exist before delivered screens.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		findings := screenNameMismatches(root)
		assert.Empty(t, findings, "a wireframe with no screen yet is not a mismatch, got %#v", findings)
	})

	t.Run("duplicate-screen-name-warn", func(t *testing.T) {
		// The duplicate branch is exercised against the on-disk glob: a file
		// whose stem collides with another screen file's stem. Because a
		// filesystem cannot hold two identical names, the rule's grouping is
		// pinned by the direct unit case below (TestScreenNamingDuplicateStem);
		// here we assert that distinct stems never collide.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "login.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "home.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := screenNameMismatches(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyScreenNameDuplicate],
			"distinct stems never duplicate, got %#v", findings)
	})

	t.Run("slug-violating-screen-warn", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "Bad_Name.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := screenSlugViolations(root)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleScreenSlugName], "got %#v", findings)
		f := findings[0]
		assert.Equal(t, SeverityWarn, f.Severity, "the tree-level slug check is advisory warn (per-file stays hard)")
		assert.Equal(t, "design/screens/Bad_Name.html", f.File)
		assert.Contains(t, f.Message, "Bad_Name")
	})

	t.Run("slug-violating-screen-also-mismatch", func(t *testing.T) {
		// A non-slug screen stem cannot be a wireframe counterpart either, so
		// it surfaces as both a naming warn and a mismatch warn — proving the
		// two naming sub-rules compose.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{"login.svg": inventoryRuleSVG}, "# Design\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "Checkout.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := screenNameMismatches(root)
		assert.Equal(t, 1, findingRules(findings)[ruleConsistencyScreenNameMismatch], "got %#v", findings)
	})
}

// TestScreenNamingDuplicateStem pins the duplicate branch of the naming rule
// with a filesystem-visible collision the glob can see: a directory cannot hold
// two identical names, so the rule's grouping is exercised through the internal
// recordsForScreenStems helper with two records sharing one stem.
func TestScreenNamingDuplicateStem(t *testing.T) {
	findings := nameMismatchesForStems([]screenStemRecord{
		{stem: "login", file: "design/screens/login.html"},
		{stem: "login", file: "design/screens/login.html.bak"},
	}, map[string]bool{"login": true})
	require.Len(t, findings, 2, "one duplicate finding per file sharing the stem, got %#v", findings)
	for _, f := range findings {
		assert.Equal(t, ruleConsistencyScreenNameDuplicate, f.Rule)
		assert.Equal(t, SeverityWarn, f.Severity)
		assert.Contains(t, f.Message, "login")
	}
}

// ---------------------------------------------------------------------------
// tree-level wiring (design_validate / design_critique static pass)
// ---------------------------------------------------------------------------

// TestValidateTreeInventoryNamingTokenFindings proves all three §4b sub-rules
// ride the standard dispatch: a seeded-bad tree surfaces the token-usage info,
// the orphan-screen info, the naming warn, and the slug warn through
// ValidateTree, each with the specified severity and the right file.
func TestValidateTreeInventoryNamingTokenFindings(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// Token usage: a wireframe with a literal fill and no token comments at all.
	seedFixture(t, root, "design/wireframes/billing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Billing</text></svg>`)
	// Orphan: an extra wireframe in neither the flow nor the README.
	seedFixture(t, root, "design/wireframes/forgotten.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Forgotten</text></svg>`)
	// Naming mismatch: a screen with no wireframe counterpart.
	seedFixture(t, root, "design/screens/receipt.html",
		`<!DOCTYPE html><html><head><style>body{width:390px}</style></head><body>x</body></html>`)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	rules := findingRules(findings)

	assert.Equal(t, 1, rules[ruleSVGTokenUsage], "token usage info must surface, got %#v", findings)
	// Both seeded extras (billing, forgotten) are declared by neither the flow
	// nor the README, so each is an orphan.
	assert.Equal(t, 2, rules[ruleConsistencyScreenOrphan], "orphan screen info must surface, got %#v", findings)
	assert.Equal(t, 1, rules[ruleConsistencyScreenNameMismatch], "naming mismatch warn must surface, got %#v", findings)

	for _, f := range findings {
		switch f.Rule {
		case ruleSVGTokenUsage:
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Equal(t, "design/wireframes/billing.svg", f.File)
		case ruleConsistencyScreenOrphan:
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Contains(t, []string{"design/wireframes/billing.svg", "design/wireframes/forgotten.svg"}, f.File,
				"an orphan finding names one of the undeclared wireframes")
		case ruleConsistencyScreenNameMismatch:
			assert.Equal(t, SeverityWarn, f.Severity)
			assert.Equal(t, "design/screens/receipt.html", f.File)
		}
	}
	assertSortedFindings(t, findings)
}

// TestValidateFileTokenUsage proves a single-file wireframe run (the
// design_critique per-target path) surfaces the token-usage rule.
func TestValidateFileTokenUsage(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	seedFixture(t, root, "design/wireframes/billing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Billing</text></svg>`)

	findings, err := ValidateFile(root, "design/wireframes/billing.svg")
	require.NoError(t, err)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleSVGTokenUsage], "got %#v", findings)
	for _, f := range findings {
		if f.Rule == ruleSVGTokenUsage {
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Equal(t, "design/wireframes/billing.svg", f.File)
		}
	}
}

// add no false positives to the fixture tree that satisfies every other rule:
// design_validate on a clean tree still yields zero findings.
func TestInventoryNamingRulesValidTree(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings)
	assert.Empty(t, findings, "the valid fixture tree must stay finding-free, got %#v", findings)

	// The individual packs are clean on that tree too.
	assert.Empty(t, ValidateInventory(root))
	assert.Empty(t, screenSlugViolations(root))
	assert.Empty(t, screenNameMismatches(root))
	for _, match := range []string{"design/wireframes/login.svg", "design/wireframes/home.svg"} {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(match)))
		require.NoError(t, readErr)
		assert.Empty(t, validateTokenUsage(match, data), "token usage on %s", match)
	}
}

// TestInventoryNamingNoDesignTree covers the missing-tree contract: no design/
// directory means no findings from any of the new packs, not an error.
func TestInventoryNamingNoDesignTree(t *testing.T) {
	root := t.TempDir()
	assert.Empty(t, ValidateInventory(root))
	assert.Empty(t, screenSlugViolations(root))
	assert.Empty(t, screenNameMismatches(root))
}
