package skills

import (
	"strings"
	"testing"
)

// TestDesignSystemSkillMockupLoop pins the design-system skill to the one goal
// the design workspace has: a fast, on-brand mockup drawn by draw_mockup and
// changed in the person's own words. The skill is prose, so the test asserts
// the load-bearing rules exist — an edit that reintroduces a design process
// (token tiers, validation passes, flow sources) or drops the kit rule would
// silently undo the goal.
func TestDesignSystemSkillMockupLoop(t *testing.T) {
	content, err := ReadContent("design-system")
	if err != nil {
		t.Fatalf("design-system skill must be embedded: %v", err)
	}
	body := strings.ToLower(content)

	needles := map[string]string{
		"the tool":              "draw_mockup",
		"never hand-write":      "never write the mockup html yourself",
		"kit composition":       "compose from a kit",
		"one document":          "one document, many screens",
		"change in their words": "pass the person's words",
		"sketch, not a system":  "a mockup is a sketch",
	}
	for label, needle := range needles {
		if !strings.Contains(body, strings.ToLower(needle)) {
			t.Errorf("design-system skill must mention %q (%s)", needle, label)
		}
	}

	// The withdrawn process layer must not creep back in.
	for _, banned := range []string{"design_validate", "design_export_tokens", "design_sync", "flow-source-hash", "tokens.json"} {
		if strings.Contains(body, strings.ToLower(banned)) {
			t.Errorf("design-system skill must not reference the withdrawn design process (%q)", banned)
		}
	}
}
