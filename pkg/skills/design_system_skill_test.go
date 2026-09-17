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
