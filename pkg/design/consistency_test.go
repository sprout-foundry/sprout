package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// §4b rule 1 — every data-nav target exists (existing ruleSVGDataNavDangling,
// exercised here as part of the consistency pack)
// ---------------------------------------------------------------------------

// TestConsistencyDataNavTargetExists covers the §4b "every data-nav target
// exists" bullet: a data-nav value that is not a wireframe stem is a hard
// finding, and one that resolves produces none.
func TestConsistencyDataNavTargetExists(t *testing.T) {
	content := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="checkout"/></svg>`

	t.Run("dangling-is-error", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": content,
			// checkout.svg is deliberately absent.
		}, "frames:\n  mobile: 390x844\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGDataNavDangling])
		for _, f := range findings {
			if f.Rule == ruleSVGDataNavDangling {
				assert.Equal(t, SeverityError, f.Severity, "a data-nav target with no wireframe is a hard violation")
				assert.Contains(t, f.Message, "checkout")
			}
		}
	})

	t.Run("resolved-clean", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg":    content,
			"checkout.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Checkout</text></svg>`,
		}, "frames:\n  mobile: 390x844\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGDataNavDangling])
	})
}

// ---------------------------------------------------------------------------
// §4b rule 2 — every flow edge has a wireframe counterpart unless terminal
// ---------------------------------------------------------------------------

// TestValidateFlowsBidirectionality covers the §4b flow-edge rule directly
// (the single-file entry point the ValidateFile dispatch uses).
func TestValidateFlowsBidirectionality(t *testing.T) {
	const rel = "design/flows/main.mmd"

	t.Run("non-terminal-missing-wireframe-flagged", func(t *testing.T) {
		// login and home have wireframes; "checkout" is the target of an edge
		// and the source of none — it is a *terminal* state and is exempt.
		// "settings" is both source and target (non-terminal) with no
		// wireframe, so it trips the rule.
		content := "flowchart LR\n  login --> home\n  home --> settings\n  settings --> home\n  home --> checkout\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home"})
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleConsistencyFlowEdgeWireframe {
				assert.Equal(t, SeverityWarn, f.Severity, "a broken flow/wireframe link is an advisory consistency warn")
				assert.Equal(t, rel, f.File)
				assert.Contains(t, f.Message, "settings")
				assert.Contains(t, f.Message, "non-terminal")
				assert.Equal(t, 3, f.Line, "the finding anchors at the node's first mention")
			}
		}
	})

	t.Run("terminal-node-exempt", func(t *testing.T) {
		// "welcome" is only ever an edge target (no outgoing edge) — a terminal
		// state, exempt from the wireframe requirement per §4b.
		content := "flowchart LR\n  login --> home\n  home --> welcome\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home"})
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
	})

	t.Run("all-wireframes-clean", func(t *testing.T) {
		content := "flowchart LR\n  login --> home\n  home --> checkout\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home", "checkout"})
		requireNoWireframeFindings(t, findings)
	})

	t.Run("source-only-node-flagged", func(t *testing.T) {
		// "ghost" is the source of an edge and the target of none: the edge
		// leaves a screen that does not exist, so it is non-terminal-broken.
		content := "flowchart LR\n  login --> home\n  ghost --> home\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home"})
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleConsistencyFlowEdgeWireframe {
				assert.Contains(t, f.Message, "ghost")
			}
		}
	})

	t.Run("mixed-stem-and-non-stem-edges", func(t *testing.T) {
		// A flow that is a screen flow (login/home are stems) but whose edges
		// also chain through a non-stem non-terminal node: the non-stem node
		// is flagged, the stems are not.
		content := "flowchart LR\n  login --> home\n  home --> wizard\n  wizard --> home\n  home --> done\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home"})
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleConsistencyFlowEdgeWireframe {
				assert.Contains(t, f.Message, "wizard")
				assert.NotContains(t, f.Message, `"done"`, "a terminal target with no outgoing edge is exempt")
			}
		}
	})

	t.Run("process-flow-untouched", func(t *testing.T) {
		// No edge endpoint is a wireframe stem, so this is a process/user flow:
		// nothing cross-checks it (matching the §1c node-stem scoping).
		content := "flowchart TD\n  start --> process --> done\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login"})
		requireNoWireframeFindings(t, findings)
	})

	t.Run("deduplicated-per-node", func(t *testing.T) {
		// One node referenced by three edges yields one finding.
		content := "flowchart LR\n  login --> ghost\n  ghost --> ghost2\n  ghost --> home\n"
		findings := flowBidirectionalityFindings(rel, []byte(content), []string{"login", "home"})
		assert.Equal(t, 1, findingRules(findings)[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
	})
}

// TestValidateFlowsDirBidirectionality exercises the rule through the dir-level
// validator: wireframe stems are gathered from disk so the cross-file
// resolution is real.
func TestValidateFlowsDirBidirectionality(t *testing.T) {
	t.Run("dangling-non-terminal-across-files", func(t *testing.T) {
		// "profile" is wired as a flow node but has no wireframe file, and it is
		// non-terminal (home -> profile -> home), so the §4b edge rule fires.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg":   `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
			"home.svg":    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`,
			"profile.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Profile</text></svg>`,
		}, "frames:\n  mobile: 390x844\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		// "settings" is a real stem; "ghost" is not, and it both leaves home
		// and returns to it — non-terminal.
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> home\n  home --> ghost\n  ghost --> home\n"), 0o644))

		all, err := ValidateFlowsDir(root)
		require.NoError(t, err)
		// The §1c node-stem rule also fires on the same unregistered node —
		// that is its job. The §4b edge rule is what this test pins, so run it
		// through the single-file entry point where the node-stem rule is
		// covered separately.
		assert.Equal(t, 1, findingRules(all)[ruleFlowchartNodeStem])
		consistency := ValidateConsistency(root)
		assert.Equal(t, 1, findingRules(consistency)[ruleConsistencyFlowEdgeWireframe], "got %#v", consistency)
	})

	t.Run("valid-screen-flow-clean", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
			"home.svg":  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`,
		}, "frames:\n  mobile: 390x844\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> home\n"), 0o644))

		findings, err := ValidateFlowsDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})
}

// ---------------------------------------------------------------------------
// §4b rule 3 — README screen references exist
//
// The consolidated item-4.9 severity matrix for all four §4b acceptance rules
// (orphan screen, dangling data-nav, this README reference, literal-color
// tracking) lives in acceptance_4b_test.go:TestAcceptance4bSeededFixturesSeverityMatrix,
// which asserts the whole rule→severity table in one auditable place. The
// per-rule subtests below remain the deep behavioural coverage.
// ---------------------------------------------------------------------------

// TestConsistencyReadmeScreenRefs covers the §4b "screens referenced in README
// exist" rule: a Screens/Flows listing naming a screen with no wireframe (or
// screen file) is a finding; a listing whose screen exists is clean.
func TestConsistencyReadmeScreenRefs(t *testing.T) {
	t.Run("missing-wireframe-flagged", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Screens\n\n- `login` — draft — sign in\n- `checkout` — draft — pay\n\n## Status markers\n\ndraft, review, ready\n")

		findings := ValidateConsistency(root)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleManifestLinkDangling], "got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleManifestLinkDangling {
				assert.Equal(t, filepath.Join(DirName, ManifestName), f.File)
				assert.Equal(t, SeverityWarn, f.Severity, "a missing README screen reference is an advisory consistency warn")
				assert.Contains(t, f.Message, "checkout")
				assert.Equal(t, 9, f.Line, "the finding anchors at the listing bullet")
			}
		}
	})

	t.Run("flows-listing-resolves-to-flow-file", func(t *testing.T) {
		// A Flows entry names a flow: it is satisfied by design/flows/<name>.mmd,
		// not by a wireframe. A flow file present with no wireframe is clean.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Flows\n\n- `sign-up` — draft — account creation\n\ndraft, review, ready\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "sign-up.mmd"),
			[]byte("flowchart TD\n  login --> login\n"), 0o644))

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("flows-listing-missing-flow-flagged", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Flows\n\n- `sign-up` — draft\n\ndraft, review, ready\n")

		findings := ValidateConsistency(root)
		rules := findingRules(findings)
		assert.Equal(t, 1, rules[ruleManifestLinkDangling], "got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleManifestLinkDangling {
				assert.Contains(t, f.Message, "sign-up.mmd")
				assert.Equal(t, SeverityWarn, f.Severity)
			}
		}
	})

	t.Run("flows-listing-satisfied-by-wireframe-stem", func(t *testing.T) {
		// A screen flow and its wireframe share a stem, so a Flows entry whose
		// wireframe exists is clean even without the flow file.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"sign-up.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Sign up</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Flows\n\n- `sign-up` — draft\n\ndraft, review, ready\n")

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("wireframe-present-clean", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg":    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
			"checkout.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Checkout</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Screens\n\n- `login` — draft\n- `checkout` — draft\n\ndraft, review, ready\n")

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("delivered-screen-without-wireframe-clean", func(t *testing.T) {
		// A screen that exists only as a delivered design/screens/*.html file
		// still satisfies "the screen exists" (wireframes are the pre-code
		// mockup; a built screen supersedes them).
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\nframes:\n  mobile: 390x844\n\n## Screens\n\n- `checkout` — draft\n\ndraft, review, ready\n")
		require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "checkout.html"),
			[]byte("<!DOCTYPE html><html><body>x</body></html>"), 0o644))

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("frames-and-status-prose-ignored", func(t *testing.T) {
		// Only Screens/Flows section bullets are read: frames entries, status
		// prose, and link lists do not create screen references.
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\n## Device frames\n\nframes:\n  mobile: 390x844\n\n## Links\n\n- [Brand](brand/brand.md)\n\n## Status markers\n\nScreens and flows carry draft, review, or ready.\n")

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("deduplicated-per-section", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\n## Screens\n\n- `ghost` — draft — one\n- `ghost` — draft — two\n")

		findings := ValidateConsistency(root)
		assert.Equal(t, 1, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("fenced-listing-skipped", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
		}, "# Design\n\n## Screens\n\n```\n- `ghost` — draft\n```\n")

		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleManifestLinkDangling], "got %#v", findings)
	})

	t.Run("missing-readme-clean", func(t *testing.T) {
		root := t.TempDir()
		findings := ValidateConsistency(root)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})
}

// ---------------------------------------------------------------------------
// tree-level wiring (design_validate / design_critique static pass)
// ---------------------------------------------------------------------------

// TestValidateTreeConsistencyFindings proves the §4b rules ride the standard
// dispatch: ValidateTree surfaces both the flow-edge and README screen
// findings on a seeded-bad tree, each with the consistency warn severity.
func TestValidateTreeConsistencyFindings(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// Seed a flow whose non-terminal node has no wireframe: home -> profile ->
	// home makes "profile" non-terminal and unwired.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "sign-up.mmd"),
		[]byte("flowchart LR\n  login --> home\n  home --> profile\n  profile --> home\n"), 0o644))

	// Seed a README Screens listing naming a screen with no wireframe.
	readme := validTreeManifest + "\n## Screens\n\n- `billing` — draft — pay\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, ManifestName), []byte(readme), 0o644))

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
	assert.Equal(t, 1, rules[ruleManifestLinkDangling], "got %#v", findings)

	for _, f := range findings {
		switch f.Rule {
		case ruleConsistencyFlowEdgeWireframe:
			assert.Equal(t, "design/flows/sign-up.mmd", f.File)
			assert.Equal(t, SeverityWarn, f.Severity)
			assert.Contains(t, f.Message, "profile")
		case ruleManifestLinkDangling:
			assert.Equal(t, filepath.Join(DirName, ManifestName), f.File)
			assert.Equal(t, SeverityWarn, f.Severity)
			assert.Contains(t, f.Message, "billing")
		}
	}
	assertSortedFindings(t, findings)
}

// TestValidateFileFlowBidirectionality proves a single-file flow run (the
// design_critique per-target path) surfaces the §4b edge rule.
func TestValidateFileFlowBidirectionality(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// login -> home resolves; home -> profile -> home leaves "profile"
	// non-terminal and unwired.
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "flows", "sign-up.mmd"),
		[]byte("flowchart LR\n  login --> home\n  home --> profile\n  profile --> home\n"), 0o644))

	findings, err := ValidateFile(root, "design/flows/sign-up.mmd")
	require.NoError(t, err)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[ruleConsistencyFlowEdgeWireframe], "got %#v", findings)
	for _, f := range findings {
		if f.Rule == ruleConsistencyFlowEdgeWireframe {
			assert.Equal(t, SeverityWarn, f.Severity)
		}
	}
}

// TestValidateConsistencyNoDesignTree covers the missing-tree contract: no
// design/ directory means no consistency findings, not an error.
func TestValidateConsistencyNoDesignTree(t *testing.T) {
	findings := ValidateConsistency(t.TempDir())
	require.NotNil(t, findings)
	assert.Empty(t, findings)
}
