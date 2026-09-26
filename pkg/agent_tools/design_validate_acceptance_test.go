//go:build !js

package tools

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// SP-140-4 §4b acceptance at the design_validate tool boundary (item 4.9).
//
// pkg/design/acceptance_4b_test.go pins the rule→severity matrix on the
// validator itself. This file pins the same matrix one layer up, on the
// `design_validate` tool output the agent actually reads: a seeded-bad tree
// must surface all four §4b AC rules with the spec severity in the structured
// findings, not merely inside the library. A regression that dropped severity
// while converting findings to the tool's findingOut shape would pass the
// library test and fail here.
//
// The four AC rules and their severities (SP-140-4 §4b / AC):
//
//   - orphan screen (`consistency_screen_orphan`)          → info
//   - dangling data-nav (`svg_data_nav_dangling`)          → error (hard)
//   - README screen reference to a missing file
//     (`manifest_link_dangling`)                            → warn
//   - literal color with no {token.path} comment
//     (`svg_token_usage`)                                   → info
// ---------------------------------------------------------------------------

// dvAC4bRuleSeverity is the expected tool-reported severity of each §4b AC
// rule. Kept as a table so the test reads as the AC itself.
var dvAC4bRuleSeverity = []struct {
	rule string
	want string
	file string
}{
	{"consistency_screen_orphan", "info", "design/screens/billing.html"},
	{"screen_nav_target", "error", "design/screens/checkout.html"},
	{"manifest_link_dangling", "warn", "design/README.md"},
}

// TestDesignValidateHandler_Acceptance4bSeverityMatrix is the item-4.9 AC at
// the tool boundary: one seeded-bad tree trips every §4b acceptance rule, and
// the design_validate structured findings carry the right severity, file, and
// rule for each.
func TestDesignValidateHandler_Acceptance4bSeverityMatrix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)

	// Orphan screen (info): a screen declared by neither the flow nor the
	// README. Token usage was an SVG-tier rule that retired with the
	// wireframes (9.4).
	dvWrite(t, root, "design/screens/billing.html",
		`<!DOCTYPE html><html data-screen="billing"><body>Billing</body></html>`)

	// Dangling data-nav (hard error): "nowhere" has no screen.
	dvWrite(t, root, "design/screens/checkout.html",
		`<!DOCTYPE html><html data-screen="checkout"><body><a data-nav="to:nowhere;trigger:x">go</a></body></html>`)
	// checkout is README-listed so billing stays the lone orphan.
	dvWrite(t, root, "design/README.md",
		dvTestManifest+"\n## Screens\n\n- `receipt` — draft — pay\n- `checkout` — draft — seeded\n")
	dvWrite(t, root, "design/generated/screens.json", dvTestScreensIndex(root))

	// (The receipt bullet rides the README seed above.)

	h := &designValidateHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "consistency findings are advisory and must never block a turn")

	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok)

	// Index the findings by rule so each AC row can be checked independently.
	byRule := map[string]struct{ severity, file string }{}
	for _, f := range out.Findings {
		byRule[f.Rule] = struct{ severity, file string }{f.Severity, f.File}
	}

	for _, tc := range dvAC4bRuleSeverity {
		got, present := byRule[tc.rule]
		require.True(t, present, "design_validate must surface %s; findings=%#v", tc.rule, out.Findings)
		require.Equal(t, tc.want, got.severity, "rule %s severity (findings=%#v)", tc.rule, out.Findings)
		require.Equal(t, tc.file, got.file, "rule %s file anchor", tc.rule)
	}

	// Post-9.4 tallies: one hard data-nav error; two warns (the README
	// reference + the off-path nav the seeded dangle also trips); one orphan
	// info. The fixture is wireframe-free — no deprecation rows.
	require.Equal(t, 1, out.BySeverity["error"], "one hard data-nav error, findings=%#v", out.Findings)
	require.Equal(t, 2, out.BySeverity["warn"], "README reference + off-path nav warns, findings=%#v", out.Findings)
	require.Equal(t, 1, out.BySeverity["info"], "the orphan info, findings=%#v", out.Findings)
	require.Equal(t, 0, out.BySeverity["fix"], "no machine-applicable fixes on this tree")
	require.Equal(t, 4, out.Count)

	// The human-readable summary names the split, so the seed's tool message
	// (not just the structured output) carries the right-severity evidence.
	require.Contains(t, res.Output, "4 finding(s)")
	require.Contains(t, res.Output, "1 error(s)")
	require.Contains(t, res.Output, "2 warn(s)")
	require.Contains(t, res.Output, "1 info")
}

// TestDesignValidateHandler_Acceptance4bCleanTreeIsEmpty is the negative half:
// on the unseeded fixture tree design_validate reports no §4b AC finding, so a
// matrix row above can only pass because its defect was seeded.
func TestDesignValidateHandler_Acceptance4bCleanTreeIsEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)

	h := &designValidateHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok)
	// The fixture's flows are §9b-derived, so the clean tree yields no
	// findings at all — no legacy notices, no §4b rows.
	require.Equal(t, 0, out.Count, "the clean fixture tree must be empty, got %#v", out.Findings)
	for _, sev := range []string{"error", "warn", "fix", "info"} {
		require.Equal(t, 0, out.BySeverity[sev], "severity %s on the clean tree", sev)
	}
	require.Contains(t, res.Output, "0 findings")
}

// TestDesignValidateHandler_Acceptance4bFindingsCarryRuleID pins that every
// finding the tool returns names its rule id (the SP-140-1g schema field the
// AC's "seeded fixtures each trip their rule" depends on): a finding without a
// rule id is not attributable to a rule pack and cannot be audited.
func TestDesignValidateHandler_Acceptance4bFindingsCarryRuleID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	dvWrite(t, root, "design/wireframes/billing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Billing</text></svg>`)

	h := &designValidateHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := res.StructuredOut.(findingsOutput)
	require.NotEmpty(t, out.Findings)
	for _, f := range out.Findings {
		require.NotEmpty(t, f.Rule, "every finding must name its rule, got %#v", f)
		require.Contains(t, []string{"error", "warn", "info", "fix"}, f.Severity,
			"severity must be one of the SP-140-1g classes, got %q", f.Severity)
	}
	_ = design.DirName
}
