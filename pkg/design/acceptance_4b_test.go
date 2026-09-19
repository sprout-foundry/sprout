package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SP-140-4 §4b acceptance — seeded fixtures trip each consistency rule with the
// RIGHT SEVERITY (TODO item 4.9).
//
// The per-rule tests live beside their rule packs (consistency_test.go for the
// flow/wireframe + README rules, inventory_rules_test.go for token usage /
// orphan screens / naming, svg_test.go for the per-file SVG rules). This file
// is the single *auditable* acceptance surface the AC asks for: one seeded-bad
// tree that trips all four §4b AC rules at once, asserted as a rule→severity
// matrix through the standard whole-tree entry point (ValidateTree) — the same
// dispatch design_validate and design_critique's static pass use.
//
// The AC's four named rules and their required severities (SP-140-4 §4b):
//
//   - orphan screen (`consistency_screen_orphan`)          → info
//   - dangling data-nav (`svg_data_nav_dangling`)           → error (hard)
//   - README screen reference to a missing file
//     (`manifest_link_dangling`)                            → warn
//   - literal color with no {token.path} comment
//     (`svg_token_usage`)                                   → info
//
// A severity regression in any of them fails here, even if the rule still
// fires, so the matrix pins the "with the right severity" half of the AC that
// a count-only assertion cannot.
//
// NOTE on the shared rule id: the §4b "README screen reference to a missing
// file" check and SP-140-1's manifest markdown-link check both emit the
// ruleManifestLinkDangling rule id, but they are *different failure modes with
// deliberately different severities*:
//
//   - a markdown link `[Login](screens/login.html)` that does not resolve is a
//     hard violation (SeverityError, manifest.go — the manifest is broken);
//   - a Screens/Flows *listing bullet* `- \`checkout\` — draft` naming a screen
//     with no file is an advisory consistency warn (SeverityWarn,
//     consistency.go — the tree is renderable but has drifted).
//
// The AC's "right severity" row below is the listing-bullet path (warn). The
// markdown-link path's hard severity is pinned separately by
// TestAcceptance4bManifestMarkdownLinkStaysHard, so a future change cannot
// collapse the two into one severity unnoticed.
// ---------------------------------------------------------------------------

// ac4bRuleSeverity is the expected severity of each §4b acceptance rule.
// Kept as a table so the test reads as the AC itself and a new §4b rule can be
// added in one place.
var ac4bRuleSeverity = []struct {
	rule string
	want Severity
	why  string
}{
	{ruleConsistencyScreenOrphan, SeverityInfo,
		"an unreferenced screen is advisory: wireframes are drafts, an orphan is drift not breakage"},
	{ruleSVGDataNavDangling, SeverityError,
		"a data-nav target with no wireframe is a hard violation (a dead navigation link)"},
	{ruleManifestLinkDangling, SeverityWarn,
		"a README Screens/Flows listing bullet naming a screen/flow with no file is an advisory consistency warn (§4b); the distinct markdown-link path stays hard, see TestAcceptance4bManifestMarkdownLinkStaysHard"},
	{ruleSVGTokenUsage, SeverityInfo,
		"a literal color without a {token.path} comment is tracked, not rejected (wireframes are drafts)"},
}

// TestAcceptance4bSeededFixturesSeverityMatrix is the item-4.9 AC test: one
// seeded-bad design tree trips every §4b acceptance rule through ValidateTree,
// and each finding carries the severity the spec assigns it.
//
// The tree starts from the known-good fixture (so no other rule fires for the
// wrong reason) and is then seeded with exactly the four defects:
//
//  1. `billing.svg` — a wireframe carrying a literal color and no token
//     comment at all (token usage, info), declared by neither the flow nor the
//     README (orphan screen, info);
//  2. `login.svg` — a dangling `data-nav="checkout"` with no checkout
//     wireframe (hard, error);
//  3. `README.md` — a Screens bullet naming `receipt`, which has no file
//     (README screen reference, warn).
func TestAcceptance4bSeededFixturesSeverityMatrix(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// (1)+(2): a seeded wireframe with a literal fill (#ff0000, no token
	// comment) that is referenced by neither the flow nor the README.
	seedFixture(t, root, "design/wireframes/billing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Billing</text></svg>`)

	// (3): a dangling data-nav on the login wireframe. "checkout" has no
	// wireframe, so the hard rule fires on the seeded source line.
	seedFixture(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="checkout"/></svg>`)

	// (4): a README Screens bullet promising a screen that does not exist.
	readme := validTreeManifest + "\n## Screens\n\n- `receipt` — draft — pay\n"
	seedFixture(t, root, "design/README.md", readme)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings)

	// One finding per seeded defect, and each rule fires exactly once so a
	// duplicate or a miss is visible rather than averaged away.
	counts := findingRules(findings)
	for _, tc := range ac4bRuleSeverity {
		assert.Equal(t, 1, counts[tc.rule],
			"AC rule %s must trip exactly once on the seeded tree, got %#v", tc.rule, findings)
	}

	// Severity matrix: every finding of every §4b AC rule carries the
	// spec-assigned severity, and its file/anchor points at the seeded defect.
	wantFile := map[string]string{
		ruleConsistencyScreenOrphan: "design/wireframes/billing.svg",
		ruleSVGDataNavDangling:      "design/wireframes/login.svg",
		ruleManifestLinkDangling:    filepath.Join(DirName, ManifestName),
		ruleSVGTokenUsage:           "design/wireframes/billing.svg",
	}
	seen := map[string]bool{}
	for _, f := range findings {
		for _, tc := range ac4bRuleSeverity {
			if f.Rule != tc.rule {
				continue
			}
			seen[tc.rule] = true
			assert.Equal(t, tc.want, f.Severity,
				"rule %s: %s", tc.rule, tc.why)
			assert.Equal(t, wantFile[tc.rule], f.File,
				"rule %s must anchor at the seeded file", tc.rule)
		}
	}
	for _, tc := range ac4bRuleSeverity {
		assert.True(t, seen[tc.rule],
			"the seeded tree must trip rule %s (%s); got %#v", tc.rule, tc.why, findings)
	}

	// The whole-tree run's findings are deterministically ordered, so the
	// matrix is a stable audit artifact rather than a map iteration.
	assertSortedFindings(t, findings)
}

// TestAcceptance4bSeededFixturesAreCleanWithoutSeed is the negative half of the
// AC matrix: the same checks produce no §4b finding on the unseeded fixture
// tree, so a matrix row can only pass because its defect was seeded — not
// because the rule fires unconditionally.
func TestAcceptance4bSeededFixturesAreCleanWithoutSeed(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings)

	for _, tc := range ac4bRuleSeverity {
		assert.Equal(t, 0, findingRules(findings)[tc.rule],
			"the clean fixture tree must not trip %s; got %#v", tc.rule, findings)
	}
	assert.Empty(t, findings, "the clean fixture tree must stay finding-free, got %#v", findings)
}

// TestAcceptance4bSeverityIsStableAcrossSeededTrees pins that the severity of
// each §4b rule is a property of the rule, not of the particular seed: the same
// four severities are observed on a second, differently-seeded tree (a
// delivered screen instead of a wireframe for the orphan, a flow file instead
// of a README bullet for the reference). A severity that drifted per call site
// would fail here.
func TestAcceptance4bSeverityIsStableAcrossSeededTrees(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// Orphan: an extra *wireframe* referenced by neither the flow nor the
	// README (the orphan rule is scoped to wireframes — the pre-code draft
	// inventory — so the seed must be a wireframe, not a delivered screen).
	seedFixture(t, root, "design/wireframes/receipt.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Receipt</text></svg>`)
	// Token usage: a literal stroke with no token comment, on a wireframe that
	// *is* referenced by the flow, so the orphan rule stays silent for it.
	seedFixture(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text stroke="rgb(1,2,3)">Home</text></svg>`)
	// README reference: a Flows bullet naming a flow with no file.
	seedFixture(t, root, "design/README.md",
		validTreeManifest+"\n## Flows\n\n- `checkout-flow` — draft — pay\n")
	// data-nav: a dangling target on the login wireframe.
	seedFixture(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="nowhere"/></svg>`)

	findings, err := ValidateTree(root)
	require.NoError(t, err)

	byRule := map[string][]Finding{}
	for _, f := range findings {
		byRule[f.Rule] = append(byRule[f.Rule], f)
	}
	for _, tc := range ac4bRuleSeverity {
		got := byRule[tc.rule]
		require.NotEmpty(t, got, "seeded tree must trip %s; got %#v", tc.rule, findings)
		for _, f := range got {
			assert.Equal(t, tc.want, f.Severity, "rule %s on %s", tc.rule, f.File)
		}
	}
}

// TestAcceptance4bSeededFixtureDataNavHardRulePerFile pins the dangling
// data-nav rule at the per-file entry point (design_validate file-target and
// design_critique's per-target static pass): the seeded dangling target is a
// hard error there too, so the severity is not a whole-tree-only property.
func TestAcceptance4bSeededFixtureDataNavHardRulePerFile(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	seedFixture(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text><rect id="go" data-nav="checkout"/></svg>`)

	findings, err := ValidateFile(root, "design/wireframes/login.svg")
	require.NoError(t, err)

	var checked bool
	for _, f := range findings {
		if f.Rule == ruleSVGDataNavDangling {
			checked = true
			assert.Equal(t, SeverityError, f.Severity)
			assert.Contains(t, f.Message, "checkout")
		}
	}
	require.True(t, checked, "per-file validation must surface the hard data-nav rule, got %#v", findings)
}

// TestAcceptance4bSeededFixtureNoDesignTreeIsClean covers the missing-tree
// contract for the AC matrix: no design/ directory yields no §4b findings (and
// no error), so the matrix's rows cannot be satisfied by ambient findings.
func TestAcceptance4bSeededFixtureNoDesignTreeIsClean(t *testing.T) {
	root := t.TempDir()
	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings)
	assert.Empty(t, findings)

	// The individual pack entry points agree.
	assert.Empty(t, ValidateConsistency(root))
	assert.Empty(t, ValidateInventory(root))

	// Sanity: the fixture root really has no design/ tree.
	_, statErr := os.Stat(filepath.Join(root, DirName))
	assert.True(t, os.IsNotExist(statErr), "the fixture root must not contain design/")
}

// TestAcceptance4bManifestMarkdownLinkStaysHard pins the other side of the
// shared ruleManifestLinkDangling id (see the file header note): a README
// *markdown link* that does not resolve is SP-140-1's hard manifest violation
// (SeverityError), not the §4b listing-bullet warn. Asserting both in the same
// file is what makes the AC's "right severity" claim unambiguous: a change that
// collapsed the two severities would fail one of these two tests rather than
// silently reclassifying a broken manifest.
func TestAcceptance4bManifestMarkdownLinkStaysHard(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// A Links-section markdown link naming a screen that does not exist. This
	// is the manifest's own link rule (SP-140-1 §1g), not a Screens/Flows
	// listing bullet.
	seedFixture(t, root, "design/README.md",
		validTreeManifest+"- [Missing screen](screens/ghost.html)\n")

	findings, err := ValidateTree(root)
	require.NoError(t, err)

	var checked bool
	for _, f := range findings {
		if f.Rule != ruleManifestLinkDangling {
			continue
		}
		checked = true
		// The markdown-link path is hard; the listing path (covered by the
		// matrix above) is warn. The two must stay distinguishable.
		assert.Equal(t, SeverityError, f.Severity,
			"a dangling manifest markdown link is a hard SP-140-1 violation: %#v", f)
		assert.Contains(t, f.Message, "does not resolve")
	}
	require.True(t, checked, "a dangling markdown link must surface rule %s, got %#v",
		ruleManifestLinkDangling, findings)
}
