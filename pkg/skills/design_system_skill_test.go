package skills

import (
	"strings"
	"testing"
)

// TestDesignSystemSkillFeedbackLoop pins the SP-140-4 §4d / item 4.7 wiring in
// the design-system skill: the loop must start any `changes-requested` target
// with a read of its feedback file, reference design_assets' pending-feedback
// report, and describe closing the loop with a resolution note. The skill is
// prose, so the test asserts the load-bearing sentences exist — a future edit
// that drops the feedback step would silently undo the feature.
func TestDesignSystemSkillFeedbackLoop(t *testing.T) {
	content, err := ReadContent("design-system")
	if err != nil {
		t.Fatalf("design-system skill must be embedded: %v", err)
	}
	body := strings.ToLower(content)

	needles := map[string]string{
		"feedback file path":       "design/feedback/<target>.json",
		"pending report":           "pending feedback",
		"design_assets pending":    "design_assets", // the pending report source
		"changes-requested status": "changes-requested",
		"read the feedback file":   "read of each pending target's feedback",
		"unresolved annotation":    "resolved: false",
		"resolution note":          "resolution",
	}
	for label, needle := range needles {
		if !strings.Contains(body, strings.ToLower(needle)) {
			t.Errorf("design-system skill must mention %q (%s)", needle, label)
		}
	}
}

// TestDesignSystemSkillCoCommit pins the SP-140-5 §5f / item 5.7 wiring in the
// design-system skill's sync section: the co-commit rule (the dev change and
// its design_sync --apply adoption land in ONE commit, so git revert can never
// desync the loop) and the `design:` commit-type convention (design-led
// iterations use it; dev-led keep feat:/fix: and include the design/ adoption
// in the same commit). The skill is prose, so the test asserts the
// load-bearing tokens exist — a future edit that drops the rule would silently
// undo the feature. The fixture-repo proof that one commit carries both sides
// lives in pkg/design/cocommit_test.go.
func TestDesignSystemSkillCoCommit(t *testing.T) {
	content, err := ReadContent("design-system")
	if err != nil {
		t.Fatalf("design-system skill must be embedded: %v", err)
	}
	body := strings.ToLower(content)

	needles := map[string]string{
		"co-commit rule":             "co-commit",
		"single commit carries both": "one commit carries both",
		"apply adoption":             "design_sync --apply",
		"revert implication":         "revert",
		"design commit type":         "`design:`",
		"extension not fork":         "not a fork",
		"dev-led keeps feat:/fix:":   "`feat:`/`fix:`",
		"same commit":                "same commit",
	}
	for label, needle := range needles {
		if !strings.Contains(body, strings.ToLower(needle)) {
			t.Errorf("design-system skill must mention %q (%s)", needle, label)
		}
	}
}

// TestDesignSystemSkillScreenKit pins the SP-143 §143.6 generator surface in
// the design-system skill: screens start from the base templates, style
// utilities-first from the generated theme, navigate with real data-nav
// anchors, declare their states, reference the runtime instead of authoring
// it, and regenerate screens.json instead of hand-editing it. The skill is
// prose, so the test asserts the load-bearing sentences exist — prompt and
// validator must state the same rules (the review blocker §143.6 names).
func TestDesignSystemSkillScreenKit(t *testing.T) {
	content, err := ReadContent("design-system")
	if err != nil {
		t.Fatalf("design-system skill must be embedded: %v", err)
	}
	body := strings.ToLower(content)

	needles := map[string]string{
		"base templates":      "base/phone.html",
		"runtime referenced":  "never hand-edited",
		"runtime hash check":  "screen_runtime_hash",
		"data-nav anchors":    `data-nav="to:<stem>;trigger:<label>"`,
		"nav targets resolve": "hard error",
		"declared states":     `data-states="a,b,c"`,
		"state sections":      `data-state="a"`,
		"utilities-first":     ".bg-*",
		"spacing utilities":   ".p-*",
		"radius utilities":    ".rounded-*",
		"shadow utilities":    ".shadow-*",
		"var references":      "var(--token)",
		"derived index":       "design_export_tokens targets:screens",
		"index drift":         "screen_index_drift",
	}
	for label, needle := range needles {
		if !strings.Contains(body, strings.ToLower(needle)) {
			t.Errorf("design-system skill must mention %q (%s)", needle, label)
		}
	}
}

// TestDesignSystemSkillFormatRework pins the SP-140-9 generator surface: the
// skill must teach the post-9.4 formats — screens as the only screen tier
// (wireframes removed, presence an error), flow .json sources with derived
// .mmd exports — matching what the validator enforces (the review blocker).
func TestDesignSystemSkillFormatRework(t *testing.T) {
	content, err := ReadContent("design-system")
	if err != nil {
		t.Fatalf("design-system skill must be embedded: %v", err)
	}
	body := strings.ToLower(content)

	needles := map[string]string{
		"screens primary":    "primary tier",
		"wireframes removed": "removed",
		"screen identity":    "data-screen=\"<stem>\"",
		"flow sources":       "<flow-name>.json",
		"flows export":       "design_export_tokens targets:flows",
		"mmd derived":        "never hand-edit",
		"flow hash":          "flow-source-hash",
		"off-path warn":      "off-path",
	}
	for label, needle := range needles {
		if !strings.Contains(body, strings.ToLower(needle)) {
			t.Errorf("design-system skill must mention %q (%s)", needle, label)
		}
	}
}
