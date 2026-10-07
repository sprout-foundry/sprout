package planstore

import (
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// RenderMarkdown renders plan as a deterministic, human-readable markdown
// view: the goal as the title, a small metadata block (schema version,
// revision, timestamps, and the design/starter references when set), then
// the scope, steps, acceptance and out-of-scope sections. Sections with no
// entries are omitted, so a minimal plan renders minimally.
//
// The rendering is pure (no I/O) and stable: it walks the plan's slices in
// document order and emits no nondeterministic values, so a given plan
// renders to identical markdown on every run and platform. It is the
// function Save uses to regenerate .sprout/plan.md from the JSON, and it is
// exported so other tooling (e.g. `sprout plan --structured`)
// can render the same view.
func RenderMarkdown(plan *plancontract.Plan) string {
	if plan == nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", plan.Goal)

	// Metadata block.
	fmt.Fprintf(&b, "- **Schema:** v%d\n", plan.Version)
	fmt.Fprintf(&b, "- **Revision:** %d\n", plan.Revision)
	fmt.Fprintf(&b, "- **Created:** %s\n", formatTime(plan.Created))
	fmt.Fprintf(&b, "- **Updated:** %s\n", formatTime(plan.Updated))
	if plan.Design != "" {
		fmt.Fprintf(&b, "- **Design:** %s\n", plan.Design)
	}
	if plan.Starter != "" {
		fmt.Fprintf(&b, "- **Starter:** %s\n", plan.Starter)
	}

	if len(plan.Scope) > 0 {
		b.WriteString("\n## Scope\n\n")
		for _, s := range plan.Scope {
			fmt.Fprintf(&b, "- **%s** — %s\n", s.ID, s.Title)
			if s.Description != "" {
				fmt.Fprintf(&b, "  %s\n", s.Description)
			}
		}
	}

	if len(plan.Steps) > 0 {
		b.WriteString("\n## Steps\n\n")
		for i, st := range plan.Steps {
			fmt.Fprintf(&b, "%d. **%s** %s\n", i+1, st.Scope, st.Description)
		}
	}

	if len(plan.Acceptance) > 0 {
		b.WriteString("\n## Acceptance\n\n")
		for _, a := range plan.Acceptance {
			check := a.Check
			if check == "" {
				check = "(manual: reported, not run)"
			}
			fmt.Fprintf(&b, "- **%s** (%s, %s): %s\n", a.ID, a.Scope, a.Kind, check)
			// Interaction items render their scripted browse steps as a
			// numbered sub-list in execution order. Steps are
			// only ever present on interaction items (the validator
			// enforces it), so this renders exactly when they exist.
			for i, st := range a.Steps {
				fmt.Fprintf(&b, "  %d. %s\n", i+1, formatStep(st))
			}
		}
	}

	if len(plan.OutOfScope) > 0 {
		b.WriteString("\n## Out of scope\n\n")
		for _, o := range plan.OutOfScope {
			fmt.Fprintf(&b, "- %s — %s\n", o.Item, o.Reason)
		}
	}

	return b.String()
}

// formatStep renders one browse step deterministically: the action first,
// then the key parameters in a fixed order (selector, value, key, millis,
// script, expect, screenshot_path), each only when set. A bare step renders
// as its action alone.
func formatStep(st plancontract.BrowseStep) string {
	var parts []string
	if st.Selector != "" {
		parts = append(parts, "selector: "+st.Selector)
	}
	if st.Value != "" {
		parts = append(parts, "value: "+st.Value)
	}
	if st.Key != "" {
		parts = append(parts, "key: "+st.Key)
	}
	if st.Millis != 0 {
		parts = append(parts, fmt.Sprintf("millis: %d", st.Millis))
	}
	if st.Script != "" {
		parts = append(parts, "script: "+st.Script)
	}
	if st.Expect != "" {
		parts = append(parts, "expect: "+st.Expect)
	}
	if st.ScreenshotPath != "" {
		parts = append(parts, "screenshot_path: "+st.ScreenshotPath)
	}
	if len(parts) == 0 {
		return st.Action
	}
	return st.Action + " " + strings.Join(parts, " ")
}

// formatTime renders t as an RFC3339 string, or a placeholder when t is the
// zero time (a plan that has not had its lifecycle timestamps set).
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "(unset)"
	}
	return t.Format(time.RFC3339)
}
