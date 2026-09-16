package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-1 item 1.10 — Fixture test tree + seeded-bad fixtures.
//
// TestDesignAssetConventions is the acceptance test the SP-140-1 spec names:
// a full fixture design/ tree validates with zero findings, and each seeded-bad
// fixture trips exactly the rule class its defect belongs to. The per-rule unit
// tests (tokens_test.go, svg_test.go, flowchart_test.go, screens_test.go,
// manifest_test.go, icons_test.go, brand_test.go, gitcontract_test.go) pin the
// fine detail of every rule; this file pins the end-to-end tree contract the
// spec's Acceptance Criteria enumerate:
//
//   - broken alias reference        -> token_alias_dangling
//   - cyclic aliases                -> token_alias_cycle
//   - unknown $type                 -> token_type_membership
//   - SVG missing viewBox           -> svg_viewbox
//   - SVG with <script>             -> svg_self_containment
//   - SVG with external href        -> svg_self_containment
//   - data-nav to a nonexistent screen -> svg_data_nav_dangling
//   - mermaid node id with no matching wireframe -> flowchart_node_stem
//   - README link to a missing file -> manifest_link_dangling
//   - screens HTML with a network script -> screen_external_ref
// -----------------------------------------------------------------------------

// seedFixture writes rel (slash-separated) under root with parent directories.
// It mirrors writeValidDesignTree's write helper so seeded-bad fixtures use the
// same on-disk layout the valid tree uses.
func seedFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// TestDesignAssetConventions_ValidTreeZeroFindings asserts the first
// Acceptance Criterion: a full fixture design/ tree — tokens, wireframes,
// a screen, an icon, brand.md, the README manifest, a flow, and the §1h git
// contract — validates with zero findings of any severity.
func TestDesignAssetConventions_ValidTreeZeroFindings(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err, "a valid tree must not raise an I/O error")
	require.NotNil(t, findings, "a completed whole-tree run must return a non-nil slice")
	assert.Empty(t, findings, "a valid design tree must yield zero findings, got %#v", findings)
}

// TestDesignAssetConventions_SeededBadFixtures asserts the second Acceptance
// Criterion: each seeded-bad fixture produces the right finding class. Every
// case starts from the valid tree and seeds exactly one defect, so the
// assertion can require a single finding carrying exactly the expected rule —
// proving the fixture trips its own rule class and nothing else.
func TestDesignAssetConventions_SeededBadFixtures(t *testing.T) {
	cases := []struct {
		name     string
		seedRel  string
		seedBody string
		wantRule string
		wantSev  Severity
	}{
		{
			name:    "broken alias reference",
			seedRel: "design/tokens/spacing.tokens.json",
			seedBody: `{
  "spacing": {
    "md": {"$value": "{spacing.missing}", "$type": "dimension"}
  }
}`,
			wantRule: ruleTokenAliasDangling,
			wantSev:  SeverityError,
		},
		{
			name:    "cyclic aliases",
			seedRel: "design/tokens/motion.tokens.json",
			seedBody: `{
  "motion": {
    "fast": {"$value": "{motion.slow}", "$type": "duration"},
    "slow": {"$value": "{motion.fast}", "$type": "duration"}
  }
}`,
			wantRule: ruleTokenAliasCycle,
			wantSev:  SeverityError,
		},
		{
			name:    "unknown $type",
			seedRel: "design/tokens/color.tokens.json",
			seedBody: `{
  "color": {
    "brand": {
      "primary": {"$value": "#0055ff", "$type": "shadow"}
    }
  }
}`,
			wantRule: ruleTokenTypeMembership,
			wantSev:  SeverityError,
		},
		{
			name:     "SVG missing viewBox",
			seedRel:  "design/wireframes/checkout.svg",
			seedBody: `<svg xmlns="http://www.w3.org/2000/svg"><text x="0" y="0">Checkout</text></svg>`,
			wantRule: ruleSVGViewBox,
			wantSev:  SeverityError,
		},
		{
			name:     "SVG with <script>",
			seedRel:  "design/wireframes/checkout.svg",
			seedBody: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Checkout</text><script>steal()</script></svg>`,
			wantRule: ruleSVGSelfContainment,
			wantSev:  SeverityError,
		},
		{
			name:     "SVG with external href",
			seedRel:  "design/wireframes/checkout.svg",
			seedBody: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Checkout</text><image href="https://cdn.example.com/logo.png" /></svg>`,
			wantRule: ruleSVGSelfContainment,
			wantSev:  SeverityError,
		},
		{
			name:     "data-nav to a nonexistent screen",
			seedRel:  "design/wireframes/checkout.svg",
			seedBody: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Checkout</text><rect id="go" data-nav="nowhere" /></svg>`,
			wantRule: ruleSVGDataNavDangling,
			wantSev:  SeverityError,
		},
		{
			name:     "mermaid node id with no matching wireframe",
			seedRel:  "design/flows/sign-up.mmd",
			seedBody: "flowchart TD\n  login --> not-a-stem\n",
			wantRule: ruleFlowchartNodeStem,
			wantSev:  SeverityError,
		},
		{
			name:     "README link to a missing file",
			seedRel:  "design/README.md",
			seedBody: "# Design Workspace\n\nframes:\n  mobile: 390x844\n\nStatus markers: draft, review, ready.\n\n- [Missing](tokens/nope.tokens.json)\n",
			wantRule: ruleManifestLinkDangling,
			wantSev:  SeverityError,
		},
		{
			name:     "screens HTML with a network script",
			seedRel:  "design/screens/checkout.html",
			seedBody: `<html><head><script src="https://cdn.example.com/checkout.js"></script></head><body><h1>Checkout</h1></body></html>`,
			wantRule: ruleScreenExternalRef,
			wantSev:  SeverityError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeValidDesignTree(t, root)
			seedFixture(t, root, tc.seedRel, tc.seedBody)

			findings, err := ValidateTree(root)
			require.NoError(t, err, "a rule violation is a finding, never an I/O error")
			require.NotNil(t, findings)

			// Exactly one seeded defect on top of a valid tree: require the
			// single finding and pin its rule, file, and severity so a
			// fixture cannot pass by tripping the wrong rule class.
			require.Len(t, findings, 1,
				"seeded fixture %q must produce exactly one finding, got %#v", tc.name, findings)
			f := findings[0]
			assert.Equal(t, tc.wantRule, f.Rule)
			assert.Equal(t, tc.wantSev, f.Severity)
			assert.Equal(t, tc.seedRel, f.File)
			assert.NotEmpty(t, f.Message)
		})
	}
}

// TestDesignAssetConventions_ScriptAndExternalHrefShareRule documents the one
// place two enumerated bad fixtures map to the same rule class: both `<script>`
// and an external href are self-containment violations (§1b), so a document
// carrying both reports two findings under svg_self_containment rather than one
// merged finding. This pins the class cardinality the AC lists as two rows.
func TestDesignAssetConventions_ScriptAndExternalHrefShareRule(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	seedFixture(t, root, "design/wireframes/checkout.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Checkout</text><script>steal()</script><image href="https://cdn.example.com/logo.png" /></svg>`)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.Len(t, findings, 2, "a <script> and an external href are two self-containment findings")
	for _, f := range findings {
		assert.Equal(t, ruleSVGSelfContainment, f.Rule)
		assert.Equal(t, SeverityError, f.Severity)
		assert.Equal(t, "design/wireframes/checkout.svg", f.File)
	}
}

// TestDesignAssetConventions_FixtureTreeShape guards the fixture's own shape:
// the valid tree must actually contain every §1.x asset class the conventions
// cover, so a green run means each validator had something to inspect rather
// than passing vacuously on empty directories.
func TestDesignAssetConventions_FixtureTreeShape(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	required := []string{
		"design/README.md",
		"design/tokens/color.tokens.json",
		"design/wireframes/login.svg",
		"design/wireframes/home.svg",
		"design/flows/sign-up.mmd",
		"design/screens/login.html",
		"design/icons/home.svg",
		"design/brand/brand.md",
		GitContractFile,
		GitIgnoreFile,
	}
	for _, rel := range required {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		require.NoError(t, err, "the fixture tree is missing %s", rel)
		assert.True(t, info.Mode().IsRegular(), "%s must be a regular file", rel)
		assert.NotZero(t, info.Size(), "%s must not be empty", rel)
	}
}
